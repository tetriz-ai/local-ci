// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package filelogger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/tetriz-ai/local-ci/internal/logger"
)

type FileLogger struct {
	File *os.File
}

func NewFileLogger(filePath string) (*FileLogger, error) {
	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logger.ErrorNoCtx("error creating directory", zap.String("directory", dir), zap.Error(err))
		return nil, errors.Wrap(err, "error in creating output dir")
	}
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, errors.Wrap(err, "error opening file for writing")
	}
	// Tighten an existing log before replacing its contents; creation mode alone
	// leaves old files readable by other users.
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, errors.Wrap(err, "securing log permissions")
	}
	if err := file.Truncate(0); err != nil {
		_ = file.Close()
		return nil, errors.Wrap(err, "truncating log")
	}
	return &FileLogger{File: file}, nil
}

func (fl *FileLogger) LogStructJson(logStruct any) error {
	projectInfoJSON, err := json.MarshalIndent(logStruct, "", "    ")
	if err != nil {
		return fmt.Errorf("marshaling log struct: %w", err)
	}
	_, err = fmt.Fprintf(fl.File, "%s\n", projectInfoJSON)
	if err != nil {
		return fmt.Errorf("writing log line: %w", err)
	}

	return nil
}
