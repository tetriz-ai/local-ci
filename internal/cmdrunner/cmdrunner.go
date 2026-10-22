// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package cmdrunner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattn/go-shellwords"
	"github.com/pkg/errors"
)

const logFlushInterval = 5 * time.Second

// cmdWaitDelay bounds how long cmd.Wait blocks for I/O to drain after the child
// process itself has exited. Stdout/Stderr here is a ChanWriter, not an *os.File,
// so Go's exec reads the process output through an os.Pipe it owns. When a
// grandchild inherits the write end and outlives the child — the classic case is
// an SSH ControlMaster spawned by `git fetch` that lingers for ControlPersist
// (minutes) — Wait would otherwise block on that pipe until the grandchild exits.
// WaitDelay makes Wait give up on the lingering pipe shortly after the process is
// done, close its end, and return. A legitimately-finished command has nothing
// left to flush, so this never truncates real output.
const cmdWaitDelay = 2 * time.Second

type Logger interface {
	Debug(msg string)
	Error(msg string)
}

type CmdRunner struct {
	dir        string
	outputPath string
	out        *bufio.Writer
	logger     Logger
}

type CustomExitErr struct {
	exitCode int
	stdErr   string
}

type ChanWriter struct {
	outputChan chan<- string
}

func (cw *ChanWriter) Write(p []byte) (n int, err error) {
	cw.outputChan <- string(p)
	return len(p), nil
}

func (c *CustomExitErr) GetExitCode() int {
	return c.exitCode
}

type CmdRunnerOpts func(*CmdRunner) error

func WithFileOutput(path string) CmdRunnerOpts {
	return func(c *CmdRunner) error {
		dir := filepath.Dir(path)
		err := os.MkdirAll(dir, 0o750)
		if err != nil {
			return errors.Wrap(err, "error creating directory for output")
		}

		f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.ModePerm)
		if err != nil {
			return errors.Wrap(err, "error opening file for writing output")
		}
		c.out = bufio.NewWriter(f)
		c.outputPath = path
		return nil
	}
}

func WithLogger(l Logger) CmdRunnerOpts {
	return func(c *CmdRunner) error {
		c.logger = l
		return nil
	}
}

func WithNoLogger() CmdRunnerOpts {
	return func(c *CmdRunner) error {
		c.logger = noLogger{}
		return nil
	}
}

func NewCmdRunner(workingDir string, opts ...CmdRunnerOpts) (*CmdRunner, error) {
	c := &CmdRunner{
		dir:    workingDir,
		logger: logger{},
	}
	for _, opt := range opts {
		err := opt(c)
		if err != nil {
			return nil, fmt.Errorf("applying cmd runner option: %w", err)
		}
	}
	return c, nil
}

func (c *CustomExitErr) Error() string {
	return fmt.Sprintf("exit code: %d\nstderr: %s\n", c.exitCode, c.stdErr)
}

func (c *CmdRunner) Close() error {
	if c.out != nil {
		if err := c.out.Flush(); err != nil {
			return fmt.Errorf("flushing output: %w", err)
		}
	}
	return nil
}

type CommandRunner interface {
	ExecuteCommand(commandStr string, envs ...string) (output string, err error)
	Close() error
}

// ExecuteCommand runs commandStr, routing it through `bash -c` when it carries a
// shell operator (see buildCmd) and otherwise exec'ing the shellwords-parsed
// arg array directly.
//
// SHELL-INJECTION HAZARD: a caller that concatenates an untrusted value (e.g. a
// git-tracked filename) into commandStr can reach the `bash -c` path — a path
// like `$(rm -rf ~).go` would execute, not be passed as an argument. Such
// callers must either shell-quote every interpolated value or, preferably, use
// ExecuteCommandArgs, which passes args verbatim as an argv slice that no shell
// ever re-parses.
func (c *CmdRunner) ExecuteCommand(commandStr string, envs ...string) (string, error) {
	c.logger.Debug(fmt.Sprintf("Executing command: %s", commandStr))

	if strings.TrimSpace(commandStr) == "" {
		return "", fmt.Errorf("cannot execute empty command")
	}
	// No request context flows into this CLI tool; runCmd owns the command's
	// lifecycle via a cancelable root context bound to the *exec.Cmd.
	ctx, cancelFn := context.WithCancel(context.Background())
	defer cancelFn()
	cmd, err := buildCmd(ctx, commandStr)
	if err != nil {
		return "", fmt.Errorf("building command: %w", err)
	}
	out, err := c.runCmd(ctx, cancelFn, cmd, false, envs...)
	if err != nil {
		return out, fmt.Errorf("running command: %w", err)
	}
	return out, nil
}

// ExecuteCommandSecret runs commandStr exactly like ExecuteCommand but treats the
// command's output as sensitive: the output is returned to the caller and nothing
// else. It is never written to the debug log or the on-disk output file, so a
// secret such as the token from `gh auth token` cannot leak into stdout or the
// untracked output/local-ci/stdoutput.log (from where a `git add -A` could commit it).
// The command string itself is still logged, so do not place a secret in the
// command's arguments — only its output is protected.
func (c *CmdRunner) ExecuteCommandSecret(commandStr string, envs ...string) (string, error) {
	c.logger.Debug(fmt.Sprintf("Executing command (output suppressed): %s", commandStr))

	if strings.TrimSpace(commandStr) == "" {
		return "", fmt.Errorf("cannot execute empty command")
	}
	ctx, cancelFn := context.WithCancel(context.Background())
	defer cancelFn()
	cmd, err := buildCmd(ctx, commandStr)
	if err != nil {
		return "", fmt.Errorf("building command: %w", err)
	}
	out, err := c.runCmd(ctx, cancelFn, cmd, true, envs...)
	if err != nil {
		return out, fmt.Errorf("running command: %w", err)
	}
	return out, nil
}

// ExecuteCommandArgs runs `name` with `args` passed verbatim as an argv slice
// (exec.Command(name, args...)). The args are NEVER joined into a command string
// or re-parsed by a shell, so a value containing shell metacharacters — e.g. a
// filename `$(touch pwned).go` — is delivered as one literal argument and cannot
// execute. Use this (not ExecuteCommand) for any command whose arguments include
// untrusted or interpolated values such as changed-file paths.
func (c *CmdRunner) ExecuteCommandArgs(name string, args []string, envs ...string) (string, error) {
	c.logger.Debug(fmt.Sprintf("Executing command: %s %s", name, strings.Join(args, " ")))

	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("cannot execute empty command")
	}
	ctx, cancelFn := context.WithCancel(context.Background())
	defer cancelFn()

	out, err := c.runCmd(ctx, cancelFn, exec.CommandContext(ctx, name, args...), false, envs...)
	if err != nil {
		return out, fmt.Errorf("running command: %w", err)
	}
	return out, nil
}

// runCmd streams the already-built command's combined output and returns it
// alongside the first error encountered. Callers own how the *exec.Cmd is
// constructed — the shell-vs-argv decision happens before this point — and pass
// the same ctx/cancelFn that bound the command's lifecycle so the output pump
// stops in lockstep with the command.
func (c *CmdRunner) runCmd(ctx context.Context, cancelFn context.CancelFunc, cmd *exec.Cmd, secret bool, envs ...string) (string, error) {
	outputChan := make(chan string)
	errChan := make(chan error)

	combinedOutput := &strings.Builder{}
	defer cancelFn()

	// buildOutput writes combinedOutput from its own goroutine, so this goroutine
	// must not read combinedOutput until the pump has stopped — outputDone closes
	// once buildOutput returns, letting the read below synchronize against the
	// pump's final write instead of racing it.
	outputDone := make(chan struct{})

	go func() {
		c.buildOutput(outputChan, combinedOutput, secret)(ctx)
		close(outputDone)
	}()

	// Start command execution with output streaming

	go func() {
		c.streamCmd(cmd, outputChan, errChan, envs...)
	}()

	// Wait for the command to finish (errChan delivers its error or is closed on
	// success), then stop the output pump and wait for it to drain before
	// reading combinedOutput.
	firstErr, chanOpen := <-errChan
	cancelFn()
	<-outputDone

	var lastErr error
	if chanOpen {
		lastErr = firstErr
		exitErr := &exec.ExitError{}
		if errors.As(firstErr, &exitErr) {
			switch {
			case secret:
				// Keep the secret contract on the failure path too: the combined
				// output may hold sensitive data, so it must never be embedded in the
				// returned error. Only the exit code is surfaced.
				lastErr = &CustomExitErr{
					exitCode: exitErr.ExitCode(),
					stdErr:   "[output suppressed: secret command]",
				}
			case c.out != nil:
				lastErr = &CustomExitErr{
					exitCode: exitErr.ExitCode(),
					stdErr:   string(exitErr.Stderr) + fmt.Sprintf(" ## Check %s for more details", c.outputPath),
				}
			default:
				lastErr = &CustomExitErr{
					exitCode: exitErr.ExitCode(),
					stdErr:   combinedOutput.String(),
				}
			}
		}
	}

	combinedOutputStr := strings.Trim(combinedOutput.String(), "\n")

	// Return combined output and last error encountered
	return combinedOutputStr, lastErr
}

func (c *CmdRunner) flushOutput() {
	if c.out != nil {

		err := c.out.Flush()
		if err != nil {
			c.logger.Error(fmt.Sprintf("error flushing output: %v", err))
		}
	}
}

func (c *CmdRunner) buildOutput(outputChan chan string, combinedOutput *strings.Builder, secret bool) func(ctx context.Context) {
	return func(ctx context.Context) {
		flushTimer := time.NewTimer(logFlushInterval)
		for {
			select {
			case <-ctx.Done():
				c.flushOutput()
				return
			case output_ := <-outputChan:
				switch {
				case secret:
					// Sensitive output (e.g. an auth token from `gh auth token`):
					// capture it only for the caller's return value, never to the
					// debug log or the on-disk output file. Either sink would persist
					// the secret — the log file lives untracked inside the repo, one
					// `git add -A` away from being committed.
					combinedOutput.WriteString(output_)
				case c.out != nil:

					_, _ = c.out.WriteString(output_)
				default:
					combinedOutput.WriteString(output_)
					c.logger.Debug(output_)
				}
			case <-flushTimer.C:
				c.flushOutput()
			}
		}
	}
}

// streamCmd runs an already-built *exec.Cmd, streaming its combined stdout and
// stderr to outputChan and sending the first error (start or non-zero exit) to
// errChan. Command construction — including the shell-vs-argv decision — happens
// before this point in ExecuteCommand / ExecuteCommandArgs.
func (c *CmdRunner) streamCmd(cmd *exec.Cmd, outputChan chan<- string, errChan chan<- error, envs ...string) {
	cmd.Env = append(os.Environ(), envs...)
	if c.dir != "" {
		cmd.Dir = c.dir
	}

	cmdWriter := &ChanWriter{outputChan}

	cmd.Stdout = cmdWriter
	cmd.Stderr = cmdWriter
	// Bound Wait so a lingering grandchild holding the output pipe (e.g. an SSH
	// ControlMaster from git fetch) cannot stall the run for ControlPersist.
	cmd.WaitDelay = cmdWaitDelay

	err := cmd.Start()
	if err != nil {
		errChan <- errors.Wrap(err, "error starting command")
		return
	}

	defer func() {
		close(outputChan)
		close(errChan)
	}()

	err = cmd.Wait()
	if err != nil {
		// WaitDelay expiring on an otherwise-successful command surfaces as
		// ErrWaitDelay: the process exited 0 but a lingering grandchild kept the
		// output pipe open past the delay. The command did complete, so this is
		// not a failure — swallow it rather than fail the run.
		if errors.Is(err, exec.ErrWaitDelay) {
			c.logger.Debug("command completed; output pipe closed by WaitDelay (a lingering child process held it open)")
			return
		}
		errChan <- err
		return
	}
}

// shellMetaTokens are the shell operators that require a real shell to honor:
// command chaining (`&&`, `||`, `;`), pipes (`|`), redirection (`>`, `<`, and
// thus `&>`), and command substitution (backtick, `$(`). shellwords.Parse does
// NOT interpret these — it silently truncates the token list at `&&`/`|`/`;`
// and drops redirects — so any command relying on them runs as a no-op or with
// the wrong arguments under the shell-less exec path. When one is present we
// hand the whole string to `bash -c` instead.
var shellMetaTokens = []string{"&&", "||", "|", ";", ">", "<", "`", "$("}

// needsShell reports whether commandStr contains a shell operator that the
// shell-less exec path cannot honor. It matches tokens as plain substrings with
// no quoting-context awareness: a string carrying one of these characters only
// inside a quoted argument is still routed to bash (harmless — bash honors the
// quotes), and a caller that interpolates an untrusted value containing one of
// them must shell-quote it, because it will reach `bash -c` (see the ref
// validation in local-ci's githubDataPrepper for the PR-branch case).
func needsShell(commandStr string) bool {
	for _, tok := range shellMetaTokens {
		if strings.Contains(commandStr, tok) {
			return true
		}
	}
	return false
}

// buildCmd turns a command string into an *exec.Cmd. Strings with shell
// operators are run via `bash -c` so chaining, redirection, pipes, and command
// substitution work as written; everything else keeps the shell-less
// shellwords-parsed arg-array exec that other checkers depend on.
func buildCmd(ctx context.Context, commandStr string) (*exec.Cmd, error) {
	if needsShell(commandStr) {

		return exec.CommandContext(ctx, "bash", "-c", commandStr), nil
	}

	parsedStr, parseErr := shellwords.Parse(commandStr)
	if parseErr != nil {
		return nil, errors.Wrap(parseErr, "error parsing command")
	}
	if len(parsedStr) == 0 {
		return nil, fmt.Errorf("no executable parsed from command %q", commandStr)
	}

	return exec.CommandContext(ctx, parsedStr[0], parsedStr[1:]...), nil
}
