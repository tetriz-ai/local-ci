// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package postprocessor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tetriz-ai/local-ci/projectinfo"
)

func TestStoredRunDetailsRemainPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			home, output := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			const content = "synthetic private PR patch"
			if err := os.WriteFile(filepath.Join(output, "check.log"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			history := filepath.Join(home, "local-ci", "run_history")
			for attempt := 0; attempt < 10; attempt++ {
				runName := time.Now().Format("2006-01-02_15-04-05")
				run := filepath.Join(history, "123", runName)
				if existing {
					if err := os.MkdirAll(run, 0o700); err != nil {
						t.Fatal(err)
					}
					for _, dir := range []string{filepath.Dir(history), history, filepath.Dir(run), run} {
						if err := os.Chmod(dir, 0o755); err != nil {
							t.Fatal(err)
						}
					}
					for _, path := range []string{filepath.Join(output, "summary.log"), filepath.Join(run, "check.log")} {
						if err := os.WriteFile(path, []byte("old data"), 0o600); err != nil {
							t.Fatal(err)
						}
						if err := os.Chmod(path, 0o755); err != nil {
							t.Fatal(err)
						}
					}
				}
				p := &projectinfo.ProjectInfo{OutputDir: output, UserInput: &projectinfo.UserInput{PrNumber: 123}}
				if err := NewStoreRunDetailsPostProcessor().Execute(p); err != nil {
					t.Fatal(err)
				}
				if time.Now().Format("2006-01-02_15-04-05") != runName {
					continue
				}
				for _, dir := range []string{filepath.Dir(history), history, filepath.Dir(run), run} {
					info, err := os.Stat(dir)
					if err != nil {
						t.Fatal(err)
					}
					if got := info.Mode().Perm(); got != 0o700 {
						t.Errorf("directory %s mode=%o; want 700", dir, got)
					}
				}
				for _, path := range []string{filepath.Join(output, "summary.log"), filepath.Join(run, "summary.log"), filepath.Join(run, "check.log")} {
					info, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					if got := info.Mode().Perm(); got != 0o600 {
						t.Errorf("log %s mode=%o; want 600", path, got)
					}
				}
				body, err := os.ReadFile(filepath.Join(run, "check.log"))
				if err != nil {
					t.Fatal(err)
				}
				if string(body) != content {
					t.Fatalf("stored content=%q; want %q", body, content)
				}
				return
			}
			t.Fatal("could not exercise a preexisting run within one timestamp second")
		})
	}
}
