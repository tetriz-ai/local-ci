// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package globmatcher

import (
	"strings"

	"github.com/gobwas/glob"
)

func Matches(srcFiles []string, globPatterns []string) bool {
	if len(srcFiles) == 0 {
		return false
	}

	var negativeGlobs, positiveGlobs []string
	for _, globPattern := range globPatterns {
		if strings.HasPrefix(globPattern, "!") {
			negativeGlobs = append(negativeGlobs, globPattern)
			continue
		}
		positiveGlobs = append(positiveGlobs, globPattern)
	}
	matchedFileSet := map[string]struct{}{}

	// Add all files matching positive glob
	for _, globStr := range positiveGlobs {
		m := glob.MustCompile(globStr)
		for _, srcFile := range srcFiles {
			if !m.Match(srcFile) {
				continue
			}
			matchedFileSet[srcFile] = struct{}{}
		}
	}

	// Remove all files matching negative glob
	for _, globStr := range negativeGlobs {
		m := glob.MustCompile(strings.TrimPrefix(globStr, "!"))
		for _, srcFile := range srcFiles {
			if !m.Match(srcFile) {
				continue
			}
			delete(matchedFileSet, srcFile)
		}
	}

	return len(matchedFileSet) != 0
}
