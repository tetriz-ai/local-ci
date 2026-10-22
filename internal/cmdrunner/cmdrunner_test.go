// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package cmdrunner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNeedsShell(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "plain command", command: "git status --porcelain", want: false},
		{name: "flags only", command: "go test -v -race ./...", want: false},
		{name: "double-quoted arg with parens", command: `psql -t -c "select version()"`, want: false},
		{name: "single-quoted arg", command: "gh api -f state='success'", want: false},
		{name: "and chain", command: "cd client && pnpm install", want: true},
		{name: "or chain", command: "false || echo recovered", want: true},
		{name: "pipe", command: "echo hi | tr a-z A-Z", want: true},
		{name: "semicolon", command: "echo a; echo b", want: true},
		{name: "redirect out", command: "echo x > file", want: true},
		{name: "redirect both (ampersand-gt)", command: "which git &> /dev/null", want: true},
		{name: "redirect in", command: "cat < file", want: true},
		{name: "command substitution backtick", command: "echo `date`", want: true},
		{name: "command substitution dollar-paren", command: "echo $(date)", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsShell(tt.command); got != tt.want {
				t.Errorf("needsShell(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

// TestExecuteCommand_ShellOperators is the regression guard for the bug where
// shellwords.Parse truncated `cd <dir> && <cmd>` to `["cd", "<dir>"]`, turning
// every chained/redirected command into a silent no-op that always "passed".
// Each case here would fail on the old shell-less runner.
func TestExecuteCommand_ShellOperators(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// marker.txt exists ONLY inside sub, so reading it succeeds only if the
	// `cd sub` prefix actually took effect.
	if err := os.WriteFile(filepath.Join(sub, "marker.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		command    string
		wantOutput string
		wantErr    bool
	}{
		{
			name:       "cd into subdir then read file (the regression)",
			command:    "cd sub && cat marker.txt",
			wantOutput: "hi",
		},
		{
			name:       "and chain runs both commands",
			command:    "echo first && echo second",
			wantOutput: "first\nsecond",
		},
		{
			name:       "pipe transforms output",
			command:    "echo hello | tr a-z A-Z",
			wantOutput: "HELLO",
		},
		{
			name:       "or chain recovers from failure",
			command:    "false || echo recovered",
			wantOutput: "recovered",
		},
		{
			name:    "and chain short-circuits and propagates failure",
			command: "cd sub && exit 3",
			wantErr: true,
		},
		{
			name:    "cd into missing dir fails (gate is now real)",
			command: "cd does-not-exist && cat marker.txt",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewCmdRunner(root, WithNoLogger())
			if err != nil {
				t.Fatalf("NewCmdRunner: %v", err)
			}
			defer func() { _ = r.Close() }()

			out, runErr := r.ExecuteCommand(tt.command)
			if tt.wantErr {
				if runErr == nil {
					t.Fatalf("ExecuteCommand(%q) = nil error, want failure (silent no-op regression)", tt.command)
				}
				return
			}
			if runErr != nil {
				t.Fatalf("ExecuteCommand(%q) unexpected error: %v", tt.command, runErr)
			}
			if out != tt.wantOutput {
				t.Errorf("ExecuteCommand(%q) output = %q, want %q", tt.command, out, tt.wantOutput)
			}
		})
	}
}

// TestExecuteCommand_ShellLessPathPreserved verifies plain commands (no shell
// operators) still run via the shellwords arg-array exec path unchanged.
func TestExecuteCommand_ShellLessPathPreserved(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		wantOutput string
		wantErr    bool
	}{
		{name: "echo plain words", command: "echo plain words", wantOutput: "plain words"},
		{name: "quoted arg kept as single token", command: `echo "a b c"`, wantOutput: "a b c"},
		{name: "nonzero exit propagates", command: "false", wantErr: true},
		{name: "missing executable errors", command: "definitely-not-a-real-binary-xyz", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewCmdRunner("", WithNoLogger())
			if err != nil {
				t.Fatalf("NewCmdRunner: %v", err)
			}
			defer func() { _ = r.Close() }()

			out, runErr := r.ExecuteCommand(tt.command)
			if tt.wantErr {
				if runErr == nil {
					t.Fatalf("ExecuteCommand(%q) = nil error, want failure", tt.command)
				}
				return
			}
			if runErr != nil {
				t.Fatalf("ExecuteCommand(%q) unexpected error: %v", tt.command, runErr)
			}
			if out != tt.wantOutput {
				t.Errorf("ExecuteCommand(%q) output = %q, want %q", tt.command, out, tt.wantOutput)
			}
		})
	}
}

// TestExecuteCommandSecret_NoLeakOnFailure pins the secret contract on both paths:
// a secret command's output is returned to the caller on success, but on a
// non-zero exit it is never embedded in the returned error (which a caller may
// log or wrap). A plain ExecuteCommand keeps surfacing its output in the error, as
// a diagnostic — the contrast case guards that the redaction is scoped to secret.
func TestExecuteCommandSecret_NoLeakOnFailure(t *testing.T) {
	tests := []struct {
		name           string
		secret         bool
		command        string
		wantErr        bool
		wantOut        string
		errMustNotHave string
		errMustHave    string
	}{
		{
			name:           "secret failing command does not leak output into the error",
			secret:         true,
			command:        "echo SECRET-TOKEN-XYZ; exit 7",
			wantErr:        true,
			errMustNotHave: "SECRET-TOKEN-XYZ",
			errMustHave:    "output suppressed",
		},
		{
			name:        "non-secret failing command surfaces output in the error",
			secret:      false,
			command:     "echo DIAGNOSTIC-INFO; exit 7",
			wantErr:     true,
			errMustHave: "DIAGNOSTIC-INFO",
		},
		{
			name:    "secret succeeding command returns output to the caller",
			secret:  true,
			command: "echo my-token-value",
			wantOut: "my-token-value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewCmdRunner("", WithNoLogger())
			if err != nil {
				t.Fatalf("NewCmdRunner: %v", err)
			}
			var out string
			var runErr error
			if tt.secret {
				out, runErr = r.ExecuteCommandSecret(tt.command)
			} else {
				out, runErr = r.ExecuteCommand(tt.command)
			}
			if tt.wantErr {
				if runErr == nil {
					t.Fatalf("expected error, got nil (out=%q)", out)
				}
				msg := runErr.Error()
				if tt.errMustNotHave != "" && strings.Contains(msg, tt.errMustNotHave) {
					t.Errorf("error leaked %q: %q", tt.errMustNotHave, msg)
				}
				if tt.errMustHave != "" && !strings.Contains(msg, tt.errMustHave) {
					t.Errorf("error missing %q: %q", tt.errMustHave, msg)
				}
				return
			}
			if runErr != nil {
				t.Fatalf("unexpected error: %v", runErr)
			}
			if tt.wantOut != "" && !strings.Contains(out, tt.wantOut) {
				t.Errorf("output = %q, want contains %q", out, tt.wantOut)
			}
		})
	}
}
