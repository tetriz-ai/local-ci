// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prepper

import (
	"fmt"
	"strings"

	"github.com/google/go-github/v60/github"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
	stamperPkg "github.com/tetriz-ai/local-ci/stamper"
)

// diffPrepper generates optimised diff for a PR
type diffPrepper struct {
	cmdExec *cmdrunner.CmdRunner
}

func NewDiffPrepper() *diffPrepper {
	return &diffPrepper{}
}

func (d *diffPrepper) GetName() string {
	return "DiffPrepper"
}

func (d *diffPrepper) Execute(p *projectinfo.ProjectInfo) error {
	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "error while trying to create cmd runner")
	}
	defer func() {
		if cerr := cmdRunner.Close(); cerr != nil {
			logger.ErrorNoCtx("closing cmd runner", zap.Error(cerr))
		}
	}()
	d.cmdExec = cmdRunner
	p.DiffData = &projectinfo.DiffData{}

	prFileChanges := d.convertGhCommitToAffectedFiles(p.PrData.PrFiles)
	p.DiffData.FilesChanges = prFileChanges
	p.DiffData.OverallFilesChanges = prFileChanges
	if err := validateAffectedFilesShellSafe(prFileChanges); err != nil {
		return fmt.Errorf("validating pr file changes: %w", err)
	}
	if p.UserInput.ForceRunCheck {
		return nil
	}

	stamper := stamperPkg.GetStamper()
	lastStamp, stampFetchErr := stamper.RetrieveLastValidStamp(p)

	if stampFetchErr != nil {
		// Graceful handle error, fallback to whole PR diff
		logger.InfoNoCtx("Cannot retrieve last valid stamp in PR: " + stampFetchErr.Error())
		return nil
	}
	p.LastValidStamp = *lastStamp

	// if stamp is on current commit only, fallback to whole PR diff
	if p.PrData.LatestCommit == lastStamp.CommitSHA {
		logger.InfoNoCtx("Last stamp is present on same commit")
		return nil
	}

	// check affected files from last stamp version to latest commit
	rangeAffectedFiles, rangeErr := d.getGitDiffInCommits(lastStamp.CommitSHA, p.PrData.LatestCommit)
	if rangeErr != nil {
		logger.ErrorNoCtx("error getting optimised diff from last stamp: ", zap.Error(rangeErr))
		return nil
	}
	if err := validateAffectedFilesShellSafe(rangeAffectedFiles); err != nil {
		return fmt.Errorf("validating range affected files: %w", err)
	}

	// If diff between last stamp commit to current is more than overall pr diff
	// fallback to pr diff to optimistically reduce number of tests to be run
	// assuming test run ∝ files changes
	if len(rangeAffectedFiles.ModifiedFiles) >= len(prFileChanges.ModifiedFiles) {
		return nil
	}
	p.DiffData.FilesChanges = rangeAffectedFiles
	p.DiffData.OptimisedFileChangesPresent = true

	return nil
}

// validateAffectedFilesShellSafe rejects contributor-controlled paths carrying
// shell syntax, except for structurally valid Next.js route-group segments.
func validateAffectedFilesShellSafe(af *projectinfo.AffectedFiles) error {
	if af == nil {
		return nil
	}
	for _, files := range [][]string{af.ModifiedFiles, af.NewFiles, af.RemovedFiles, af.NonRemovedFiles} {
		for _, f := range files {
			if err := validateChangedFilePath(f); err != nil {
				return fmt.Errorf("validating shell-safe file path: %w", err)
			}
		}
	}
	return nil
}

func (d *diffPrepper) convertGhCommitToAffectedFiles(prFiles []*github.CommitFile) *projectinfo.AffectedFiles {
	affectedFiles := &projectinfo.AffectedFiles{}

	for _, affectedFileInfo := range prFiles {
		fileName := affectedFileInfo.GetFilename()
		affectedFiles.ModifiedFiles = append(affectedFiles.ModifiedFiles, fileName)

		// also include previous file name to modified and removed files
		if affectedFileInfo.GetStatus() == "renamed" {
			affectedFiles.ModifiedFiles = append(affectedFiles.ModifiedFiles, affectedFileInfo.GetPreviousFilename())
			affectedFiles.RemovedFiles = append(affectedFiles.RemovedFiles, affectedFileInfo.GetPreviousFilename())
		}

		if affectedFileInfo.GetStatus() == "removed" {
			affectedFiles.RemovedFiles = append(affectedFiles.RemovedFiles, fileName)
		} else {
			affectedFiles.NonRemovedFiles = append(affectedFiles.NonRemovedFiles, fileName)
		}

		// Track newly added files (not modified or renamed)
		if affectedFileInfo.GetStatus() == "added" {
			affectedFiles.NewFiles = append(affectedFiles.NewFiles, fileName)
		}
	}
	return affectedFiles
}

func (d *diffPrepper) getGitDiffInCommits(startCommit, endCommit string) (*projectinfo.AffectedFiles, error) {
	// Get all files changed
	filesDiff, diffCmdErr := d.cmdExec.ExecuteCommandArgs("git", []string{"diff", "--name-only", startCommit + "..." + endCommit, "--"})
	if diffCmdErr != nil {
		return nil, errors.Wrap(diffCmdErr, "error getting files diff from current branch")
	}

	var modifiedFiles []string
	if filesDiff != "" {
		modifiedFiles = strings.Split(filesDiff, "\n")
	}

	// Get only deleted files
	removedFilesDiff, removedDiffCmdErr := d.cmdExec.ExecuteCommandArgs("git", []string{"diff", "--name-only", "--diff-filter=D", startCommit + "..." + endCommit, "--"})
	if removedDiffCmdErr != nil {
		return nil, errors.Wrap(removedDiffCmdErr, "error getting removed files diff from current branch")
	}

	var removedFiles []string
	if removedFilesDiff != "" {
		removedFiles = strings.Split(removedFilesDiff, "\n")
	}

	// Get only added files
	addedFilesDiff, addedDiffCmdErr := d.cmdExec.ExecuteCommandArgs("git", []string{"diff", "--name-only", "--diff-filter=A", startCommit + "..." + endCommit, "--"})
	if addedDiffCmdErr != nil {
		return nil, errors.Wrap(addedDiffCmdErr, "error getting added files diff from current branch")
	}

	var newFiles []string
	if addedFilesDiff != "" {
		newFiles = strings.Split(addedFilesDiff, "\n")
	}

	var nonRemovedFiles []string
	// Create NonRemovedFiles which is a subset of ModifiedFiles excluding RemovedFiles
	// Create a map to store removed files for faster lookup
	removedFileMap := make(map[string]struct{})
	for _, file := range removedFiles {
		removedFileMap[file] = struct{}{}
	}

	// Iterate over modifiedFiles and check if it's not in removedFileMap
	for _, file := range modifiedFiles {
		if _, ok := removedFileMap[file]; !ok {
			nonRemovedFiles = append(nonRemovedFiles, file)
		}
	}

	return &projectinfo.AffectedFiles{
		ModifiedFiles:   modifiedFiles,
		NonRemovedFiles: nonRemovedFiles,
		RemovedFiles:    removedFiles,
		NewFiles:        newFiles,
	}, nil
}

func (d *diffPrepper) Cleanup(p *projectinfo.ProjectInfo) error {
	return nil
}
