// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/spf13/cobra"

	"github.com/tetriz-ai/local-ci/internal/depsglob"
	"github.com/tetriz-ai/local-ci/internal/projectroot"
)

// modulerootFromGoMod returns the module path from the `module` directive of
// the repo's go.mod — the base import path every package in the repo hangs off.
func modulerootFromGoMod(repo string) (string, error) {
	b, err := os.ReadFile(filepath.Join(repo, "go.mod")) //#nosec G304 -- repo root is resolved by projectroot, not user input
	if err != nil {
		return "", fmt.Errorf("reading go.mod: %w", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "module "); ok {
			if mod := strings.TrimSpace(after); mod != "" {
				return mod, nil
			}
		}
	}
	return "", fmt.Errorf("no module directive in %s/go.mod", repo)
}

// impactedPkgsCmd represents the impacteddirs command
var impactedDirsCmd = &cobra.Command{
	Use:   "impacteddirs",
	Short: "impacteddirs sub command lists all the impacted dirs and impacted go test dirs",
	Long:  `impacteddirs command lists all the impacted dirs and impacted go test dirs.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		modifiedDirs, flagErr := cmd.Flags().GetString("modified_dirs")
		if flagErr != nil {
			panic("Invalid modified files passed: " + flagErr.Error())
		}
		if err := findImpactedDirs(modifiedDirs); err != nil {
			return fmt.Errorf("finding impacted dirs: %w", err)
		}
		return nil
	},
}

func findImpactedDirs(dirs string) error {
	log.SetFlags(log.Lshortfile | log.LstdFlags)
	modifiedDirs := strings.Split(dirs, "\n")
	var changedPkgs string
	var modifiedGoFiles, modifiedGoTestFiles []string
	fileGlobFilter := depsglob.NewFilePathGlobFilter()
	repo, err := projectroot.GetRepoRoot()
	if err != nil {
		return fmt.Errorf("error getting repo root: %w", err)
	}
	// Resolve the repo name the worktree-aware way (git remote origin basename),
	// since changed-file paths may be prefixed with it. filepath.Base(repo) breaks
	// in a git worktree, whose root basename is the worktree dir, not the repo name.
	repoName := projectroot.GetRepoName(repo)

	// The module path comes straight from the repo's go.mod `module` directive,
	// so impacted package paths are correct for any module, not a fixed org.
	modulePath, err := modulerootFromGoMod(repo)
	if err != nil {
		return fmt.Errorf("resolving module path: %w", err)
	}

	// filter directories with go files
	for _, dir := range modifiedDirs {
		normalizedDir := NormalizeModifiedDir(dir, repoName)
		changedPkgs += fmt.Sprintf(modulePath+"/%s ", normalizedDir)
		localDir := "./" + normalizedDir
		goFiles, err := filepath.Glob(filepath.Join(localDir, "*.go"))
		if err != nil {
			return fmt.Errorf("error finding .go files: %w", err)
		}
		goFiles = lo.Filter(goFiles, func(file string, _ int) bool {
			return !strings.HasSuffix(file, "_test.go")
		})
		modifiedGoFiles = append(modifiedGoFiles, goFiles...)

		testFiles, err := fileGlobFilter.Glob(filepath.Join(localDir, "*_test.go"))
		if err != nil {
			return fmt.Errorf("error finding _test.go files: %w", err)
		}
		modifiedGoTestFiles = append(modifiedGoTestFiles, testFiles...)
	}
	// 1. modifiedDirs has all modified directories even those that are completely deleted. This is used to compile the impacted directories and report errors if any.
	// 2. modifiedGoDirs includes only modified directories where go files are present right now and doesn't include deleted.
	// 3. impactedDirs includes all directories that are directly impacted by the change including both modification and deletion
	//
	// modifiedDirs may include directories without any go files. So, we need to filter them out.
	// So we compute the go files in all the modified directories and then compute directories from them to filter out the directories without go files.

	// find unique directories from modifiedGoFiles
	impactedPkg := findImpactedPkg(changedPkgs, modulePath)
	var trimmedImpactedDirs, trimmedImpactedGoTestDirs string
	var impactedGoDirs, impactedGoTestDirs []string
	impactedGoDirs, err = FindImpactedDirsHelper(modifiedGoFiles, impactedPkg, false, fileGlobFilter)
	if err != nil {
		return fmt.Errorf("finding impacted go dirs: %w", err)
	}
	impactedGoDirs, trimmedImpactedDirs, err = MergeSubdirectories(impactedGoDirs)
	if err != nil {
		return errors.Wrap(err, "error merging subdirectories for impactedGoDirs")
	}

	// 1. modifiedGoTestDirs includes only modified directories where go test files are present right now and doesn't include deleted.
	// 2. impactedGoTestDirs includes all test directories that are directly impacted by the change including both modification and deletion
	impactedGoTestDirs, err = FindImpactedDirsHelper(modifiedGoTestFiles, impactedPkg, true, fileGlobFilter)
	if err != nil {
		return fmt.Errorf("finding impacted go test dirs: %w", err)
	}
	impactedGoTestDirs, trimmedImpactedGoTestDirs, err = MergeSubdirectories(impactedGoTestDirs)
	if err != nil {
		return errors.Wrap(err, "error merging subdirectories for impactedGoTestDirs")
	}

	res := &output{
		ImpactedGoTestDirs:        impactedGoTestDirs,
		ImpactedDirs:              impactedGoDirs,
		TrimmedImpactedDirs:       trimmedImpactedDirs,
		TrimmedImpactedGoTestDirs: trimmedImpactedGoTestDirs,
	}

	jsonOutput, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling JSON: %w", err)
	}

	fmt.Println(string(jsonOutput))
	return nil
}

type output struct {
	ImpactedGoTestDirs        []string `json:"impactedGoTestDirs"`
	ImpactedDirs              []string `json:"impactedDirs"`
	TrimmedImpactedDirs       string   `json:"trimmedImpactedDirs"`
	TrimmedImpactedGoTestDirs string   `json:"trimmedImpactedGoTestDirs"`
}

// NormalizeModifiedDir strips a leading repo-name prefix from a changed dir —
// turning "<repo>/pkg/x" into the repo-root-relative "pkg/x" the globs expect; an
// already-relative "pkg/x" passes through. repoName must be the worktree-aware
// name (the git-remote origin basename), not the worktree directory basename, or
// the prefix never matches and nothing strips.
func NormalizeModifiedDir(dir, repoName string) string {
	return strings.TrimPrefix(dir, repoName+"/")
}

// combineAndSortDirectories combines, sorts, and returns unique directories

func combineAndSortDirectories(impactedDirs []string, modifiedDirs []string) []string {
	allDirs := append(impactedDirs, modifiedDirs...)
	sort.Strings(allDirs)
	return unique(allDirs)
}

// unique returns a sorted slice of unique elements
func unique(inp []string) []string {
	uniqMap := make(map[string]struct{}, len(inp))
	for _, val := range inp {
		uniqMap[val] = struct{}{}
	}
	uniqSlice := make([]string, 0, len(uniqMap))
	for val := range uniqMap {
		uniqSlice = append(uniqSlice, val)
	}
	sort.Strings(uniqSlice)
	return uniqSlice
}

// findDir returns a slice of dir of given files
func findDir(files []string) []string {
	var directory []string
	for _, file := range files {
		dir := fmt.Sprintf("./%s/", filepath.Dir(file))
		directory = append(directory, dir)
	}
	return directory
}

//

func init() {
	rootCmd.AddCommand(impactedDirsCmd)
	impactedDirsCmd.Flags().String("modified_dirs", "", "list modified dirs space separated")
}

func findImpactedPkg(changedPkgs, modulePath string) []string {
	args := strings.Split(changedPkgs, " ")
	impactedDirs := FindImpactedPkgsHelper(nil, args)
	for i, dir := range impactedDirs {
		dir = strings.Replace(dir, modulePath, "", 1)
		impactedDirs[i] = "." + dir + "/"
	}
	return impactedDirs
}

func FindImpactedDirsHelper(modifiedFiles []string, impactedPkg []string, testFileMode bool, fileGlobFilter depsglob.IFileGlobFilter) ([]string, error) {
	modifiedFilesDir := findDir(modifiedFiles)
	modifiedDirs := unique(modifiedFilesDir)
	impactedDirs := combineAndSortDirectories(impactedPkg, modifiedDirs)
	if testFileMode {
		return impactedDirs, nil
	}
	// Compiling a directory that holds only test files fails with
	// "no non-test Go files in <dir>", so keep only dirs with a non-test go file.
	filtered := make([]string, 0, len(impactedDirs))
	for _, dir := range impactedDirs {
		goFiles, err := fileGlobFilter.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			return nil, fmt.Errorf("globbing go files in %s: %w", dir, err)
		}
		goTestFiles, err := fileGlobFilter.Glob(filepath.Join(dir, "*_test.go"))
		if err != nil {
			return nil, fmt.Errorf("globbing test files in %s: %w", dir, err)
		}
		if len(goFiles) != len(goTestFiles) {
			filtered = append(filtered, dir)
		}
	}
	return filtered, nil
}
