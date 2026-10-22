// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"bufio"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

// BufLintCommentsWFName is the workflow name for this checker.
const BufLintCommentsWFName = "buf_lint_comments"

// commentRules are the buf lint rules this checker enforces.
var commentRules = []string{
	"COMMENT_ENUM",
	"COMMENT_ENUM_VALUE",
	"COMMENT_FIELD",
	"COMMENT_MESSAGE",
}

type bufLintCommentsChecker struct{}

func NewBufLintCommentsChecker() *bufLintCommentsChecker {
	return &bufLintCommentsChecker{}
}

func (c *bufLintCommentsChecker) GetName() string            { return BufLintCommentsWFName }
func (c *bufLintCommentsChecker) GetLciStampKey() string     { return BufLintCommentsWFName }
func (c *bufLintCommentsChecker) GetWorkflowNames() []string { return []string{BufLintCommentsWFName} }

func (c *bufLintCommentsChecker) Execute(p *projectinfo.ProjectInfo) []*Report {
	if err := c.execute(p); err != nil {
		return []*Report{{Name: c.GetName(), Status: Failure, Err: err}}
	}
	return []*Report{{Name: c.GetName(), Status: Success}}
}

// bufLintViolation is a single violation from buf lint --error-format json.
type bufLintViolation struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	Type      string `json:"type"`
	Message   string `json:"message"`
}

func (c *bufLintCommentsChecker) execute(p *projectinfo.ProjectInfo) error {
	if p.PrData == nil || p.PrData.BaseBranch == "" {
		logStep(c, "No PR data available, skipping")
		return nil
	}

	affectedFiles := p.DiffData.OverallFilesChanges
	if affectedFiles == nil {
		return nil
	}

	protoFiles := filterProtoFiles(affectedFiles.NonRemovedFiles)
	if len(protoFiles) == 0 {
		logStep(c, "No proto files changed, skipping")
		return nil
	}

	// Fail closed when buf is missing on a proto PR: skipping here would stamp the
	// gate `true` and let a PR through unchecked. proto_breaking fails closed in the
	// same spot; this comment gate must not be the open door beside a closed one.
	if _, err := exec.LookPath("buf"); err != nil {
		return errors.Wrapf(err, "buf CLI not found but %d proto file(s) changed; the proto doc-comment gate cannot be skipped on a proto PR — install buf", len(protoFiles))
	}

	logStep(c, fmt.Sprintf("Checking %d proto file(s) for doc comments", len(protoFiles)))

	// Get all COMMENT violations from buf lint on changed proto files.
	violations, err := runBufLint(p.RepoPath, protoFiles)
	if err != nil {
		return errors.Wrap(err, "running buf lint")
	}
	if len(violations) == 0 {
		logStep(c, "All proto comments present")
		return nil
	}

	// Compute merge base for diff comparison.
	mergeBase := fmt.Sprintf("origin/%s", p.PrData.BaseBranch)

	// Get diff-touched lines (additions only) for each proto file.
	diffLines, err := getDiffAddedLines(p.RepoPath, mergeBase, protoFiles)
	if err != nil {
		return errors.Wrap(err, "error computing diff lines")
	}

	// Intersect: keep only violations on lines that are additions in the diff.
	var newViolations []bufLintViolation
	for _, v := range violations {
		if diffLines[v.Path] != nil && diffLines[v.Path][v.StartLine] {
			newViolations = append(newViolations, v)
		}
	}

	if len(newViolations) == 0 {
		logStep(c, fmt.Sprintf("All %d comment violations are pre-existing (not in diff), passing", len(violations)))
		return nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d new proto doc comment violation(s) on lines you added/modified:\n\n", len(newViolations))
	for _, v := range newViolations {
		fmt.Fprintf(&sb, "  %s:%d [%s] %s\n", v.Path, v.StartLine, v.Type, v.Message)
	}
	sb.WriteString("\nFix: add a // comment on the line above each declaration.\n")
	sb.WriteString("  Message: // MessageName describes [what and when].\n")
	sb.WriteString("  Field:   // field_name is [purpose]. [nil/empty semantics if applicable].\n")
	sb.WriteString("  Enum:    // EnumName classifies [what].\n")
	sb.WriteString("  Value:   // VALUE_NAME means [domain meaning].\n")

	return fmt.Errorf("%s", sb.String())
}

// filterProtoFiles returns the .proto files from the list, regardless of where
// they live in the repository. Covering every changed .proto — not only those
// under a fixed root like protos/ — keeps the proto gates from silently passing
// a PR whose .proto lives elsewhere.
func filterProtoFiles(files []string) []string {
	var result []string
	for _, f := range files {
		if strings.HasSuffix(f, ".proto") {
			result = append(result, f)
		}
	}
	return result
}

// buildBufLintConfig reads buf.yaml from the repo root, adds COMMENT rules to
// the lint.use list, and returns the modified config as JSON for --config.
// This avoids hardcoding deps - changes to buf.yaml deps are picked up automatically.
func buildBufLintConfig(repoPath string) (string, error) {

	data, err := os.ReadFile(filepath.Join(repoPath, "buf.yaml"))
	if err != nil {
		return "", fmt.Errorf("reading buf.yaml: %w", err)
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parsing buf.yaml: %w", err)
	}

	// Add COMMENT rules to lint.use.
	lint, ok := cfg["lint"].(map[string]any)
	if !ok {
		lint = map[string]any{}
		cfg["lint"] = lint
	}
	useList, _ := lint["use"].([]any)
	for _, rule := range commentRules {
		found := false
		for _, u := range useList {
			if u == rule {
				found = true
				break
			}
		}
		if !found {
			useList = append(useList, rule)
		}
	}
	lint["use"] = useList

	out, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshaling config: %w", err)
	}
	return string(out), nil
}

// runBufLint runs buf lint with COMMENT rules on the given proto files.
func runBufLint(repoPath string, protoFiles []string) ([]bufLintViolation, error) {
	inlineConfig, err := buildBufLintConfig(repoPath)
	if err != nil {
		return nil, errors.Wrap(err, "building buf lint config")
	}

	args := []string{"lint", "--error-format", "json", "--config", inlineConfig}
	for _, f := range protoFiles {
		args = append(args, "--path", f)
	}

	cmd := exec.CommandContext(context.Background(), "buf", args...)
	cmd.Dir = repoPath

	// buf exits bufBreakingExitCode (100) with the violations as JSON on stdout
	// when it finds lint issues — that is the expected "violations found" path, so
	// the error is data, not a failure. Any other non-zero exit (buf missing, a
	// crash, a bad config) is operational: fail loud instead of reading empty
	// stdout as "no violations" and passing the gate open.
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if !stderrors.As(err, &exitErr) || exitErr.ExitCode() != bufBreakingExitCode {
			return nil, errors.Wrap(err, "running buf lint (is buf installed and the config valid?)")
		}
	}

	var violations []bufLintViolation
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var v bufLintViolation
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			logger.Warn(fmt.Sprintf("[%s] skipping non-JSON buf lint output line: %v", BufLintCommentsWFName, err))
			continue
		}
		// Only keep COMMENT_* violations. buf lint also reports STANDARD violations
		// (e.g., IMPORT_USED) which are not our concern here.
		if isCommentRule(v.Type) {
			violations = append(violations, v)
		}
	}
	return violations, nil
}

func isCommentRule(ruleType string) bool {
	for _, r := range commentRules {
		if ruleType == r {
			return true
		}
	}
	return false
}

// getDiffAddedLines parses git diff to extract added line numbers per file.
// Returns map[filePath]map[lineNumber]true for lines that are additions.
func getDiffAddedLines(repoPath, mergeBase string, files []string) (map[string]map[int]bool, error) {
	result := make(map[string]map[int]bool)

	cmdRunner, err := cmdrunner.NewCmdRunner(repoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return nil, errors.Wrap(err, "creating command runner")
	}
	defer func() { _ = cmdRunner.Close() }()

	for _, file := range files {
		// Pass the path as a verbatim argv element so a filename containing a space
		// (or any other character) is one argument, never re-split by a shell.
		output, err := cmdRunner.ExecuteCommandArgs("git", []string{"diff", mergeBase + "...HEAD", "--", file})
		if err != nil {
			// Diff error could mean new file not yet committed - treat all lines as added
			logger.Warn(fmt.Sprintf("[%s] git diff for %s failed, treating as new file: %v", BufLintCommentsWFName, file, err))
			continue
		}

		addedLines := parseDiffAddedLines(output)
		if len(addedLines) > 0 {
			result[file] = addedLines
		}
	}

	// For brand new files, all lines are additions.
	for _, file := range files {
		if result[file] != nil {
			continue
		}
		// Check if file is new (exists in working tree but not in merge base).
		// argv form keeps a space-bearing path a single argument.
		if _, showErr := cmdRunner.ExecuteCommandArgs("git", []string{"show", mergeBase + ":" + file}); showErr != nil {
			// A non-nil error here is the expected signal that the file does not
			// exist in the merge base - it's new, so all lines are additions.
			logger.InfoNoCtx(fmt.Sprintf("[%s] %s absent from %s, treating as new file: %v", BufLintCommentsWFName, file, mergeBase, showErr))
			lines, readErr := countFileLines(repoPath, file)
			if readErr != nil {
				// Without a line count the file's violations would be misclassified
				// as pre-existing (a silent false negative), so surface the cause.
				logger.Warn(fmt.Sprintf("[%s] counting lines for new file %s failed; its comment violations may be missed: %v", BufLintCommentsWFName, file, readErr))
				continue
			}
			m := make(map[int]bool, lines)
			for i := 1; i <= lines; i++ {
				m[i] = true
			}
			result[file] = m
		}
	}

	return result, nil
}

// parseDiffAddedLines parses unified diff output and returns a set of line
// numbers (1-based, new-file side) for added lines (lines starting with +).
func parseDiffAddedLines(diff string) map[int]bool {
	result := make(map[int]bool)
	scanner := bufio.NewScanner(strings.NewReader(diff))
	currentLine := 0
	inHunk := false

	for scanner.Scan() {
		line := scanner.Text()

		// Parse hunk header: @@ -a,b +c,d @@ (or @@ -a +c @@)
		if strings.HasPrefix(line, "@@") {
			parts := strings.SplitN(line, "+", 2)
			if len(parts) < 2 {
				continue
			}
			// Extract the number before comma or space: "+10,8 @@" -> "10"
			rest := parts[1]
			rest = strings.SplitN(rest, ",", 2)[0]
			rest = strings.SplitN(rest, " ", 2)[0]
			n, err := strconv.Atoi(rest)
			if err != nil {
				logger.Warn(fmt.Sprintf("[%s] skipping unparseable diff hunk header start line %q: %v", BufLintCommentsWFName, rest, err))
				continue
			}
			currentLine = n
			inHunk = true
			continue
		}

		if !inHunk {
			continue
		}

		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			result[currentLine] = true
			currentLine++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			// Deleted line: no line number on new side, don't increment.
		} else {
			// Context line: increment new-side counter.
			currentLine++
		}
	}

	return result
}

// countFileLines counts lines in a file.
func countFileLines(repoPath, file string) (int, error) {
	// Prefix "./" so a path beginning with "-" (e.g. "-c.proto") is read as a path,
	// not parsed by wc as an option — which would silently drop that file's
	// violations. filepath.Join would re-clean the "./" away, so concatenate.

	cmd := exec.CommandContext(context.Background(), "wc", "-l", "./"+file)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return 0, errors.Wrap(err, "running wc")
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) == 0 {
		return 0, fmt.Errorf("wc output empty")
	}
	count, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, errors.Wrap(err, "parsing wc line count")
	}
	return count, nil
}

func (c *bufLintCommentsChecker) Cleanup(_ *projectinfo.ProjectInfo) error { return nil }

func (c *bufLintCommentsChecker) GetFileFilter(_ *projectinfo.ProjectInfo) []string {
	return []string{"**.proto"}
}

func (c *bufLintCommentsChecker) GetExecutionMode() ExecutionMode { return Serial }
