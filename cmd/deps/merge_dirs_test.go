// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func TestMergedDirectoriesOutputSeparatesPaths(t *testing.T) {
	dirs, output, err := MergeSubdirectories([]string{"./checker/filter", "./pkg/cmdlogger"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 2 {
		t.Fatalf("expected two independent directories, got %v", dirs)
	}
	if want := strings.Join(dirs, " "); output != want {
		t.Fatalf("output = %q, want %q", output, want)
	}
}
