// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"path"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

const LockfileSyncWFName = "lockfile_sync"

const pnpmLockfileName = "pnpm-lock.yaml"

// lockfileSyncChecker detects pnpm-lock.yaml drift: a package.json edited without
// a matching lockfile regeneration. It regenerates the lockfile with `pnpm install
// --lockfile-only` (no node_modules download, so it completes in seconds) and
// fails if the result differs from what is committed.
//
// It is a Serial checker because it mutates the working tree. The regenerated
// lockfile is restored immediately after the drift check — and, as a safety net,
// again in Cleanup — so a later serial checker never sees a dirty tree.
type lockfileSyncChecker struct {
	dir          string
	lockfilePath string
}

// LockfileSyncOpt configures a lockfileSyncChecker.
type LockfileSyncOpt func(*lockfileSyncChecker)

// WithLockfileDir sets the directory, relative to the repo root, the pnpm
// workspace lives in. The default is the repo root.
func WithLockfileDir(dir string) LockfileSyncOpt {
	return func(c *lockfileSyncChecker) { c.dir = dir }
}

// WithLockfilePath overrides the committed lockfile path checked for drift
// (relative to the repo root). The default is <dir>/pnpm-lock.yaml.
func WithLockfilePath(lockfilePath string) LockfileSyncOpt {
	return func(c *lockfileSyncChecker) { c.lockfilePath = lockfilePath }
}

func NewLockfileSyncChecker(opts ...LockfileSyncOpt) *lockfileSyncChecker {
	c := &lockfileSyncChecker{dir: "."}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// lockfilePathFor returns the repo-relative pnpm lockfile path for a workspace
// directory. A forward-slash path (path, not filepath) because it is used as a
// git pathspec. Split out so the derivation is table-testable.
func lockfilePathFor(dir string) string {
	return path.Join(dir, pnpmLockfileName)
}

// lockfile returns the committed lockfile path this checker guards: the explicit
// override when set, else the one derived from the workspace directory.
func (c *lockfileSyncChecker) lockfile() string {
	if c.lockfilePath != "" {
		return c.lockfilePath
	}
	return lockfilePathFor(c.dir)
}

func (c *lockfileSyncChecker) GetName() string {
	return LockfileSyncWFName
}

func (c *lockfileSyncChecker) GetLciStampKey() string {
	return LockfileSyncWFName
}

func (c *lockfileSyncChecker) GetWorkflowNames() []string {
	return []string{LockfileSyncWFName}
}

func (c *lockfileSyncChecker) Execute(p *projectinfo.ProjectInfo) []*Report {
	if err := c.execute(p); err != nil {
		return []*Report{{Name: c.GetName(), Status: Failure, Err: err}}
	}
	return []*Report{{Name: c.GetName(), Status: Success}}
}

func (c *lockfileSyncChecker) execute(p *projectinfo.ProjectInfo) error {
	lockfile := c.lockfile()

	cmdRunner, err := cmdrunner.NewCmdRunner(filepath.Join(p.RepoPath, c.dir), cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "error creating cmd runner")
	}
	defer func() { _ = cmdRunner.Close() }()

	logStep(c, "Regenerating pnpm lockfile (--lockfile-only, skips node_modules)")
	if out, runErr := cmdRunner.ExecuteCommandArgs("pnpm", []string{"install", "--lockfile-only"}); runErr != nil {
		// Restore any partial write before returning so a later serial checker
		// sees a clean tree; Cleanup is the safety net if this restore also fails.
		gitRestoreLockfile(cmdRunner, p.RepoPath, lockfile)
		if isBinaryMissing(execOutputMsg(out, runErr)) {
			return errors.New("pnpm not installed — cannot run the lockfile sync check (install pnpm: https://pnpm.io/installation)")
		}
		return errors.Wrap(runErr, "pnpm install --lockfile-only failed")
	}

	logStep(c, "Checking pnpm-lock.yaml for drift")
	_, diffErr := cmdRunner.ExecuteCommandArgs("git", []string{"-C", p.RepoPath, "diff", "--exit-code", "--", lockfile})

	// Restore immediately — never leave the tree dirty while later serial checkers
	// run; Cleanup is the safety net if this restore fails.
	gitRestoreLockfile(cmdRunner, p.RepoPath, lockfile)

	if diffErr != nil {
		return errors.Wrap(diffErr, "pnpm-lock.yaml is out of sync — run `pnpm install` and commit the refreshed lockfile")
	}

	logStep(c, "pnpm-lock.yaml is in sync")
	return nil
}

func (c *lockfileSyncChecker) Cleanup(p *projectinfo.ProjectInfo) error {
	// Safety net: execute() restores the lockfile immediately after both the diff
	// and any pnpm failure, but Cleanup runs unconditionally via the runner's
	// deferred queue to catch any path that exited before either restore (a panic).
	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "creating cmd runner for cleanup")
	}
	defer func() { _ = cmdRunner.Close() }()
	_, err = cmdRunner.ExecuteCommandArgs("git", []string{"-C", p.RepoPath, "checkout", "--", c.lockfile()})
	return errors.Wrap(err, "restoring pnpm-lock.yaml")
}

// gitRestoreLockfile restores a single committed lockfile with a scoped
// `git checkout -- <lockfile>`. This is a single-path restore of a file this
// checker itself just rewrote — never a tree-wide discard. Errors are logged,
// not returned: the restore is best-effort cleanup beside the real result.
func gitRestoreLockfile(cmdRunner *cmdrunner.CmdRunner, repoPath, lockfile string) {
	if _, err := cmdRunner.ExecuteCommandArgs("git", []string{"-C", repoPath, "checkout", "--", lockfile}); err != nil {
		logger.Warn("could not restore " + lockfile + " after the lockfile sync check: " + err.Error())
	}
}

func (c *lockfileSyncChecker) GetFileFilter(_ *projectinfo.ProjectInfo) []string {
	// Leading "**" so the globs match a manifest anywhere in the tree and absorb
	// any repo-name prefix the eligibility matcher adds.
	return []string{
		"**/package.json",
		"**/pnpm-lock.yaml",
	}
}

func (c *lockfileSyncChecker) GetExecutionMode() ExecutionMode {
	return Serial
}
