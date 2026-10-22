// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type dummyFileGlobFilter struct {
	filesPresent []string
}

func (d *dummyFileGlobFilter) Glob(pattern string) ([]string, error) {
	matches := []string{}
	for _, file := range d.filesPresent {
		if matched, _ := filepath.Match(strings.TrimPrefix(pattern, "./"), file); matched {
			matches = append(matches, file)
		}
	}
	return matches, nil
}

func Test_FindImpactedDirsHelper(t *testing.T) {
	t.Parallel()

	type args struct {
		filesPresent  []string
		modifiedFiles []string
		impactedPkg   []string
		testFilesMode bool
	}
	type output struct {
		impactedDirs []string
	}
	tests := []struct {
		name string
		args args
		want output
	}{
		{
			name: "duplicate dir in modified files",
			args: args{
				filesPresent:  []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go", "dir4/file4.go", "dir5/file5.go"},
				modifiedFiles: []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go"},
				impactedPkg:   []string{"./dir4/", "./dir5/"},
				testFilesMode: false,
			},
			want: output{
				impactedDirs: []string{"./dir1/", "./dir2/", "./dir3/", "./dir4/", "./dir5/"},
			},
		},
		{
			name: "duplicate dir in modified files (test files mode)",
			args: args{
				filesPresent:  []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go", "dir4/file4.go", "dir5/file5.go"},
				modifiedFiles: []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go"},
				impactedPkg:   []string{"./dir4/", "./dir5/"},
				testFilesMode: true,
			},
			want: output{
				impactedDirs: []string{"./dir1/", "./dir2/", "./dir3/", "./dir4/", "./dir5/"},
			},
		},
		{
			name: "empty impacted pkg",
			args: args{
				filesPresent:  []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go", "dir4/file4.go", "dir5/file5.go"},
				modifiedFiles: []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go"},
				impactedPkg:   []string{},
				testFilesMode: false,
			},
			want: output{
				impactedDirs: []string{"./dir1/", "./dir2/", "./dir3/"},
			},
		},
		{
			name: "empty impacted pkg (test files mode)",
			args: args{
				filesPresent:  []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go", "dir4/file4.go", "dir5/file5.go"},
				modifiedFiles: []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go"},
				impactedPkg:   []string{},
				testFilesMode: true,
			},
			want: output{
				impactedDirs: []string{"./dir1/", "./dir2/", "./dir3/"},
			},
		},
		{
			name: "duplicate dir in impacted pkg",
			args: args{
				filesPresent:  []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go", "dir4/file4.go", "dir5/file5.go"},
				modifiedFiles: []string{"dir1/file1.go", "dir2/file2.go", "dir3/file3.go"},
				impactedPkg:   []string{"./dir4/", "./dir4/", "./dir5/"},
				testFilesMode: false,
			},
			want: output{
				impactedDirs: []string{"./dir1/", "./dir2/", "./dir3/", "./dir4/", "./dir5/"},
			},
		},
		{
			name: "duplicate dir in impacted pkg (test files mode)",
			args: args{
				filesPresent:  []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go", "dir4/file4.go", "dir5/file5.go"},
				modifiedFiles: []string{"dir1/file1.go", "dir2/file2.go", "dir3/file3.go"},
				impactedPkg:   []string{"./dir4/", "./dir4/", "./dir5/"},
				testFilesMode: true,
			},
			want: output{
				impactedDirs: []string{"./dir1/", "./dir2/", "./dir3/", "./dir4/", "./dir5/"},
			},
		},
		{
			name: "empty modified files",
			args: args{
				filesPresent:  []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go", "dir4/file4.go", "dir5/file5.go"},
				modifiedFiles: []string{},
				impactedPkg:   []string{"./dir4/", "./dir5/"},
				testFilesMode: false,
			},
			want: output{
				impactedDirs: []string{"./dir4/", "./dir5/"},
			},
		},
		{
			name: "empty modified files (test files mode)",
			args: args{
				filesPresent:  []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go", "dir4/file4.go", "dir5/file5.go"},
				modifiedFiles: []string{},
				impactedPkg:   []string{"./dir4/", "./dir5/"},
				testFilesMode: true,
			},
			want: output{
				impactedDirs: []string{"./dir4/", "./dir5/"},
			},
		},
		{
			name: "impacted pkgs contains no non go test file",
			args: args{
				filesPresent:  []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go", "dir4/file4.go", "dir5/file5.go"},
				modifiedFiles: []string{"dir1/file1_test.go", "dir2/file2_test.go"},
				impactedPkg:   []string{"./dir6/", "./dir7/"},
				testFilesMode: false,
			},
			want: output{
				impactedDirs: []string{"./dir1/", "./dir2/"},
			},
		},
		{
			name: "impacted pkgs contains no non go test file (test files mode)",
			args: args{
				filesPresent:  []string{"dir1/file1.go", "dir1/file4.go", "dir2/file2.go", "dir3/file3.go", "dir4/file4.go", "dir5/file5.go"},
				modifiedFiles: []string{"dir1/file1_test.go", "dir2/file2_test.go"},
				impactedPkg:   []string{"./dir6/", "./dir7/"},
				testFilesMode: true,
			},
			want: output{
				impactedDirs: []string{"./dir1/", "./dir2/", "./dir6/", "./dir7/"},
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		sort.Strings(tt.want.impactedDirs)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fileGlobFilter := &dummyFileGlobFilter{tt.args.filesPresent}
			impactedDir, err := FindImpactedDirsHelper(tt.args.modifiedFiles, tt.args.impactedPkg, tt.args.testFilesMode, fileGlobFilter)
			if err != nil {
				t.Fatalf("FindImpactedDirsHelper returned error: %v", err)
			}
			sort.Strings(impactedDir)
			if !reflect.DeepEqual(impactedDir, tt.want.impactedDirs) {
				t.Errorf("got = %v, want = %v", impactedDir, tt.want.impactedDirs)
			}
		})
	}
}

func Test_NormalizeModifiedDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		dir      string
		repoName string
		want     string
	}{
		{
			name:     "repo-name-prefixed path strips to repo-relative",
			dir:      "myrepo/pkg/service",
			repoName: "myrepo",
			want:     "pkg/service",
		},
		{
			name:     "already repo-relative path passes through unchanged",
			dir:      "pkg/service",
			repoName: "myrepo",
			want:     "pkg/service",
		},
		{
			// A repoName that does not match the path's leading segment strips
			// nothing: the prefix never matches, so the path passes through as-is.
			// This pins the pure helper's contract — it takes repoName literally;
			// resolving the correct repoName (git-remote name, not a worktree dir
			// basename) is guarded by projectroot's own tests.
			name:     "non-matching repo name leaves the path unchanged",
			dir:      "myrepo/pkg/service",
			repoName: "some-worktree-dir",
			want:     "myrepo/pkg/service",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeModifiedDir(tt.dir, tt.repoName); got != tt.want {
				t.Errorf("NormalizeModifiedDir(%q, %q) = %q, want %q", tt.dir, tt.repoName, got, tt.want)
			}
		})
	}
}

func Test_FilterSubsequentDirs(t *testing.T) {
	t.Parallel()
	type args struct {
		impactedDirs []string
		repoName     string
	}
	type output struct {
		impactedDirs        []string
		trimmedImpactedDirs string
		expectErr           bool
	}
	tests := []struct {
		name string
		args args
		want output
	}{
		{
			name: "all the sub directory is not present",
			args: args{
				impactedDirs: []string{"./cmd/deps/testdata/", "./cmd/deps/testdata/dir1/"},
				repoName:     "core",
			},
			want: output{
				impactedDirs:        []string{"./cmd/deps/testdata/", "./cmd/deps/testdata/dir1/..."},
				trimmedImpactedDirs: "./cmd/deps/testdata/ ./cmd/deps/testdata/dir1/...",
				expectErr:           false,
			},
		},
		{
			name: "all the sub directory is present",
			args: args{
				impactedDirs: []string{"./cmd/deps/testdata/", "./cmd/deps/testdata/dir1/", "./cmd/deps/testdata/dir2/", "./cmd/deps/testdata/dir3/", "./cmd/deps/testdata/dir3/dir4/", "./cmd/deps/testdata/dir3/dir5/"},
				repoName:     "core",
			},
			want: output{
				impactedDirs:        []string{"./cmd/deps/testdata/..."},
				trimmedImpactedDirs: "./cmd/deps/testdata/...",
				expectErr:           false,
			},
		},
		{
			name: "mixture of present and missing sub-directory",
			args: args{
				impactedDirs: []string{"./cmd/deps/testdata/", "./cmd/deps/testdata/dir1/", "./cmd/deps/testdata/dir2/", "./cmd/deps/testdata/dir3/", "./cmd/deps/testdata/dir3/dir4/"},
				repoName:     "core",
			},
			want: output{
				impactedDirs:        []string{"./cmd/deps/testdata/", "./cmd/deps/testdata/dir1/...", "./cmd/deps/testdata/dir2/...", "./cmd/deps/testdata/dir3/", "./cmd/deps/testdata/dir3/dir4/..."},
				trimmedImpactedDirs: "./cmd/deps/testdata/ ./cmd/deps/testdata/dir1/... ./cmd/deps/testdata/dir2/... ./cmd/deps/testdata/dir3/ ./cmd/deps/testdata/dir3/dir4/...",
				expectErr:           false,
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		sort.Strings(tt.want.impactedDirs)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			impactedDirs, trimmedImpactedDirs, err := MergeSubdirectories(tt.args.impactedDirs)
			if (err != nil) != tt.want.expectErr {
				t.Errorf("got = %v, want = %v", err, tt.want.expectErr)
			}
			if !reflect.DeepEqual(impactedDirs, tt.want.impactedDirs) {
				t.Errorf("got = %v, want = %v", impactedDirs, tt.want.impactedDirs)
			}
			if !reflect.DeepEqual(trimmedImpactedDirs, tt.want.trimmedImpactedDirs) {
				t.Errorf("got = %v, want = %v", trimmedImpactedDirs, tt.want.trimmedImpactedDirs)
			}
		})
	}
}
