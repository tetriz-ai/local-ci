// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadmeChecker_ValidateReadmeContent(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "readme_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	checker := &readmeChecker{}

	tests := []struct {
		name        string
		content     string
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid readme with title and content",
			content: `# Test Package
This is a test package that demonstrates the functionality.
It contains multiple lines of meaningful content.`,
			expectError: false,
		},
		{
			name: "valid readme with multiple sections",
			content: `# Test Package
This package provides testing utilities.

## Features
- Feature 1
- Feature 2

## Usage
Example usage here.`,
			expectError: false,
		},
		{
			name: "invalid readme - too short",
			content: `# Test Package
Short.`,
			expectError: true,
			errorMsg:    "README.md must contain at least 2 lines of meaningful content",
		},
		{
			name: "invalid readme - no title",
			content: `This is a test package.
It contains multiple lines of content.
But no title.`,
			expectError: true,
			errorMsg:    "README.md should have a title",
		},
		{
			name:        "invalid readme - empty",
			content:     ``,
			expectError: true,
			errorMsg:    "README.md must contain at least 2 lines of meaningful content",
		},
		{
			name:        "invalid readme - only title",
			content:     `# Test Package`,
			expectError: true,
			errorMsg:    "README.md must contain at least 2 lines of meaningful content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create README.md file
			readmePath := filepath.Join(tempDir, "README.md")
			err := os.WriteFile(readmePath, []byte(tt.content), 0o644)
			if err != nil {
				t.Fatalf("Failed to write README.md: %v", err)
			}

			// Test validation
			err = checker.validateReadmeContent(readmePath)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error to contain '%s', got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestReadmeChecker_ShouldSkipDirectory(t *testing.T) {
	checker := &readmeChecker{}

	tests := []struct {
		name     string
		dir      string
		expected bool
	}{
		{
			name:     "skip .github",
			dir:      ".github",
			expected: true,
		},
		{
			name:     "skip scripts",
			dir:      "scripts",
			expected: true,
		},
		{
			name:     "skip tools",
			dir:      "tools",
			expected: true,
		},
		{
			name:     "skip output",
			dir:      "output",
			expected: true,
		},
		{
			name:     "don't skip api segment (handled by shouldSkipPath)",
			dir:      "api",
			expected: false,
		},
		{
			name:     "don't skip wire",
			dir:      "wire",
			expected: false,
		},
		{
			name:     "don't skip an ordinary package dir (config)",
			dir:      "config",
			expected: false,
		},
		{
			name:     "don't skip db",
			dir:      "db",
			expected: false,
		},
		{
			name:     "don't skip service directory",
			dir:      "myservice",
			expected: false,
		},
		{
			name:     "don't skip package directory",
			dir:      "pkg",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checker.shouldSkipDirectory(tt.dir)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for directory %s", tt.expected, result, tt.dir)
			}
		})
	}
}

// Note: TestReadmeChecker_GetNewFolders requires a git repository with actual branches
// and is better suited as an integration test. The logic is tested through the
// individual helper function tests above.
