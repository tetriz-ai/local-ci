// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package logger is a small zap-backed logging facade used across local-ci. It keeps
// a single process-wide *zap.Logger in Log, defaulting to a no-op so logging
// before Init is a silent no-op rather than a nil-pointer panic.
package logger

import (
	"context"

	"go.uber.org/zap"
)

// Log is the process-wide logger. It defaults to a no-op logger; a command
// entry point replaces it with a configured one (see Init).
var Log = zap.NewNop()

// IsDevLogger reports whether Log is a human-readable development logger.
var IsDevLogger bool

// Init installs a development or production-style logger writing to the given
// paths. debug enables debug-level output. It is safe to call once at startup.
func Init(debug bool, outputPaths ...string) error {
	config := zap.NewDevelopmentConfig()
	if len(outputPaths) > 0 {
		config.OutputPaths = outputPaths
	}
	if !debug {
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}
	config.DisableStacktrace = true

	built, err := config.Build(zap.AddCallerSkip(1))
	if err != nil {
		return err
	}
	Log = built
	IsDevLogger = true
	return nil
}

// DebugNoCtx logs a debug message.
func DebugNoCtx(msg string, fields ...zap.Field) { Log.Debug(msg, fields...) }

// InfoNoCtx logs an info message.
func InfoNoCtx(msg string, fields ...zap.Field) { Log.Info(msg, fields...) }

// ErrorNoCtx logs an error message.
func ErrorNoCtx(msg string, fields ...zap.Field) { Log.Error(msg, fields...) }

// Warn logs a warning message.
func Warn(msg string, fields ...zap.Field) { Log.Warn(msg, fields...) }

// RecoverPanicAndError recovers from a panic in the calling goroutine and logs
// it with a stack trace. Deferred at the top of a goroutine, it keeps a panic
// there from crashing the process. The ctx argument is accepted for call-site
// symmetry; this facade attaches no context fields.
func RecoverPanicAndError(_ context.Context) {
	if r := recover(); r != nil {
		Log.WithOptions(zap.AddStacktrace(zap.ErrorLevel)).Error(
			"recovered from panic",
			zap.Any("recoverMsg", r),
		)
	}
}
