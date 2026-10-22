# Changelog

All notable changes are recorded using [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-10-22

### Added

- Initial release.
- `local-ci` runs eligible pull-request checks locally and records results in a git note on the head commit.
- A checker interface for project-specific checks and nine built-in checkers.
- `deps` lists impacted Go packages and directories and merges directory patterns.
- A composite GitHub Action and example merge gate for recorded commit stamps.
- Local run logs, per-PR history and ownership-checked output cleanup.
- Three complete offline examples and portable release archives with checksums and `SOURCE.json`.

[Unreleased]: https://github.com/tetriz-ai/local-ci/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/tetriz-ai/local-ci/releases/tag/v0.1.0
