// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package filter

import (
	"github.com/mohae/deepcopy"
	"github.com/pkg/errors"
	"github.com/samber/lo"

	"github.com/tetriz-ai/local-ci/checker/pathhelper"
	"github.com/tetriz-ai/local-ci/internal/globmatcher"
	"github.com/tetriz-ai/local-ci/projectinfo"
	stamperPkg "github.com/tetriz-ai/local-ci/stamper"
	"github.com/tetriz-ai/local-ci/workflow"
)

func filterWfs(modifiedFiles, commonImpactedPaths []string, workflows []*workflow.Workflow) []*workflow.Workflow {
	commonFilteredWorkflows := lo.Filter(workflows, func(wf *workflow.Workflow, _ int) bool {
		return globmatcher.Matches(modifiedFiles, commonImpactedPaths)
	})

	specificFilteredWorkflows := lo.Filter(workflows, func(wf *workflow.Workflow, _ int) bool {
		return globmatcher.Matches(modifiedFiles, pathhelper.ExtractImpactedPaths(wf.Jobs.RunChecks.With.PathsToTrigger))
	})

	return lo.Union(commonFilteredWorkflows, specificFilteredWorkflows)
}

func FilterEligibleWorkflows(stamper stamperPkg.Stamper, p *projectinfo.ProjectInfo, serialWf []*workflow.Workflow, parallelWf []*workflow.Workflow) ([]*workflow.Workflow, []*workflow.Workflow, error) {
	allWfStampKeyToWorkflowMap, err := createWfStampKeyToWfMap(lo.Union(serialWf, parallelWf))
	if err != nil {
		return nil, nil, errors.Wrap(err, "error creating workflow stamp key to workflow map")
	}

	var lastStampPassWf []string
	stamp, stampErr := stamper.RetrieveLastValidStamp(p)
	if stampErr != nil && !errors.Is(stampErr, stamperPkg.NoStampPresentError) {
		return nil, nil, errors.Wrap(stampErr, "error getting last valid stamp")
	}
	lastStampPassWf = getLastStampPassWfStampKeys(stamp)

	// add repo name prefix to the modified files
	overallModifiedFilesWithPrefix, modifiedFilesBetweenCommitWithPrefix := AddRepoPrefixToModifiedFiles(p, p.RepoName)

	// if there is no optimised file changes and last stamp is not present or running in force mode
	// fallback to use only 'paths to trigger' to filter workflows
	if p.UserInput.ForceRunCheck || (!p.DiffData.OptimisedFileChangesPresent && len(lastStampPassWf) == 0) {
		serialWf = filterWfs(overallModifiedFilesWithPrefix, p.CommonImpactedPaths, serialWf)
		parallelWf = filterWfs(overallModifiedFilesWithPrefix, p.CommonImpactedPaths, parallelWf)

		return serialWf, parallelWf, nil
	}

	overallEligibleSerialWf, overallEligibleParallelWf := getEligibleWorkflowStampKeysForDiff(p.CommonImpactedPaths, serialWf, parallelWf, overallModifiedFilesWithPrefix)

	diffEligibleSerialWf := make(map[string]interface{})
	diffEligibleParallelWf := make(map[string]interface{})

	// in case of stamp present on latest commit itself,
	// final eligible workflows = overall eligible - last stamp run
	if p.PrData.LatestCommit != stamp.CommitSHA {
		diffEligibleSerialWf, diffEligibleParallelWf = getEligibleWorkflowStampKeysForDiff(p.CommonImpactedPaths, serialWf, parallelWf, modifiedFilesBetweenCommitWithPrefix)
	}

	// final eligible workflows = overall eligible - last stamp run + diff eligible
	eligibleSerialWfStampKeys := processEligibleWfFiles(lastStampPassWf, overallEligibleSerialWf, diffEligibleSerialWf)
	eligibleParallelWfStampKeys := processEligibleWfFiles(lastStampPassWf, overallEligibleParallelWf, diffEligibleParallelWf)

	eligibleSerialWfs := convertWfStampKeyToWf(eligibleSerialWfStampKeys, allWfStampKeyToWorkflowMap)
	eligibleParallelWfs := convertWfStampKeyToWf(eligibleParallelWfStampKeys, allWfStampKeyToWorkflowMap)

	return eligibleSerialWfs, eligibleParallelWfs, nil
}

func processEligibleWfFiles(lastStampPassWf []string, overallEligibleWf map[string]interface{}, diffEligibleWf map[string]interface{}) map[string]interface{} {
	eligibleWfStampKeys := overallEligibleWf
	for _, wfStampKey := range lastStampPassWf {
		delete(eligibleWfStampKeys, wfStampKey)
	}

	for wfStampKey := range diffEligibleWf {
		if _, ok := eligibleWfStampKeys[wfStampKey]; !ok {
			eligibleWfStampKeys[wfStampKey] = nil
		}
	}
	return eligibleWfStampKeys
}

func convertWfStampKeyToWf(wfStampKeys map[string]interface{}, wfStampKeyToWfMap map[string]*workflow.Workflow) []*workflow.Workflow {
	var wfs []*workflow.Workflow
	for k := range wfStampKeys {
		wf := wfStampKeyToWfMap[k]
		wfs = append(wfs, wf)
	}
	return wfs
}

func getLastStampPassWfStampKeys(stamp *projectinfo.StampInfo) []string {
	var stampWfKeys []string
	if stamp == nil {
		return stampWfKeys
	}

	for key, val := range stamp.ChecksStatus {
		if val {
			stampWfKeys = append(stampWfKeys, key)
		}
	}
	return stampWfKeys
}

func createWfStampKeyToWfMap(workflows []*workflow.Workflow) (map[string]*workflow.Workflow, error) {
	wfStampKeyToWfMap := make(map[string]*workflow.Workflow)
	for _, wf := range workflows {
		stampKey := wf.GetLciStampKey()
		if stampKey == "" {
			return nil, errors.Errorf("local-ci_stamp_key is not set in %s", wf.FileName)
		}
		if existingWf, ok := wfStampKeyToWfMap[stampKey]; !ok {
			wfStampKeyToWfMap[stampKey] = wf
		} else {
			return nil, errors.Errorf("duplicate local-ci_stamp_key %s found in workflows: %s and %s", stampKey, existingWf.FileName, wf.FileName)
		}
	}
	return wfStampKeyToWfMap, nil
}

func getEligibleWorkflowStampKeysForDiff(commonImpactedPaths []string, serialWf []*workflow.Workflow, parallelWf []*workflow.Workflow, modifiedFilesWithRepoPrefix []string) (map[string]interface{}, map[string]interface{}) {
	serialWfStampKeys := map[string]interface{}{}
	parallelWfStampKeys := map[string]interface{}{}

	for _, wf := range serialWf {
		wfImpactedPath := pathhelper.ExtractImpactedPaths(wf.Jobs.RunChecks.With.PathsToTrigger)
		// Ensure matching is performed for both common impacted paths and workflow-specific impacted paths individually.
		// This allows workflows to run even if a path is excluded from common impacted paths but is still relevant for the workflow.
		if !globmatcher.Matches(modifiedFilesWithRepoPrefix, commonImpactedPaths) && !globmatcher.Matches(modifiedFilesWithRepoPrefix, wfImpactedPath) {
			continue
		}
		serialWfStampKeys[wf.GetLciStampKey()] = nil
	}

	for _, wf := range parallelWf {
		wfImpactedPath := pathhelper.ExtractImpactedPaths(wf.Jobs.RunChecks.With.PathsToTrigger)
		// Ensure matching is performed for both common impacted paths and workflow-specific impacted paths individually.
		// This allows workflows to run even if a path is excluded from common impacted paths but is still relevant for the workflow.
		if !globmatcher.Matches(modifiedFilesWithRepoPrefix, commonImpactedPaths) && !globmatcher.Matches(modifiedFilesWithRepoPrefix, wfImpactedPath) {
			continue
		}
		parallelWfStampKeys[wf.GetLciStampKey()] = nil
	}

	return serialWfStampKeys, parallelWfStampKeys
}

func AddRepoPrefixToModifiedFiles(p *projectinfo.ProjectInfo, repoName string) ([]string, []string) {
	diffDataCopy, ok := deepcopy.Copy(p.DiffData).(*projectinfo.DiffData)
	if !ok {
		// deepcopy.Copy preserves the concrete type, so this is unreachable in
		// practice; fall back to the uncopied paths rather than panic.
		return p.DiffData.OverallFilesChanges.ModifiedFiles, p.DiffData.FilesChanges.ModifiedFiles
	}
	if diffDataCopy.OverallFilesChanges != nil && len(diffDataCopy.OverallFilesChanges.ModifiedFiles) > 0 {
		diffDataCopy.OverallFilesChanges.ModifiedFiles = addRepoPrefixToFilesList(diffDataCopy.OverallFilesChanges.ModifiedFiles, repoName)
	}
	if diffDataCopy.FilesChanges != nil && len(diffDataCopy.FilesChanges.ModifiedFiles) > 0 {
		diffDataCopy.FilesChanges.ModifiedFiles = addRepoPrefixToFilesList(diffDataCopy.FilesChanges.ModifiedFiles, repoName)
	}
	return diffDataCopy.OverallFilesChanges.ModifiedFiles, diffDataCopy.FilesChanges.ModifiedFiles
}

func addRepoPrefixToFilesList(files []string, repoName string) []string {
	var filesWithPrefix []string
	for _, file := range files {
		filesWithPrefix = append(filesWithPrefix, repoName+"/"+file)
	}
	return filesWithPrefix
}
