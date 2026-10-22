// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package projectroot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/tetriz-ai/local-ci/internal/logger"
)

func GetRepoRoot() (string, error) {
	if loggerErr := initLog(); loggerErr != nil {
		return "", errors.Wrap(loggerErr, "error initiating logger")
	}

	wd, wdErr := os.Getwd()
	if wdErr != nil {
		return "", errors.Wrap(wdErr, "getting working directory")
	}
	// Derive the repo root from git, not `go env GOMOD`. local-ci operates on a git
	// repository — it reads the PR diff and stamps the head commit via git notes —
	// so the root is the git worktree root, independent of where (or whether) a
	// go.mod sits. Keying off GOMOD broke every JS-only repo and every monorepo
	// whose go.mod is not at the root: outside a Go module `go env GOMOD` returns
	// the sentinel "/dev/null" (not an empty string, so the guard below never
	// caught it), which the old code mistook for a valid root and then tried to
	// create /dev/null/output.
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "--show-toplevel")
	cmd.Dir = wd
	out, cmdErr := cmd.Output()
	if cmdErr != nil {
		return "", fmt.Errorf("not inside a git repository (run local-ci from a git worktree): %w", cmdErr)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("could not determine git repository root from %q", wd)
	}

	return root, nil
}

// GetMainRepoRoot returns the root of the main worktree, even when called from
// a git worktree. It uses `git rev-parse --git-common-dir` which always points
// to the .git directory of the main repo; taking its parent gives the main root.
// Falls back to repoPath if the git command fails (e.g. non-worktree setup).
func GetMainRepoRoot(repoPath string) string {
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "--git-common-dir")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "git rev-parse --git-common-dir failed, falling back to repoPath %q: %v\n", repoPath, err)
		return repoPath
	}
	gitCommonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitCommonDir) {
		gitCommonDir = filepath.Join(repoPath, gitCommonDir)
	}
	return filepath.Dir(gitCommonDir)
}

// GetRepoName returns the repository name for the repo at repoPath. It prefers
// the name from `git remote origin`, which is stable across git worktrees where
// the worktree directory basename differs from the real repo name (a worktree
// checked out as ".../ship-queue-10457" still has origin ".../core"). It falls
// back to the path basename when the remote is unavailable.
//
// This is the single source of truth for worktree-aware repo-name resolution:
// projectinfo.GetRepoName and the deps impacted-dirs tool both delegate here, so
// the prefix one component prepends to a changed-file path is exactly the prefix
// the other strips. Resolving it two different ways is the defect this consolidates.
func GetRepoName(repoPath string) string {
	name, err := repoNameFromGitRemote(repoPath)
	if err == nil {
		return name
	}
	fmt.Fprintf(os.Stderr, "git remote unavailable for %q, falling back to directory basename: %v\n", repoPath, err)
	return filepath.Base(repoPath)
}

// GetRepoOwner returns the owner (org or user) from the repo's `git remote
// origin` URL — the `<owner>` in `github.com/<owner>/<repo>`. It returns an
// empty string when the remote is unavailable or has no owner segment; callers
// that need the owner (GitHub API calls) treat empty as "unknown".
func GetRepoOwner(repoPath string) string {
	owner, _, err := ownerAndNameFromGitRemote(repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git remote unavailable for %q, repo owner unknown: %v\n", repoPath, err)
		return ""
	}
	return owner
}

// repoNameFromGitRemote extracts the repo name from the `git remote origin` URL.
func repoNameFromGitRemote(repoPath string) (string, error) {
	_, name, err := ownerAndNameFromGitRemote(repoPath)
	return name, err
}

// ownerAndNameFromGitRemote parses the `git remote origin` URL into its owner
// and repo name, handling both SSH (git@github.com:org/repo.git) and HTTPS
// (https://github.com/org/repo.git) forms.
func ownerAndNameFromGitRemote(repoPath string) (owner, name string, err error) {
	cmd := exec.CommandContext(context.Background(), "git", "remote", "get-url", "origin")
	cmd.Dir = repoPath
	out, cmdErr := cmd.Output()
	if cmdErr != nil {
		return "", "", fmt.Errorf("running git remote get-url origin: %w", cmdErr)
	}
	remote := strings.TrimSpace(string(out))
	remote = strings.TrimSuffix(strings.TrimRight(remote, "/"), ".git")
	// Normalise the SSH scp-like form git@host:owner/repo to host/owner/repo so
	// splitting on "/" yields the owner and repo as the last two segments.
	remote = strings.Replace(remote, ":", "/", 1)
	parts := strings.Split(remote, "/")
	if len(parts) < 2 || parts[len(parts)-1] == "" {
		return "", "", fmt.Errorf("unexpected remote format: %s", remote)
	}
	return parts[len(parts)-2], parts[len(parts)-1], nil
}

var (
	devLogOnce sync.Once
	devLogErr  error
)

// initLog installs a dev logger for the platform CLI tools, exactly once per
// process. It must NOT key off `logger.Log != nil` as an "already initialized"
// guard: the global now defaults to a no-op logger (zap.NewNop) so that logging
// before Init is a silent no-op rather than a nil-deref panic, which would make
// that guard permanently true and leave the tools logging into the void.
func initLog() error {
	devLogOnce.Do(func() {
		config := zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)

		built, err := config.Build(zap.AddCallerSkip(1))
		if err != nil {
			devLogErr = fmt.Errorf("build dev logger: %w", err)
			return
		}
		logger.Log = built
		logger.IsDevLogger = true
	})
	return devLogErr
}
