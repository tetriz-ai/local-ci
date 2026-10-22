// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

const ReadmeCheckerWFName = "readme_checker"

// readmeChecker checks if new folders have README.md files with proper content
type readmeChecker struct {
}

func NewReadmeChecker() *readmeChecker {
	return &readmeChecker{}
}

func (r *readmeChecker) GetName() string {
	return ReadmeCheckerWFName
}

func (r *readmeChecker) GetLciStampKey() string {
	return ReadmeCheckerWFName
}

func (r *readmeChecker) GetWorkflowNames() []string {
	return []string{ReadmeCheckerWFName}
}

func (r *readmeChecker) Execute(p *projectinfo.ProjectInfo) []*Report {
	err := r.execute(p)
	if err != nil {
		return []*Report{{Name: r.GetName(), Status: Failure, Err: err}}
	}
	return []*Report{{Name: r.GetName(), Status: Success}}
}

func (r *readmeChecker) execute(p *projectinfo.ProjectInfo) error {
	if p.DiffData == nil || p.DiffData.OverallFilesChanges == nil {
		return fmt.Errorf("missing diff data in project info")
	}

	affectedFiles := p.DiffData.OverallFilesChanges

	// Get all new folders created in this PR
	newFolders, err := r.getNewFolders(p, affectedFiles)
	if err != nil {
		return errors.Wrap(err, "error getting new folders")
	}

	if len(newFolders) == 0 {
		logger.InfoNoCtx(fmt.Sprintf("[%s] No new folders detected in this PR", r.GetName()))
		return nil
	}

	logger.InfoNoCtx(fmt.Sprintf("[%s] Found %d new folders to check: %v", r.GetName(), len(newFolders), newFolders))

	// Check each new folder for README.md
	var missingReadmes []string
	var invalidReadmes []string

	for _, folder := range newFolders {
		readmePath := filepath.Join(p.RepoPath, folder, "README.md")

		// Check if README.md exists
		if _, err := os.Stat(readmePath); os.IsNotExist(err) {
			missingReadmes = append(missingReadmes, folder)
			continue
		}

		// Check README.md content
		if err := r.validateReadmeContent(readmePath); err != nil {
			invalidReadmes = append(invalidReadmes, fmt.Sprintf("%s: %v", folder, err))
		}
	}

	// Report errors
	if len(missingReadmes) > 0 || len(invalidReadmes) > 0 {
		var errorMsg strings.Builder
		errorMsg.WriteString("README.md validation failed:\n\n")

		if len(missingReadmes) > 0 {
			errorMsg.WriteString("Missing README.md files in new folders:\n")
			for _, folder := range missingReadmes {
				fmt.Fprintf(&errorMsg, "  - %s/\n", folder)
			}
			errorMsg.WriteString("\n")
		}

		if len(invalidReadmes) > 0 {
			errorMsg.WriteString("Invalid README.md content:\n")
			for _, invalid := range invalidReadmes {
				fmt.Fprintf(&errorMsg, "  - %s\n", invalid)
			}
			errorMsg.WriteString("\n")
		}

		errorMsg.WriteString("Rationale: Documentation is essential for code maintainability and helps both humans and AI tools understand the codebase better.\n")
		errorMsg.WriteString("Please add a README.md file with at least 2 lines of meaningful documentation to each new folder.\n")
		// The example must itself satisfy the rule: at least 2 non-header lines
		// (validateReadmeContent ignores the "# ..." heading). A one-line body here
		// would fail the very check it illustrates.
		errorMsg.WriteString("Example README.md content:\n")
		errorMsg.WriteString("```\n")
		errorMsg.WriteString("# Folder Name\n")
		errorMsg.WriteString("Brief description of what this folder contains and its purpose.\n")
		errorMsg.WriteString("Note any entry points, key files, or how this folder is used.\n")
		errorMsg.WriteString("```")

		return fmt.Errorf("%s", errorMsg.String())
	}

	logger.InfoNoCtx(fmt.Sprintf("[%s] All new folders have valid README.md files", r.GetName()))
	return nil
}

func (r *readmeChecker) Cleanup(p *projectinfo.ProjectInfo) error {
	return nil
}

func (r *readmeChecker) GetFileFilter(p *projectinfo.ProjectInfo) []string {
	return []string{
		"**/README.md",
		"**/*.go",
		"**/*.yml",
		"**/*.yaml",
		"**/*.json",
		"**/*.proto",
	}
}

func (r *readmeChecker) GetExecutionMode() ExecutionMode {
	return Serial
}

// getNewFolders identifies folders that are newly created in this PR
// A folder is considered "new" if it didn't exist in the base branch
// This checks all directory levels, not just root-level directories
func (r *readmeChecker) getNewFolders(p *projectinfo.ProjectInfo, affectedFiles *projectinfo.AffectedFiles) ([]string, error) {
	folderMap := make(map[string]bool)

	// Use NewFiles if available (more efficient as it only contains added files)
	// Otherwise fall back to NonRemovedFiles for backward compatibility
	filesToCheck := affectedFiles.NewFiles
	if len(filesToCheck) == 0 {
		filesToCheck = affectedFiles.NonRemovedFiles
	}

	// Process new/non-removed files to identify potential new folders
	for _, file := range filesToCheck {
		// Skip if it's a README.md file itself
		if strings.HasSuffix(file, "README.md") {
			continue
		}

		// Get the directory path
		dir := filepath.Dir(file)
		if dir == "." {
			continue // Skip root level files
		}

		// Check if any directory component should be skipped
		parts := strings.Split(dir, string(filepath.Separator))
		skip := false
		for _, part := range parts {
			if r.shouldSkipDirectory(part) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if r.shouldSkipPath(dir) {
			continue
		}

		// Add the directory itself (could be any depth)
		folderMap[dir] = true
	}

	// Now check which of these folders are actually new
	var newFolders []string
	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return nil, errors.Wrap(err, "error creating cmd runner")
	}
	defer func() {
		if closeErr := cmdRunner.Close(); closeErr != nil {
			logger.ErrorNoCtx(fmt.Sprintf("closing command runner: %v", closeErr))
		}
	}()

	baseBranch := p.PrData.BaseBranch
	if baseBranch == "" {
		return nil, fmt.Errorf("base branch not found in project info")
	}

	for folder := range folderMap {
		isNew, err := r.isFolderNew(cmdRunner, folder, baseBranch)
		if err != nil {
			logger.Warn(fmt.Sprintf("[%s] Error checking if folder %s is new: %v", r.GetName(), folder, err))
			continue
		}
		if isNew {
			newFolders = append(newFolders, folder)
		}
	}

	return newFolders, nil
}

// shouldSkipDirectory checks if a directory should be skipped from README checks
func (r *readmeChecker) shouldSkipDirectory(dir string) bool {
	// Common non-package top-level directories a Go repository does not need a
	// per-folder README for. Kept generic — a project with its own layout adds
	// its own names here.
	skipDirs := []string{
		".",
		".git",
		".github",
		"node_modules",
		"vendor",
		"testdata",
		"test",
		"testing",
		"gen", // generated code
		"output",
		"build",
		"dist",
		"docs",
		"scripts",
		"tools",
	}

	for _, skipDir := range skipDirs {
		if dir == skipDir {
			return true
		}
	}
	return false
}

// shouldSkipPath exempts specific vendored or generated path prefixes from
// README enforcement. Use this instead of adding generic segment names to
// shouldSkipDirectory when the exemption should apply only under a specific
// parent path — otherwise a segment like "api" would silently exempt every
// repo path containing it (e.g. services/foo/api).
func (r *readmeChecker) shouldSkipPath(dir string) bool {
	skipPrefixes := []string{
		"proto-deps" + string(filepath.Separator), // vendored proto deps (googleapis/google/api, etc.)
	}
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(dir+string(filepath.Separator), prefix) {
			return true
		}
	}
	return false
}

// isFolderNew checks if a folder existed in the base branch
// Returns true if the folder is new (didn't exist in base branch)
func (r *readmeChecker) isFolderNew(cmdRunner *cmdrunner.CmdRunner, folder string, baseBranch string) (bool, error) {
	// Check if any files existed in this folder in the base branch
	// git ls-tree will list files in the tree, returns empty if folder doesn't exist.
	// Pass the folder as a verbatim argv element so a space in the path is one
	// argument, not re-split by a shell.
	output, err := cmdRunner.ExecuteCommandArgs("git", []string{"ls-tree", "-r", baseBranch, "--name-only", "--", folder + "/"})
	if err != nil {
		// A failure here usually means the folder doesn't exist in the base branch
		// (so it's new), but it could also be a git error; treat as potentially new
		// and surface the cause so a genuine git fault isn't masked.
		logger.Warn(fmt.Sprintf("[%s] ls-tree for %s on %s failed, treating folder as new: %v", ReadmeCheckerWFName, folder, baseBranch, err))
		return true, nil
	}

	// If output is empty, the folder didn't exist in base branch
	return strings.TrimSpace(output) == "", nil
}

// validateReadmeContent checks if README.md has meaningful content
func (r *readmeChecker) validateReadmeContent(readmePath string) error {
	content, err := os.ReadFile(filepath.Clean(readmePath))
	if err != nil {
		return errors.Wrap(err, "failed to read README.md")
	}

	lines := strings.Split(string(content), "\n")

	// Count non-empty lines
	nonEmptyLines := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			nonEmptyLines++
		}
	}

	// Check for minimum content requirement
	if nonEmptyLines < 2 {
		return fmt.Errorf("README.md must contain at least 2 lines of meaningful content (excluding headers and empty lines), found %d", nonEmptyLines)
	}

	// Check for basic structure (should have a title)
	hasTitle := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && len(trimmed) > 1 {
			hasTitle = true
			break
		}
	}

	if !hasTitle {
		return fmt.Errorf("README.md should have a title (line starting with #)")
	}

	return nil
}
