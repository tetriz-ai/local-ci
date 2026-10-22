// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package waitgroup provides a WaitGroup wait bounded by a timeout, so a missed
// Done cannot block a run forever.
package waitgroup

import (
	"context"
	"sync"
	"time"

	"github.com/tetriz-ai/local-ci/internal/logger"
)

// SafeWait waits for wg to finish or for timeout to elapse, whichever comes
// first. It returns true if wg finished before the timeout, false on timeout.
// The waiting goroutine recovers from a panic rather than crashing the process.
func SafeWait(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		defer logger.RecoverPanicAndError(context.Background())
		defer close(done)
		wg.Wait()
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
