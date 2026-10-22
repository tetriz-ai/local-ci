// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package git

import (
	"strings"

	"github.com/pkg/errors"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
)

// FetchNotesFromRemoteRepo syncs the local git repo with the notes in the remote repo. It handles for
// non-fast-forward rejection error by retrying the operation with force flag.
func FetchNotesFromRemoteRepo(cmdRunner *cmdrunner.CmdRunner) error {
	_, cmdErr := cmdRunner.ExecuteCommand("git fetch origin refs/notes/commits:refs/notes/commits")
	if cmdErr != nil {
		// Check if the error indicates a non-fast-forward rejection
		if strings.Contains(cmdErr.Error(), "non-fast-forward") {
			// Retry the fetch operation with force flag
			_, forceFetchErr := cmdRunner.ExecuteCommand("git fetch -f origin refs/notes/commits:refs/notes/commits")
			if forceFetchErr != nil {
				return errors.Wrap(forceFetchErr, "error force fetching latest commit from origin")
			}
		} else if strings.Contains(cmdErr.Error(), "couldn't find remote ref") {
			// refs/notes/commits doesn't exist in the remote repo yet, skip
			return nil
		} else {
			// Return the original fetch error
			return errors.Wrap(cmdErr, "error fetching latest commit from origin")
		}
	}
	return nil
}
