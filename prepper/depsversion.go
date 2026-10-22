// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prepper

// Minimum versions are stored WITHOUT a leading "v"; callers prepend one before
// handing them to semver.Compare (which requires the "vX.Y.Z" form). Storing the
// "v" here too would double it ("vv2.11.4"), an invalid semver string that makes
// Compare silently treat the bound as unset so the version check never fails.
const (
	GhCliMinVersion        = "2.35.0"
	GolangciLintMinVersion = "2.11.4"
)
