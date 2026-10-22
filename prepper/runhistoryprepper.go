// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prepper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/tetriz-ai/local-ci/cliutils"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

type runHistoryPrepper struct{}

func NewRunHistoryPrepper() *runHistoryPrepper {
	return &runHistoryPrepper{}
}

func (g *runHistoryPrepper) GetName() string {
	return "RunHistoryPrepper"
}

func (g *runHistoryPrepper) Execute(p *projectinfo.ProjectInfo) error {
	lciHistoryDir, err := cliutils.GetLciRunHistoryDirPath()
	if err != nil {
		return fmt.Errorf("getting local-ci run history dir: %w", err)
	}
	err = cleanLciHistoryDir(lciHistoryDir, 7)
	if err != nil {
		return fmt.Errorf("cleaning local-ci history dir: %w", err)
	}
	return nil
}

func (g *runHistoryPrepper) Cleanup(p *projectinfo.ProjectInfo) error {
	return nil
}

func cleanLciHistoryDir(dir string, retentionDays int) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		fmt.Printf("Directory %s does not exist, nothing to clean.\n", dir)
		return nil
	}
	now := time.Now()
	threshold := now.AddDate(0, 0, -retentionDays)
	var dirsToDelete []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.ModTime().Before(threshold) {
			dirsToDelete = append(dirsToDelete, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking local-ci history dir: %w", err)
	}
	for _, dirPath := range dirsToDelete {
		cmd := exec.CommandContext(context.Background(), "rm", "-rf", dirPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("error executing command: %w\nOutput: %s", err, output)
		}
	}
	return nil
}
