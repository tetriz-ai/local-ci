// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package filter

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/samber/lo"
	"go.uber.org/mock/gomock"

	"github.com/tetriz-ai/local-ci/internal/stamper/mocks"
	"github.com/tetriz-ai/local-ci/projectinfo"
	stamperPkg "github.com/tetriz-ai/local-ci/stamper"
	"github.com/tetriz-ai/local-ci/workflow"
)

func Test_filterEligibleWorkflows(t *testing.T) {
	type wfArgs struct {
		name     string
		filter   string
		stampKey string
	}

	type args struct {
		optimisedFileChangesPresent bool
		serialWfs                   []wfArgs
		parallelWfs                 []wfArgs
		runForceMode                bool
	}
	type wantWfs struct {
		serialWfNames   []string
		parallelWfNames []string
	}
	tests := []struct {
		name       string
		args       args
		wantWfs    wantWfs
		setupMocks func(s *mocks.MockStamper)
		wantErr    bool
	}{
		{
			name: "no last stamp is present, should only filter wf on file filter",
			args: args{
				serialWfs: []wfArgs{
					{
						name:     "serial1",
						filter:   "repo/overall/**.go",
						stampKey: "serial1",
					},
					{
						name:     "serial2",
						filter:   "**.sql",
						stampKey: "serial2",
					},
				},
				parallelWfs: []wfArgs{
					{
						name:     "parallel1",
						filter:   "repo/overall/**.go",
						stampKey: "parallel1",
					},
				},
				optimisedFileChangesPresent: false,
			},
			wantWfs: wantWfs{
				serialWfNames:   []string{"serial1"},
				parallelWfNames: []string{"parallel1"},
			},
			setupMocks: func(s *mocks.MockStamper) {
				s.EXPECT().RetrieveLastValidStamp(gomock.Any()).Times(1).Return(nil, stamperPkg.NoStampPresentError)
			},
		},
		{
			name: "error in getting last stamp, should return error",
			args: args{
				serialWfs: []wfArgs{
					{
						name:     "serial1",
						filter:   "repo/overall/**.go",
						stampKey: "serial1",
					},
					{
						name:     "serial2",
						filter:   "**.sql",
						stampKey: "serial2",
					},
				},
				parallelWfs: []wfArgs{
					{
						name:     "parallel1",
						filter:   "repo/overall/**.go",
						stampKey: "parallel1",
					},
				},
				optimisedFileChangesPresent: false,
			},
			wantWfs: wantWfs{},
			setupMocks: func(s *mocks.MockStamper) {
				s.EXPECT().RetrieveLastValidStamp(gomock.Any()).Times(1).Return(nil, fmt.Errorf("some dummy error"))
			},
			wantErr: true,
		},
		{
			name: "no runs present on last stamp, should only filter wf on file filter",
			args: args{
				serialWfs: []wfArgs{
					{
						name:     "serial1",
						filter:   "repo/overall/**.go",
						stampKey: "serial1",
					},
					{
						name:     "serial2",
						filter:   "**.sql",
						stampKey: "serial2",
					},
				},
				parallelWfs: []wfArgs{
					{
						name:     "parallel1",
						filter:   "repo/overall/**.go",
						stampKey: "parallel1",
					},
				},
				optimisedFileChangesPresent: false,
			},
			wantWfs: wantWfs{
				serialWfNames:   []string{"serial1"},
				parallelWfNames: []string{"parallel1"},
			},
			setupMocks: func(s *mocks.MockStamper) {
				s.EXPECT().RetrieveLastValidStamp(gomock.Any()).Times(1).Return(&projectinfo.StampInfo{
					CommitSHA:    "dummy",
					ChecksStatus: map[string]bool{},
				}, nil)
			},
		},
		{
			name: "diff does not trigger any checker, last stamp contains no runs then filter all wfs on file filter",
			args: args{
				serialWfs: []wfArgs{
					{
						name:     "serial1",
						filter:   "repo/overall/**.go",
						stampKey: "serial1",
					},
					{
						name:     "serial2",
						filter:   "**.sql",
						stampKey: "serial2",
					},
				},
				parallelWfs: []wfArgs{
					{
						name:     "parallel1",
						filter:   "repo/overall/**.go",
						stampKey: "parallel1",
					},
				},
				optimisedFileChangesPresent: true,
			},
			wantWfs: wantWfs{
				serialWfNames:   []string{"serial1"},
				parallelWfNames: []string{"parallel1"},
			},
			setupMocks: func(s *mocks.MockStamper) {
				s.EXPECT().RetrieveLastValidStamp(gomock.Any()).Times(1).Return(&projectinfo.StampInfo{
					CommitSHA:    "dummy",
					ChecksStatus: map[string]bool{},
				}, nil)
			},
		},
		{
			name: "diff triggers same workflows as overall, last stamp contains no run then should return same wfs (file pattern filtered)",
			args: args{
				serialWfs: []wfArgs{
					{
						name:     "serial1",
						filter:   "repo/overall/**.go",
						stampKey: "serial1",
					},
					{
						name:     "serial_diff1",
						filter:   "repo/diff/**.go",
						stampKey: "serial_diff1",
					},
					{
						name:     "serial2",
						filter:   "**.sql",
						stampKey: "serial2",
					},
				},
				parallelWfs: []wfArgs{
					{
						name:     "parallel1",
						filter:   "repo/overall/**.go",
						stampKey: "parallel1",
					},
					{
						name:     "parallel_diff1",
						filter:   "repo/diff/**.go",
						stampKey: "parallel_diff1",
					},
				},
				optimisedFileChangesPresent: true,
			},
			wantWfs: wantWfs{
				serialWfNames:   []string{"serial1", "serial_diff1"},
				parallelWfNames: []string{"parallel1", "parallel_diff1"},
			},
			setupMocks: func(s *mocks.MockStamper) {
				s.EXPECT().RetrieveLastValidStamp(gomock.Any()).Times(1).Return(&projectinfo.StampInfo{
					CommitSHA:    "dummy",
					ChecksStatus: map[string]bool{},
				}, nil)
			},
		},
		{
			name: "diff triggers no workflows, last stamp has wf present, then should return all wfs excluding last stamp (file pattern filtered)",
			args: args{
				serialWfs: []wfArgs{
					{
						name:     "serial1",
						filter:   "repo/overall/file1.go",
						stampKey: "serial1",
					},
					{
						name:     "serial2",
						filter:   "repo/overall/file2.go",
						stampKey: "serial2",
					},
					{
						name:     "serial3",
						filter:   "**.sql",
						stampKey: "serial3",
					},
				},
				parallelWfs: []wfArgs{
					{
						name:     "parallel1",
						filter:   "repo/overall/file1.go",
						stampKey: "parallel1",
					},
					{
						name:     "parallel2",
						filter:   "repo/overall/file2.go",
						stampKey: "parallel2",
					},
				},
				optimisedFileChangesPresent: true,
			},
			wantWfs: wantWfs{
				serialWfNames:   []string{"serial1"},
				parallelWfNames: []string{"parallel1"},
			},
			setupMocks: func(s *mocks.MockStamper) {
				s.EXPECT().RetrieveLastValidStamp(gomock.Any()).Times(1).Return(&projectinfo.StampInfo{
					CommitSHA: "dummy",
					ChecksStatus: map[string]bool{
						"serial2":   true,
						"parallel2": true,
					},
				}, nil)
			},
		},
		{
			name: "diff triggers a wf, last stamp has same wf present, then should return all wfs (file pattern filtered)",
			args: args{
				serialWfs: []wfArgs{
					{
						name:     "serial1",
						filter:   "repo/overall/file1.go",
						stampKey: "serial1",
					},
					{
						name:     "serial_diff1",
						filter:   "repo/diff/file1.go",
						stampKey: "serial_diff1",
					},
					{
						name:     "serial2",
						filter:   "**.sql",
						stampKey: "serial2",
					},
				},
				parallelWfs: []wfArgs{
					{
						name:     "parallel1",
						filter:   "repo/overall/file1.go",
						stampKey: "parallel1",
					},
					{
						name:     "parallel_diff1",
						filter:   "repo/diff/file1.go",
						stampKey: "parallel_diff1",
					},
				},
				optimisedFileChangesPresent: true,
			},
			wantWfs: wantWfs{
				serialWfNames:   []string{"serial1", "serial_diff1"},
				parallelWfNames: []string{"parallel1", "parallel_diff1"},
			},
			setupMocks: func(s *mocks.MockStamper) {
				s.EXPECT().RetrieveLastValidStamp(gomock.Any()).Times(1).Return(&projectinfo.StampInfo{
					CommitSHA: "dummy",
					ChecksStatus: map[string]bool{
						"serial_diff1":   true,
						"parallel_diff1": true,
					},
				}, nil)
			},
		},
		{
			name: "diff triggers a new wf, last stamp has wf present, then should return all wfs - last stamp + diff (file pattern filtered)",
			args: args{
				serialWfs: []wfArgs{
					{
						name:     "serial1",
						filter:   "repo/overall/file1.go",
						stampKey: "serial1",
					},
					{
						name:     "serial2",
						filter:   "repo/overall/file2.go",
						stampKey: "serial2",
					},
					{
						name:     "serial_diff2",
						filter:   "repo/diff/file2.go",
						stampKey: "serial_diff2",
					},
					{
						name:     "serial3",
						filter:   "**.sql",
						stampKey: "serial3",
					},
				},
				parallelWfs: []wfArgs{
					{
						name:     "parallel1",
						filter:   "repo/overall/file1.go",
						stampKey: "parallel1",
					},
					{
						name:     "parallel2",
						filter:   "repo/overall/file2.go",
						stampKey: "parallel2",
					},
					{
						name:     "parallel_diff2",
						filter:   "repo/diff/file2.go",
						stampKey: "parallel_diff2",
					},
				},
				optimisedFileChangesPresent: true,
			},
			wantWfs: wantWfs{
				serialWfNames:   []string{"serial1", "serial_diff2"},
				parallelWfNames: []string{"parallel1", "parallel_diff2"},
			},
			setupMocks: func(s *mocks.MockStamper) {
				s.EXPECT().RetrieveLastValidStamp(gomock.Any()).Times(1).Return(&projectinfo.StampInfo{
					CommitSHA: "dummy",
					ChecksStatus: map[string]bool{
						"serial2":   true,
						"parallel2": true,
					},
				}, nil)
			},
		},
		{
			name: "diff triggers a new wf, last stamp has wf present but running in force mode, then should return all wfs (file pattern filtered)",
			args: args{
				serialWfs: []wfArgs{
					{
						name:     "serial1",
						filter:   "repo/overall/file1.go",
						stampKey: "serial1",
					},
					{
						name:     "serial2",
						filter:   "repo/overall/file2.go",
						stampKey: "serial2",
					},
					{
						name:     "serial_diff2",
						filter:   "repo/diff/file2.go",
						stampKey: "serial_diff2",
					},
					{
						name:     "serial3",
						filter:   "**.sql",
						stampKey: "serial3",
					},
				},
				parallelWfs: []wfArgs{
					{
						name:     "parallel1",
						filter:   "repo/overall/file1.go",
						stampKey: "parallel1",
					},
					{
						name:     "parallel2",
						filter:   "repo/overall/file2.go",
						stampKey: "parallel2",
					},
					{
						name:     "parallel_diff2",
						filter:   "repo/diff/file2.go",
						stampKey: "parallel_diff2",
					},
				},
				optimisedFileChangesPresent: true,
				runForceMode:                true,
			},
			wantWfs: wantWfs{
				serialWfNames:   []string{"serial1", "serial2"},
				parallelWfNames: []string{"parallel1", "parallel2"},
			},
			setupMocks: func(s *mocks.MockStamper) {
				s.EXPECT().RetrieveLastValidStamp(gomock.Any()).Times(1).Return(&projectinfo.StampInfo{
					CommitSHA: "dummy",
					ChecksStatus: map[string]bool{
						"serial2":   true,
						"parallel2": true,
					},
				}, nil)
			},
		},
		{
			name: "stamp is on current commit, then should return all workflows - success runs on stamp (file pattern filtered)",
			args: args{
				serialWfs: []wfArgs{
					{
						name:     "serial1",
						filter:   "repo/overall/file1.go",
						stampKey: "serial1",
					},
					{
						name:     "serial2",
						filter:   "repo/overall/file2.go",
						stampKey: "serial2",
					},
					{
						name:     "serial3",
						filter:   "**.sql",
						stampKey: "serial3",
					},
				},
				parallelWfs: []wfArgs{
					{
						name:     "parallel1",
						filter:   "repo/overall/file1.go",
						stampKey: "parallel1",
					},
					{
						name:     "parallel2",
						filter:   "repo/overall/file2.go",
						stampKey: "parallel2",
					},
				},
				optimisedFileChangesPresent: false,
			},
			wantWfs: wantWfs{
				serialWfNames:   []string{"serial1"},
				parallelWfNames: []string{"parallel1"},
			},
			setupMocks: func(s *mocks.MockStamper) {
				s.EXPECT().RetrieveLastValidStamp(gomock.Any()).Times(1).Return(&projectinfo.StampInfo{
					CommitSHA: "abcdef",
					ChecksStatus: map[string]bool{
						"serial2":   true,
						"parallel2": true,
					},
				}, nil)
			},
		},
		{
			name: "skip workflows for certain paths using !",
			args: args{
				serialWfs: []wfArgs{
					{
						name:     "serial1",
						filter:   "!repo/overall/file1.go",
						stampKey: "serial1",
					},
				},
				parallelWfs: []wfArgs{
					{
						name:     "parallel1",
						filter:   "*.sql",
						stampKey: "parallel1",
					},
					{
						name:     "parallel2",
						filter:   "**.go",
						stampKey: "parallel2",
					},
					{
						name:     "parallel3",
						filter:   "!repo/endpoints-{staging,qa,uat}.yml",
						stampKey: "parallel3",
					},
				},
				optimisedFileChangesPresent: false,
			},
			wantWfs: wantWfs{
				serialWfNames:   []string{},
				parallelWfNames: []string{"parallel2"},
			},
			setupMocks: func(s *mocks.MockStamper) {
				s.EXPECT().RetrieveLastValidStamp(gomock.Any()).Times(1).Return(&projectinfo.StampInfo{
					CommitSHA: "abcdef",
				}, nil)
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctr := gomock.NewController(t)
			defer ctr.Finish()
			stamper := mocks.NewMockStamper(ctr)
			tt.setupMocks(stamper)

			p := &projectinfo.ProjectInfo{
				DiffData: &projectinfo.DiffData{
					OptimisedFileChangesPresent: tt.args.optimisedFileChangesPresent,
					OverallFilesChanges: &projectinfo.AffectedFiles{
						ModifiedFiles: []string{"overall/file1.go", "overall/file2.go", "overall/file3.go", "diff/file1.go", "endpoints-staging.yml"},
					},
					FilesChanges: &projectinfo.AffectedFiles{
						ModifiedFiles: []string{"diff/file1.go", "diff/file2.go", "diff/file3.go"},
					},
				},
				PrData: &projectinfo.PrData{
					LatestCommit: "abcdef",
				},
				UserInput: &projectinfo.UserInput{
					ForceRunCheck: tt.args.runForceMode,
				},
				RepoName: "repo",
			}

			serialWfs := lo.Map(tt.args.serialWfs, func(wfArgs wfArgs, _ int) *workflow.Workflow {
				return createWf(wfArgs.name, wfArgs.filter, wfArgs.stampKey, false)
			})
			parallelWfs := lo.Map(tt.args.parallelWfs, func(wfArgs wfArgs, _ int) *workflow.Workflow {
				return createWf(wfArgs.name, wfArgs.filter, wfArgs.stampKey, true)
			})

			eligibleSerialWfs, eligibleParallelWfs, filterErr := FilterEligibleWorkflows(stamper, p, serialWfs, parallelWfs)

			if (filterErr != nil) != tt.wantErr {
				t.Errorf("filterEligibleWorkflows() failed, expected error: %v, got error: %v", tt.wantErr, filterErr)
			}

			if tt.wantErr {
				return
			}

			eligibleSerialWfNames := lo.Map(eligibleSerialWfs, func(wf *workflow.Workflow, _ int) string {
				return wf.FileName
			})
			eligibleParallelWfNames := lo.Map(eligibleParallelWfs, func(wf *workflow.Workflow, _ int) string {
				return wf.FileName
			})

			sort.Strings(eligibleSerialWfNames)
			sort.Strings(eligibleParallelWfNames)
			sort.Strings(tt.wantWfs.serialWfNames)
			sort.Strings(tt.wantWfs.parallelWfNames)

			if !reflect.DeepEqual(eligibleParallelWfNames, tt.wantWfs.parallelWfNames) {
				t.Errorf("filterEligibleWorkflows() failed, expected: %#v, received: %#v", tt.wantWfs.parallelWfNames, eligibleParallelWfNames)
			}

			if !reflect.DeepEqual(eligibleSerialWfNames, tt.wantWfs.serialWfNames) {
				t.Errorf("filterEligibleWorkflows() failed, expected: %#v, received: %#v", tt.wantWfs.serialWfNames, eligibleSerialWfNames)
			}
		})
	}
}

func createWf(fileName, fileFilter, stampKey string, parallel bool) *workflow.Workflow {
	w := workflow.New(fileName)
	w.Jobs.RunChecks.With.LciStampKey = stampKey
	w.Jobs.RunChecks.With.PathsToTrigger = fmt.Sprintf(`impacted_paths:
          - '%s'`, fileFilter)
	return w
}
