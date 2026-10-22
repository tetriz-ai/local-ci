// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package cliutils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
)

func CheckCommandPresent(cmdExec cmdrunner.CommandRunner, command string) (bool, error) {
	_, err := cmdExec.ExecuteCommand(fmt.Sprintf("which %s &> /dev/null", command))
	if err != nil {
		var customErr *cmdrunner.CustomExitErr
		if !errors.As(err, &customErr) {
			return false, errors.Wrap(err, "error checking if command is present")
		}
		if customErr.GetExitCode() == 1 {
			return false, nil
		}
		return false, errors.Wrap(customErr, "unhandled error checking if command is present")

	}
	return true, nil
}

func ZipDirectory(pr int) error {
	rootDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(err, "resolving user home dir")
	}
	dirPath := fmt.Sprintf("%s/local-ci/run_history/%d", rootDir, pr)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		return fmt.Errorf("run history directory does not exist: %s", dirPath)
	}
	// Write the archive beside the history folder, not inside it: running zip from
	// the parent with a relative source keeps the <pr>.zip out of the tree it is
	// archiving (otherwise the zip lands in the folder it zips) and stores relative
	// paths rather than absolute ones.
	parent := filepath.Dir(dirPath)
	zipName := fmt.Sprintf("%d.zip", pr)
	zipPath := filepath.Join(parent, zipName)
	cmd := exec.CommandContext(context.Background(), "zip", "-r", zipName, filepath.Base(dirPath))
	cmd.Dir = parent

	err = cmd.Run()
	if err != nil {
		return errors.Wrap(err, "running zip command")
	}
	logger.InfoNoCtx("Zipped run history successfully", zap.String("zip", zipPath))
	return nil
}

func GetLciRunHistoryDirPath() (string, error) {
	rootDir, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "resolving user home dir")
	}
	dirPath := fmt.Sprintf("%s/local-ci/run_history", rootDir)
	return dirPath, nil
}
