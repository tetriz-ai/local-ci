// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package stamper

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

func TestGetExistingStampForCommit_CommitBinding(t *testing.T) {
	repo, sha := newRepoWithRemote(t)
	runner, err := cmdrunner.NewCmdRunner(repo, cmdrunner.WithNoLogger())
	if err != nil {
		t.Fatal(err)
	}
	n := &NotesStamper{cmdExec: runner}
	for _, claimed := range []string{"", "deadbeef", sha + "; touch injected"} {
		writeRawStamp(t, repo, sha, projectinfo.StampInfo{CommitSHA: claimed, Version: "0.1.0", BaseBranch: "main"})
		if _, err := n.getExistingStampForCommit(sha); err == nil {
			t.Fatalf("accepted note with commitSha %q for queried commit %q", claimed, sha)
		}
	}
	marker := filepath.Join(repo, "injected")
	if _, err := n.getExistingStampForCommit(sha + "; touch " + marker); err == nil {
		t.Fatal("accepted invalid commit query")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("commit query executed shell syntax: %v", err)
	}
}

func writeRawStamp(t *testing.T, repo, sha string, stamp projectinfo.StampInfo) {
	t.Helper()
	data, err := json.Marshal(stamp)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "notes", "add", "-f", "-m", base64.StdEncoding.EncodeToString(data), sha)
}
