// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prepper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
)

func TestGitDiffTreatsRevisionAsLiteralArgument(t *testing.T) {
	for _, start := range []string{"HEAD~1", "HEAD;printf${IFS}INJECTED;"} {
		t.Run(start, func(t *testing.T) {
			bin := t.TempDir()
			argv := filepath.Join(bin, "args")
			t.Setenv("LCI_TEST_ARGV", argv)
			fake := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$LCI_TEST_ARGV\"\n"
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte(fake), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			runner, err := cmdrunner.NewCmdRunner(bin, cmdrunner.WithNoLogger())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = runner.Close() }()
			d := &diffPrepper{cmdExec: runner}
			if _, err := d.getGitDiffInCommits(start, "HEAD"); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(argv)
			if err != nil {
				t.Fatal(err)
			}
			revision := start + "...HEAD"
			want := strings.Join([]string{"diff", "--name-only", revision, "--", "diff", "--name-only", "--diff-filter=D", revision, "--", "diff", "--name-only", "--diff-filter=A", revision, "--", ""}, "\n")
			if string(body) != want {
				t.Fatalf("git argv = %q; want %q", body, want)
			}
		})
	}
}
