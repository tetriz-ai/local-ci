// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package filelogger

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileLoggerKeepsSummaryPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "summary.log")
			if existing {
				if err := os.WriteFile(path, []byte("old private data"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			log, err := NewFileLogger(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := log.LogStructJson(map[string]string{"patch": "synthetic private patch"}); err != nil {
				t.Fatal(err)
			}
			if err := log.File.Close(); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("summary mode=%o; want 600", got)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "{\n    \"patch\": \"synthetic private patch\"\n}\n" {
				t.Fatalf("summary content=%q", body)
			}
		})
	}
}
