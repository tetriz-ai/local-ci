// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package postprocessor

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/tetriz-ai/local-ci/cliutils"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/filelogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

type storeRunDetailsPostProcessor struct{}

func NewStoreRunDetailsPostProcessor() PostProcessor {
	return &storeRunDetailsPostProcessor{}
}

func (s *storeRunDetailsPostProcessor) GetName() string {
	return "StoreRunDetailsPostProcessor"
}

func (s *storeRunDetailsPostProcessor) Execute(p *projectinfo.ProjectInfo) error {
	outputDirPath := p.OutputDir
	summaryFilePath := filepath.Join(p.OutputDir, "summary.log")

	fLogger, err := filelogger.NewFileLogger(summaryFilePath)
	if err != nil {
		return errors.Wrap(err, "error in creating file logger")
	}
	err = fLogger.LogStructJson(p)
	if err != nil {
		return fmt.Errorf("log struct json: %w", err)
	}

	defer func() {
		err = fLogger.File.Close()
		if err != nil {
			logger.ErrorNoCtx("error in closing file", zap.Error(err))
			return
		}
	}()

	currentTime := time.Now()
	currentDate := currentTime.Format("2006-01-02")
	currentTimeString := currentTime.Format("15-04-05")

	dirName := fmt.Sprintf("%s_%s", currentDate, currentTimeString)
	prHistoryPath, err := cliutils.GetLciRunHistoryDirPath()
	if err != nil {
		return fmt.Errorf("getting local-ci run history dir path: %w", err)
	}
	dirPath := fmt.Sprintf("%s/%d/%s", prHistoryPath, p.UserInput.PrNumber, dirName)
	for _, dir := range []string{filepath.Dir(prHistoryPath), prHistoryPath, filepath.Dir(dirPath), dirPath} {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("creating private history directory: %w", err)
		}
		if err = os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("securing history directory: %w", err)
		}
	}
	files, err := os.ReadDir(outputDirPath)
	if err != nil {
		return fmt.Errorf("reading dir: %w", err)
	}

	for _, file := range files {
		src := fmt.Sprintf("%s/%s", outputDirPath, file.Name())
		dst := fmt.Sprintf("%s/%s", dirPath, file.Name())
		content, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}
		historyLog, err := filelogger.NewFileLogger(dst)
		if err != nil {
			return fmt.Errorf("opening private history log: %w", err)
		}
		_, writeErr := historyLog.File.Write(content)
		closeErr := historyLog.File.Close()
		if writeErr != nil {
			return fmt.Errorf("writing history log: %w", writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("closing history log: %w", closeErr)
		}
	}

	return nil
}

func (s *storeRunDetailsPostProcessor) RunAlways() bool {
	return true
}
