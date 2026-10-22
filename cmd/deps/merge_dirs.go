// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/tetriz-ai/local-ci/internal/projectroot"
)

// mergeDirsCmd represents the mergeDirsCmd command
var mergeDirsCmd = &cobra.Command{
	Use:   "mergedirs --dirs= ./pay/test/ ./pay/test2/",
	Short: "mergedirs sub command merges the given set of directories into wildcard pattern",
	Long:  `mergedirs command merges the specified directories into a wildcard pattern if all subdirectories containing Go files are present in the list. Otherwise, it adds the subdirectories individually.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dirs, flagErr := cmd.Flags().GetString("dirs")
		if flagErr != nil {
			panic("Invalid dirs passed: " + flagErr.Error())
		}
		dirsList := strings.Split(dirs, " ")
		if err := getMergedDirs(dirsList); err != nil {
			return fmt.Errorf("getting merged dirs: %w", err)
		}
		return nil
	},
}

//

func init() {
	rootCmd.AddCommand(mergeDirsCmd)
	mergeDirsCmd.Flags().String("dirs", "", "Comma-separated list of directories to merge")
}

func getMergedDirs(dirs []string) error {
	mergedDirs, _, err := MergeSubdirectories(dirs)
	if err != nil {
		return fmt.Errorf("merging subdirectories: %w", err)
	}
	mergedDirsString := strings.Join(mergedDirs, " ")
	fmt.Println(mergedDirsString)
	return nil
}

// MergeSubdirectories merges subdirectories into a wildcard pattern if all subdirectories containing Go files are present in the impactedDirs array.
// If not all subdirectories are present, it adds the directory individually.
func MergeSubdirectories(impactedDirs []string) ([]string, string, error) {
	sort.Strings(impactedDirs)
	// ensure all directories end with '/' for consistency
	for i, dir := range impactedDirs {
		if !strings.HasSuffix(dir, "/") {
			impactedDirs[i] = dir + "/"
		}
	}

	// created map for quick lookup
	dirMap := make(map[string]bool)
	for _, dir := range impactedDirs {
		dirMap[dir] = true
	}

	var result []string
	usedDirs := make(map[string]bool)

	// Iterate through impactedDirs to process each directory
	for _, dir := range impactedDirs {
		if usedDirs[dir] {
			continue
		}
		repoRoot, err := projectroot.GetRepoRoot()
		if err != nil {
			return nil, "", errors.Wrap(err, "error in getting repo root")
		}

		baseDir := filepath.Join(repoRoot, dir)
		allSubDirsPresent, baseContainsGoFiles, err := checkSubDirs(baseDir, repoRoot, dirMap)
		if err != nil {
			return nil, "", fmt.Errorf("checking subdirs for %s: %w", baseDir, err)
		}

		// Check if the base directory itself contains Go files
		baseGoFiles, globErr := filepath.Glob(filepath.Join(baseDir, "*.go"))
		if globErr != nil {
			return nil, "", errors.Wrap(globErr, "error globbing base dir go files")
		}
		if len(baseGoFiles) > 0 {
			baseContainsGoFiles = true
		}

		// If the base directory and all subdirectories containing Go files and are present in impactedDirs, we use the wildcard pattern
		if allSubDirsPresent && baseContainsGoFiles {
			result = append(result, fmt.Sprintf("%s...", dir))
			usedDirs[dir] = true
		} else if baseContainsGoFiles {
			// add the base directory individually
			result = append(result, dir)
			usedDirs[dir] = true
		}
	}

	finalResult := removeRedundantDirs(result)
	trimmedImpactedDirs := strings.Join(finalResult, " ")

	return finalResult, trimmedImpactedDirs, nil
}

// checkSubDirs checks if all subdirectories containing Go files are present in the impactedDirs array.
// It also checks if the base directory contains Go files.
func checkSubDirs(baseDir, repoRoot string, dirMap map[string]bool) (bool, bool, error) {
	allSubDirsPresent := true
	baseContainsGoFiles := false

	err := filepath.Walk(baseDir, func(subDir string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() || subDir == baseDir {
			return nil
		}
		goFiles, globErr := filepath.Glob(filepath.Join(subDir, "*.go"))
		if globErr != nil {
			return errors.Wrap(globErr, "error globbing subdir go files")
		}
		if len(goFiles) > 0 {
			subDir = strings.TrimPrefix(subDir, repoRoot)
			subDir = "." + subDir + "/"
			if !dirMap[subDir] {
				allSubDirsPresent = false
			}
		}
		return nil
	})
	if err != nil {
		return false, false, errors.Wrap(err, "error walking through directories")
	}

	baseGoFiles, globErr := filepath.Glob(filepath.Join(baseDir, "*.go"))
	if globErr != nil {
		return false, false, errors.Wrap(globErr, "error globbing base dir go files")
	}
	if len(baseGoFiles) > 0 {
		baseContainsGoFiles = true
	}

	return allSubDirsPresent, baseContainsGoFiles, nil
}

// removeRedundantDirs removes directories that are already covered by wildcard patterns.
// For example, if "./dir/..." exists, it will remove any individual directories under "./dir/".
// Returns a slice of unique directory patterns with redundant entries removed.
func removeRedundantDirs(result []string) []string {
	var finalResult []string
	wildcards := make(map[string]bool)

	for _, entry := range result {
		if strings.HasSuffix(entry, "/...") {
			parentDir := entry[:len(entry)-4]
			wildcards[parentDir] = true
		}
	}

	for _, dir := range result {
		isSubDirOfWildcard := false
		for wildcardPatternDir := range wildcards {
			if strings.HasPrefix(dir, wildcardPatternDir) && dir != wildcardPatternDir+"/..." {
				isSubDirOfWildcard = true
				break
			}
		}
		if !isSubDirOfWildcard {
			finalResult = append(finalResult, dir)
		}
	}
	return finalResult
}
