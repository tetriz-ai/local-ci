// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package stamper

import (
	"os"
	"testing"
)

// TestMain isolates HOME so the notes push/fetch and any gh shell-out in a test
// cannot reach the developer's real git or GitHub credentials.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "stamper-test-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HOME", tmp)
	_ = os.Setenv("USERPROFILE", tmp)
	_ = os.Setenv("GH_CONFIG_DIR", tmp)
	// Deterministic committer identity for git operations under the isolated HOME.
	_ = os.Setenv("GIT_AUTHOR_NAME", "Test")
	_ = os.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	_ = os.Setenv("GIT_COMMITTER_NAME", "Test")
	_ = os.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}
