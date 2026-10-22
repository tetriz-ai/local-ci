// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"go/token"
	"log"

	"github.com/spf13/cobra"
	"golang.org/x/tools/go/packages"
)

// impactedPkgsCmd represents the impactedpkgs command
var impactedPkgsCmd = &cobra.Command{
	Use:   "impactedpkgs <pkg1> <pkg2> ...",
	Short: "impactedpkgs sub command lists all the packages impacted by the given set of packages",
	Long: `impactedpkgs command loads all the packages in repository and finds the packages impacted by the given set of packages.
Packages which directly imports the given set of packages are considered as impacted packages.
The command prints all the directly impacted packages to the stdout separated by new line.`,
	Run: findImpactedPkgs,
}

//

func init() {
	rootCmd.AddCommand(impactedPkgsCmd)
}

func findImpactedPkgs(cmd *cobra.Command, args []string) {
	impactedPkgs := FindImpactedPkgsHelper(cmd, args)
	for _, pkg := range impactedPkgs {
		fmt.Println(pkg)
	}
}

func FindImpactedPkgsHelper(cmd *cobra.Command, args []string) []string {
	log.SetFlags(log.Lshortfile | log.LstdFlags)
	pkgs, err := loadImports()
	if err != nil {
		log.Fatal("failed to load imports", err)
	}
	reverseMap := make(map[string][]string)
	for _, pkg := range pkgs {
		for _, importedPkg := range pkg.Imports {
			reverseMap[importedPkg.ID] = append(reverseMap[importedPkg.ID], pkg.ID)
		}
	}
	impactedPks := make(map[string]struct{})
	for _, pkg := range args {
		for _, impactedPkg := range reverseMap[pkg] {
			impactedPks[impactedPkg] = struct{}{}
		}
	}
	var impactedPkgs []string

	for pkg := range impactedPks {
		impactedPkgs = append(impactedPkgs, pkg)
	}
	return impactedPkgs
}

func loadImports() (pkgs []*packages.Package, err error) {
	astCfg := &packages.Config{Fset: token.NewFileSet(), Mode: packages.NeedDeps | packages.NeedImports, Dir: "."}
	loaded, err := packages.Load(astCfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("loading packages: %w", err)
	}
	return loaded, nil
}
