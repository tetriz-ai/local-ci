// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package checker

import (
	"strings"
	"testing"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
)

// a GraphQL response that carries top-level errors must be treated as a
// failure even though gh exited 0, so the comment-reply gate does not read an
// error payload as "no unanswered threads". A mutation dropping the errors guard
// survived without this test.
func TestFetchReviewThreadPage_GraphQLErrors(t *testing.T) {
	tests := []struct {
		name    string
		ghOut   string
		wantErr bool
	}{
		{name: "top-level errors on exit 0", ghOut: `{"errors":[{"message":"boom"}]}`, wantErr: true},
		{name: "clean empty response", ghOut: `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeBin(t, "gh", "cat <<'EOF'\n"+tt.ghOut+"\nEOF")
			runner, err := cmdrunner.NewCmdRunner(t.TempDir(), cmdrunner.WithNoLogger())
			if err != nil {
				t.Fatal(err)
			}
			_, gotErr := fetchReviewThreadPage(runner, "example-org", "example-repo", 1, "")
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("fetchReviewThreadPage() err = %v, wantErr %v", gotErr, tt.wantErr)
			}
			if tt.wantErr && gotErr != nil && !strings.Contains(gotErr.Error(), "graphql") {
				t.Errorf("error should name the graphql failure, got %v", gotErr)
			}
		})
	}
}
