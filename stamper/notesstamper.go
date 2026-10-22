// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package stamper

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mohae/deepcopy"
	"github.com/pkg/errors"
	"golang.org/x/mod/semver"

	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/pkg/git"
	"github.com/tetriz-ai/local-ci/projectinfo"
)

// NoStampPresentError is returned when a commit carries no local-ci stamp. It keeps
// the Error suffix (rather than the errXxx form staticcheck prefers) because it
// is part of the exported API that callers compare against.
var NoStampPresentError error = fmt.Errorf("no stamp found")
var errStampDecode = fmt.Errorf("error decoding stamp info")

// NotesStamper stamps the latest commit through git notes
type NotesStamper struct {
	cmdExec   *cmdrunner.CmdRunner
	lastStamp *projectinfo.StampInfo
}

func (n *NotesStamper) Stamp(p *projectinfo.ProjectInfo) error {
	// fetch latest notes from github
	cmdRunner, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return errors.Wrap(err, "error while trying to create cmd runner")
	}
	defer func() {
		if closeErr := cmdRunner.Close(); closeErr != nil {
			logger.InfoNoCtx("error closing cmd runner: " + closeErr.Error())
		}
	}()
	n.cmdExec = cmdRunner

	// Fetch notes from origin
	cmdErr := git.FetchNotesFromRemoteRepo(cmdRunner)
	if cmdErr != nil {
		return errors.Wrap(cmdErr, "error fetching git note from remote repo")
	}

	stampInfo := n.getStampInfo(p)

	// encode stamp info -> json -> base64
	jsonStampInfo, jsonErr := json.Marshal(stampInfo)
	if jsonErr != nil {
		return errors.Wrap(jsonErr, "error marshalling stamp info to json")
	}
	encodedStamp := base64.StdEncoding.EncodeToString(jsonStampInfo)

	// add notes to commit
	_, cmdErr = cmdRunner.ExecuteCommandArgs("git", []string{"notes", "add", "-f", "-m", encodedStamp, "--", p.PrData.LatestCommit})
	if cmdErr != nil {
		return errors.Wrap(cmdErr, "error stamping current commit")
	}

	// push notes to github
	_, cmdErr = cmdRunner.ExecuteCommandArgs("git", []string{"push", "origin", "refs/notes/commits", "--no-verify"})
	if cmdErr != nil {
		return errors.Wrap(cmdErr, "error pushing stamp to github")
	}

	return nil
}

func (n *NotesStamper) getExistingStampForCommit(latestCommit string) (*projectinfo.StampInfo, error) {
	stampInfo := &projectinfo.StampInfo{}

	note, noteFetchErr := n.cmdExec.ExecuteCommandArgs("git", []string{"notes", "show", "--", latestCommit})
	if noteFetchErr == nil {
		b64DecodedStr, b64DecodeErr := base64.StdEncoding.DecodeString(note)
		if b64DecodeErr != nil {
			logger.DebugNoCtx("error decoding existing stamp: " + b64DecodeErr.Error())
			return stampInfo, errStampDecode
		}
		jsonErr := json.Unmarshal(b64DecodedStr, stampInfo)
		if jsonErr != nil {
			logger.DebugNoCtx("error unmarshalling existing stamp info: " + jsonErr.Error())
			return stampInfo, errStampDecode
		}
		if stampInfo.CommitSHA != latestCommit {
			return nil, fmt.Errorf("stamp commit does not match queried commit: %w", errStampDecode)
		}
		return stampInfo, nil
	}

	if strings.Contains(note, "no note found") {
		return stampInfo, NoStampPresentError
	}
	return stampInfo, fmt.Errorf("unhandled error while getting git note: %w", noteFetchErr)
}

func (n *NotesStamper) getStampInfo(p *projectinfo.ProjectInfo) *projectinfo.StampInfo {
	stampInfo := &projectinfo.StampInfo{
		CommitSHA:    p.PrData.LatestCommit,
		ChecksStatus: p.CheckerStatus,
		Version:      p.ToolInfo.Version,
		BaseBranch:   p.PrData.BaseBranch,
	}

	var refStamp *projectinfo.StampInfo

	if n.lastStamp != nil {
		copied, ok := deepcopy.Copy(n.lastStamp).(*projectinfo.StampInfo)
		if !ok {
			logger.InfoNoCtx("deep copy of last stamp returned unexpected type, using freshly built stamp info")
			return stampInfo
		}
		refStamp = copied
	} else {
		existingStamp, stampErr := n.getExistingStampForCommit(p.PrData.LatestCommit)
		if stampErr != nil {
			logger.InfoNoCtx("no usable existing stamp for commit, using freshly built stamp info: " + stampErr.Error())
			return stampInfo
		}
		copied, ok := deepcopy.Copy(existingStamp).(*projectinfo.StampInfo)
		if !ok {
			logger.InfoNoCtx("deep copy of existing stamp returned unexpected type, using freshly built stamp info")
			return stampInfo
		}
		refStamp = copied
	}

	// overall logic
	// checks status = checks status from previous stamp - workflows eligible on the diff + workflow checks run this time
	if refStamp != nil {
		// copy status from existing
		if len(refStamp.ChecksStatus) != 0 {
			stampInfo.ChecksStatus = refStamp.ChecksStatus
		}
	}

	// Invalidate by the result key, which may differ from the checker name.
	for _, wf := range p.AffectedWorkflows {
		delete(stampInfo.ChecksStatus, wf.GetLciStampKey())
	}

	// overwrite status from current run
	for check, status := range p.CheckerStatus {
		stampInfo.ChecksStatus[check] = status
	}

	return stampInfo
}

func (n *NotesStamper) RetrieveLastValidStamp(p *projectinfo.ProjectInfo) (*projectinfo.StampInfo, error) {
	if n.lastStamp != nil {
		return n.lastStamp, nil
	}

	cmdExec, err := cmdrunner.NewCmdRunner(p.RepoPath, cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return nil, errors.Wrap(err, "error while trying to create cmd runner")
	}
	defer func() {
		if closeErr := cmdExec.Close(); closeErr != nil {
			logger.InfoNoCtx("error closing cmd runner: " + closeErr.Error())
		}
	}()
	n.cmdExec = cmdExec

	// get commits from PR data
	if p.PrData == nil || len(p.PrData.AllCommits) == 0 {
		return nil, fmt.Errorf("missing commits prerequisite for getting last stamp, make sure prepper to populate pr data is run")
	}

	commitsList := p.PrData.AllCommits
	for l := len(commitsList) - 1; l >= 0; l-- {
		stamp, err := n.getExistingStampForCommit(commitsList[l])
		if err != nil {
			logger.InfoNoCtx("no valid stamp on commit " + commitsList[l] + ", trying earlier commit: " + err.Error())
			continue
		}

		// for cases in stacked PR, where previously the base was another PR but now the base became master
		// The experience is the same as github where changing the branch invalidates the checks
		if stamp.BaseBranch != p.PrData.BaseBranch {
			continue
		}

		// Release binaries omit the v prefix; semver requires it. Invalid versions
		// must never compare as matching merely because both parse to empty strings.
		stampedVersion := "v" + strings.TrimPrefix(stamp.Version, "v")
		currentVersion := "v" + strings.TrimPrefix(p.ToolInfo.Version, "v")
		if semver.IsValid(stampedVersion) && semver.IsValid(currentVersion) &&
			semver.MajorMinor(stampedVersion) == semver.MajorMinor(currentVersion) {
			if semver.Compare(stampedVersion, currentVersion) == -1 {
				logger.InfoNoCtx("Found existing stamp with lower patch version, considering it as valid stamp")
			}
			n.lastStamp = stamp
			return stamp, nil
		}

	}
	return nil, NoStampPresentError
}
