// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/tetriz-ai/local-ci/cliutils"
	"github.com/tetriz-ai/local-ci/internal/logger"
)

func TestRunRootFinalizesOutputBeforeReturning(t *testing.T) {
	tests := []struct {
		name       string
		zip        bool
		history    bool
		explicitPR bool
		wantCode   int
	}{
		{name: "ordinary PR lookup failure", wantCode: 1},
		{name: "zip failure", zip: true, explicitPR: true, wantCode: 1},
		{name: "successful zip utility", zip: true, history: true, explicitPR: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			home := t.TempDir()
			guard := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("GH_CONFIG_DIR", filepath.Join(home, "gh"))
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			if err := os.WriteFile(filepath.Join(guard, "gh"), []byte("#!/bin/sh\necho test-denies-GitHub >&2\nexit 97\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", guard+string(os.PathListSeparator)+os.Getenv("PATH"))
			cmd := exec.Command("git", "init", "-q", repo)
			if body, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git init: %v %s", err, body)
			}
			t.Chdir(repo)
			if tt.history {
				path := filepath.Join(home, "local-ci", "run_history", "123")
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "summary.log"), []byte("synthetic"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			command := &cobra.Command{}
			command.Flags().Bool(debugMode, false, "")
			command.Flags().Int(prNumber, 0, "")
			command.Flags().Bool(zipRunHistory, tt.zip, "")
			if tt.explicitPR {
				if err := command.Flags().Set(prNumber, "123"); err != nil {
					t.Fatal(err)
				}
			}
			code := runRoot(command)
			if code != tt.wantCode {
				t.Fatalf("code=%d want=%d", code, tt.wantCode)
			}
			path := filepath.Join(repo, "output", "local-ci")
			body, err := os.ReadFile(filepath.Join(path, "stdoutput.log"))
			if err != nil {
				t.Fatal(err)
			}
			if tt.history && len(body) == 0 {
				t.Fatal("successful utility log was not flushed")
			}
			if err := cliutils.SetupOutputCliDir(path); err != nil {
				t.Fatalf("completed failure/success did not finalize reusable output: %v", err)
			}
			if logger.Log != nil {
				_ = logger.Log.Sync()
			}
		})
	}
}

func TestGetPRNumberTreatsBranchAsLiteralArgument(t *testing.T) {
	for _, branch := range []string{"feature/ordinary", "topic;printf${IFS}INJECTED"} {
		t.Run(branch, func(t *testing.T) {
			repo, home, bin := t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("GH_CONFIG_DIR", home)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			for _, args := range [][]string{{"init", "-q", repo}, {"-C", repo, "symbolic-ref", "HEAD", "refs/heads/" + branch}} {
				if body, err := exec.Command("git", args...).CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v %s", args, err, body)
				}
			}
			argv := filepath.Join(bin, "args")
			t.Setenv("LCI_TEST_ARGV", argv)
			fake := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$LCI_TEST_ARGV\"\nprintf '[{\"number\":123}]\\n'\n"
			if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fake), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Chdir(repo)
			command := &cobra.Command{}
			command.Flags().Int(prNumber, 0, "")
			got, err := GetPRNumber(command)
			if err != nil || got != 123 {
				t.Fatalf("GetPRNumber() = %d, %v; want 123", got, err)
			}
			body, err := os.ReadFile(argv)
			if err != nil {
				t.Fatal(err)
			}
			if want := "pr\nlist\n--json\nnumber\n-H\n" + branch + "\n"; string(body) != want {
				t.Fatalf("gh argv = %q; want %q", body, want)
			}
		})
	}
}
