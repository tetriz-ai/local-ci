// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package cmdlogger

import (
	logger "github.com/tetriz-ai/local-ci/internal/logger"
)

type Logger struct{}

func (l Logger) Debug(msg string) {
	logger.DebugNoCtx(msg)
}

func (l Logger) Error(msg string) {
	logger.ErrorNoCtx(msg)
}
