// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package postprocessor

import (
	"os"
	"testing"
)

// TestMain points HOME (and USERPROFILE on Windows) at a throwaway directory
// before any test runs, so a test that forgets its fake and shells out to gh or
// git cannot reach the developer's real credentials or config. No test in this
// package should talk to a live GitHub; this is the backstop that enforces it.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "postprocessor-test-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HOME", tmp)
	_ = os.Setenv("USERPROFILE", tmp)
	// A bogus GH_CONFIG_DIR keeps gh from finding a real token even if HOME is honored differently.
	_ = os.Setenv("GH_CONFIG_DIR", tmp)
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}
