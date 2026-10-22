// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package goroutine runs work in a goroutine that recovers and logs a panic
// instead of crashing the process.
package goroutine

import (
	"context"

	"github.com/tetriz-ai/local-ci/internal/logger"
)

// RunWithCtx executes fn in a new goroutine, passing ctx through unchanged. A
// panic inside fn is recovered and logged rather than crashing the process.
func RunWithCtx(ctx context.Context, fn func(ctx context.Context)) {
	go func() {
		defer logger.RecoverPanicAndError(ctx)
		fn(ctx)
	}()
}
