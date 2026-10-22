// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package pathhelper

import (
	"strings"
)

func ExtractImpactedPaths(pathsToTrigger string) []string {
	var paths []string
	lines := strings.Split(pathsToTrigger, "\n")
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "- ") {
			path := strings.Trim(trimmedLine, "- '")
			paths = append(paths, path)
		}
	}

	return paths
}
