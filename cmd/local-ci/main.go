// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import "runtime/debug"

func main() {
	Execute(version())
}

// version returns the release version. goreleaser stamps it via
// -ldflags "-X main.buildVersion=v1.2.3"; otherwise it falls back to the module
// version embedded in the binary, then to "dev" for a plain `go build`.
func version() string {
	if buildVersion != "" {
		return buildVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// buildVersion is set via -ldflags at release time.
var buildVersion string
