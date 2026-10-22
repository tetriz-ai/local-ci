// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v3"

	"github.com/tetriz-ai/local-ci/checker"
	"github.com/tetriz-ai/local-ci/cliutils"
	"github.com/tetriz-ai/local-ci/internal/cmdrunner"
	"github.com/tetriz-ai/local-ci/internal/logger"
	"github.com/tetriz-ai/local-ci/internal/projectroot"
	"github.com/tetriz-ai/local-ci/pkg/cmdlogger"
	"github.com/tetriz-ai/local-ci/projectinfo"
	"github.com/tetriz-ai/local-ci/runner"
)

// defaultCheckers is the built-in checker set this CLI ships with. The engine
// itself holds no fixed roster — a fork supplies its own set here (add a
// checker implementing checker.Checker, or drop one), which is the whole point
// of the plugin seam.
func defaultCheckers() []checker.Checker {
	return []checker.Checker{
		checker.NewCommentReplyChecker(),
		checker.NewIssueLinkerChecker(),
		checker.NewProtoBreakingChecker(),
		checker.NewBufLintCommentsChecker(),
		checker.NewReadmeChecker(),
		checker.NewGovulncheckChecker(),
		checker.NewPnpmAuditChecker(),
		checker.NewLockfileSyncChecker(),
		checker.NewGolangciLintChecker(),
	}
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "local-ci",
	Short: "local-ci runs CI checks locally and stamps the commit so matching GitHub checks can skip",
	Long: `local-ci (Local CI) runs project checks on your machine against the current PR and
stamps the head commit with each check's result. A merge gate on GitHub reads the
stamp and skips the checks it already covers, so you don't wait on redundant CI.`,
	Run: func(cmd *cobra.Command, args []string) {
		if code := runRoot(cmd); code != 0 {
			os.Exit(code)
		}
	},
}

// runRoot returns before exiting so output ownership is finalized on ordinary failures too.
func runRoot(cmd *cobra.Command) (exitCode int) {
	debug, debugErr := cmd.Flags().GetBool(debugMode)
	if debugErr != nil {
		fmt.Fprintln(os.Stderr, "invalid --debug flag:", debugErr)
		return 1
	}
	repoPath, repoPathErr := projectroot.GetRepoRoot()
	if repoPathErr != nil {
		fmt.Fprintln(os.Stderr, "error getting repo root (run local-ci from inside a git repository):", repoPathErr)
		return 1
	}
	// Output goes to the current worktree's output/local-ci (not main repo root),
	// so parallel local-ci runs from different worktrees don't collide.
	outputPath := filepath.Join(repoPath, "output", "local-ci")
	err := cliutils.SetupOutputCliDir(outputPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error preparing the output/local-ci directory", err)
		return 1
	}
	defer func() {
		if logger.Log != nil {
			_ = logger.Log.Sync()
		}
		if err := cliutils.FinalizeOutputCliDir(outputPath); err != nil {
			fmt.Fprintln(os.Stderr, "error recording output ownership:", err)
			if exitCode == 0 {
				exitCode = 1
			}
		}
	}()
	if loggerErr := initialiseLogger(debug, repoPath); loggerErr != nil {
		fmt.Fprintln(os.Stderr, "error initialising logger:", loggerErr)
		return 1
	}
	prNum, prErr := GetPRNumber(cmd)
	if prErr != nil {
		fmt.Println("Error getting PR number: ", prErr)
		return 1
	}
	// Handle --zip-history before anything that assumes a normal check run: it
	// is a standalone utility that zips the stored run history and exits, so
	// "Running checks for PR" must not print and no checks are set up for it.
	zipHistory, zipHistoryErr := cmd.Flags().GetBool(zipRunHistory)
	if zipHistoryErr != nil {
		fmt.Fprintln(os.Stderr, "invalid --zip-history flag:", zipHistoryErr)
		return 1
	}
	if zipHistory {
		if zipErr := cliutils.ZipDirectory(prNum); zipErr != nil {
			fmt.Fprintf(os.Stderr, "Error zipping run history: %v\n", zipErr)
			return 1
		}
		return 0
	}

	fmt.Println("Running checks for PR:", prNum)
	force, forceErr := cmd.Flags().GetBool(forceRunCheck)
	if forceErr != nil {
		fmt.Fprintln(os.Stderr, "invalid --force flag:", forceErr)
		return 1
	}
	checkers, checkersErr := cmd.Flags().GetStringSlice(enableWorkflows)
	if checkersErr != nil {
		fmt.Fprintln(os.Stderr, "invalid --enable-workflows list:", checkersErr)
		return 1
	}
	disabled, disabledErr := cmd.Flags().GetStringSlice(disableWorkflows)
	if disabledErr != nil {
		fmt.Fprintln(os.Stderr, "invalid --disable-workflows list:", disabledErr)
		return 1
	}
	dryRun, dryRunErr := cmd.Flags().GetBool(dryRunMode)
	if dryRunErr != nil {
		fmt.Fprintln(os.Stderr, "invalid --dry-run flag:", dryRunErr)
		return 1
	}
	ghActionMode, ghActionErr := cmd.Flags().GetBool(ghActionRunMode)
	if ghActionErr != nil {
		fmt.Fprintln(os.Stderr, "invalid --gh-action flag:", ghActionErr)
		return 1
	}
	// "all" clears the selection filter, so every eligible checker runs.
	for _, c := range checkers {
		if c == "all" {
			checkers = []string{}
			break
		}
	}

	// In dry-run there is no checker filtering: list everything eligible.
	if dryRun {
		checkers = []string{}
	}

	userInput := &projectinfo.UserInput{
		PrNumber:           prNum,
		SelectiveWorkflows: checkers,
		DisabledWorkflows:  disabled,
		ForceRunCheck:      force,
		DryRunMode:         dryRun,
		GhActionMode:       ghActionMode,
	}

	checksRunner := runner.NewRunner(userInput, cmd.Version, defaultCheckers())
	if err := checksRunner.Run(); err != nil {
		fmt.Printf("Error completing the checks: \n%v\n", err.Error())
		return 1
	}
	return 0
}

func GetPRNumber(cmd *cobra.Command) (int, error) {
	if cmd.Flags().Changed(prNumber) {
		pr, prErr := cmd.Flags().GetInt(prNumber)
		if prErr != nil {
			return 0, fmt.Errorf("invalid pr number passed: %w", prErr)
		}

		return pr, nil
	}
	cmdRunner, err := cmdrunner.NewCmdRunner(".", cmdrunner.WithLogger(cmdlogger.Logger{}))
	if err != nil {
		return 0, fmt.Errorf("couldn't create command runner to auto fetch PR Number: %w", err)
	}
	currentBranch, err := cmdRunner.ExecuteCommand("git branch --show-current")
	if err != nil {
		return 0, fmt.Errorf("couldn't get the current git branch to auto fetch PR number: %w", err)
	}

	res, prErr := cmdRunner.ExecuteCommandArgs("gh", []string{"pr", "list", "--json", "number", "-H", currentBranch})
	if prErr != nil {
		return 0, fmt.Errorf("couldn't auto fetch PR number from the current branch %q: %w", currentBranch, prErr)
	}
	type PRNumList struct {
		Number int `json:"number"`
	}
	var prNumList []PRNumList
	if unMarshalErr := yaml.Unmarshal([]byte(res), &prNumList); unMarshalErr != nil {
		return 0, fmt.Errorf("couldn't unmarshal the PR number list: %w", unMarshalErr)
	}
	if len(prNumList) == 0 {
		return 0, fmt.Errorf("no PR number found for the current branch %q", currentBranch)
	}
	if len(prNumList) > 1 {
		prNums := []int{}
		for _, pr := range prNumList {
			prNums = append(prNums, pr.Number)
		}
		return 0, fmt.Errorf("multiple PR numbers %v found for the current branch %q", prNums, currentBranch)
	}

	return prNumList[0].Number, nil
}

func initialiseLogger(debugMode bool, repoPath string) error {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

	config.OutputPaths = []string{"stdout", filepath.Join(repoPath, "output", "local-ci", "stdoutput.log")}
	if !debugMode {
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}
	config.DisableStacktrace = true

	var loggerErr error
	logger.Log, loggerErr = config.Build(zap.AddCallerSkip(1))
	logger.IsDevLogger = true

	if loggerErr != nil {
		return fmt.Errorf("building zap logger: %w", loggerErr)
	}
	return nil
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(toolVersion string) {
	rootCmd.Version = toolVersion
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, "local-ci failed:", err)
		os.Exit(1)
	}
}

//

func init() {
	rootCmd.Flags().IntP(prNumber, prNumberShortHand, 0, "PR number to run local-ci on; inferred from the current branch when omitted")
	rootCmd.Flags().BoolP(debugMode, debugModeShortHand, false, "Print all debug logs")
	rootCmd.Flags().BoolP(forceRunCheck, forceRunCheckShortHand, false, "Discard previous stamp and run checks")
	rootCmd.Flags().Bool(dryRunMode, false, "Run in dry run mode to get list of all the available runners for a PR")
	rootCmd.Flags().StringSliceP(enableWorkflows, enableWorkflowsShortHand, []string{}, "Comma-separated checkers to run. Omitting the flag, or passing \"all\", runs every eligible checker (the two are equivalent)")
	rootCmd.Flags().StringSliceP(disableWorkflows, disableWorkflowsShortHand, []string{}, "Comma-separated checkers to turn off by name (e.g. issue_linker,readme_checker). Disabled checkers never run and are not stamped, so a gate that requires one will fail")
	rootCmd.Flags().BoolP(ghActionRunMode, ghActionRunModeShortHand, false, "Run in github action mode")
	rootCmd.Flags().BoolP(zipRunHistory, zipRunHistoryShortHand, false, "Zip the local-ci run history of the given PR number")
}
