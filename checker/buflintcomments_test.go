// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseDiffAddedLines(t *testing.T) {
	tests := []struct {
		name     string
		diff     string
		expected map[int]bool
	}{
		{
			name: "single hunk with additions",
			diff: `--- a/protos/foo.proto
+++ b/protos/foo.proto
@@ -10,6 +10,8 @@ message Foo {
   string id = 1;
   string name = 2;
+  // new_field is the new thing.
+  string new_field = 3;
   string old = 4;
 }`,
			expected: map[int]bool{12: true, 13: true},
		},
		{
			name: "deletion does not increment line counter",
			diff: `--- a/protos/bar.proto
+++ b/protos/bar.proto
@@ -5,7 +5,7 @@ message Bar {
   string a = 1;
-  string removed = 2;
+  // replacement comment
+  string replaced = 2;
   string c = 3;
 }`,
			expected: map[int]bool{6: true, 7: true},
		},
		{
			name: "new file (all additions)",
			diff: `--- /dev/null
+++ b/protos/new.proto
@@ -0,0 +1,5 @@
+syntax = "proto3";
+package foo.v1;
+message New {
+  string id = 1;
+}`,
			expected: map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true},
		},
		{
			name: "multiple hunks",
			diff: `--- a/protos/multi.proto
+++ b/protos/multi.proto
@@ -3,6 +3,7 @@ message A {
   string a = 1;
+  string b = 2;
 }
@@ -20,6 +21,7 @@ message B {
   string x = 1;
+  string y = 2;
 }`,
			expected: map[int]bool{4: true, 22: true},
		},
		{
			name: "hunk header without comma (single line change)",
			diff: `--- a/protos/single.proto
+++ b/protos/single.proto
@@ -10 +10 @@ message Foo {
-  string old = 1;
+  string new = 1;`,
			expected: map[int]bool{10: true},
		},
		{
			name:     "empty diff",
			diff:     "",
			expected: map[int]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDiffAddedLines(tt.diff)
			for line, expected := range tt.expected {
				if result[line] != expected {
					t.Errorf("line %d: got %v, want %v", line, result[line], expected)
				}
			}
			for line := range result {
				if !tt.expected[line] {
					t.Errorf("unexpected added line %d", line)
				}
			}
		})
	}
}

func TestFilterProtoFiles(t *testing.T) {
	files := []string{
		"protos/example/accounts/v1/accounts.proto",
		"gen/example/accounts/v1/accounts.pb.go",
		"protos/example/billing/v1/service.proto",
		"docs/README.md",
		"web/app/src/lib/types.ts",
	}

	result := filterProtoFiles(files)
	if len(result) != 2 {
		t.Fatalf("expected 2 proto files, got %d: %v", len(result), result)
	}
	if result[0] != "protos/example/accounts/v1/accounts.proto" {
		t.Errorf("expected accounts.proto, got %s", result[0])
	}
	if result[1] != "protos/example/billing/v1/service.proto" {
		t.Errorf("expected service.proto, got %s", result[1])
	}
}

func TestBuildBufLintConfig(t *testing.T) {
	tmpDir := t.TempDir()
	bufYAML := `version: v2
modules:
  - path: protos
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
deps:
  - buf.build/bufbuild/protovalidate
  - buf.build/googleapis/googleapis
`
	if err := os.WriteFile(filepath.Join(tmpDir, "buf.yaml"), []byte(bufYAML), 0o644); err != nil {
		t.Fatalf("writing buf.yaml: %v", err)
	}

	config, err := buildBufLintConfig(tmpDir)
	if err != nil {
		t.Fatalf("buildBufLintConfig: %v", err)
	}

	// Verify COMMENT rules were injected.
	for _, rule := range commentRules {
		if !strings.Contains(config, rule) {
			t.Errorf("config missing rule %s", rule)
		}
	}
	// Verify STANDARD is preserved.
	if !strings.Contains(config, "STANDARD") {
		t.Error("config lost STANDARD rule")
	}
	// Verify deps are preserved.
	if !strings.Contains(config, "bufbuild/protovalidate") {
		t.Error("config lost protovalidate dep")
	}
	if !strings.Contains(config, "googleapis/googleapis") {
		t.Error("config lost googleapis dep")
	}
	// Verify it's valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(config), &parsed); err != nil {
		t.Errorf("config is not valid JSON: %v", err)
	}
}

func TestBuildBufLintConfig_MissingFile(t *testing.T) {
	_, err := buildBufLintConfig(t.TempDir())
	require.Error(t, err, "expected error for missing buf.yaml")
}

func TestIsCommentRule(t *testing.T) {
	if !isCommentRule("COMMENT_FIELD") {
		t.Error("COMMENT_FIELD should match")
	}
	if !isCommentRule("COMMENT_MESSAGE") {
		t.Error("COMMENT_MESSAGE should match")
	}
	if isCommentRule("IMPORT_USED") {
		t.Error("IMPORT_USED should not match")
	}
	if isCommentRule("ENUM_PASCAL_CASE") {
		t.Error("ENUM_PASCAL_CASE should not match")
	}
}
