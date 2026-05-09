# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

When a new release is proposed:

1. Create a new branch `bump/x.x.x` (this isn't a long-lived branch!!!);
2. The Unreleased section on `CHANGELOG.md` gets a version number and date;
3. Open a Pull Request with the bump version changes targeting the `main` branch;
4. When the Pull Request is merged, a new git tag must be created using [GitHub environment](https://github.com/rios0rios0/rios0rios0/tags).

Releases to productive environments should run from a tagged version.
Exceptions are acceptable depending on the circumstances (critical bug fixes that can be cherry-picked, etc.).

## [Unreleased]

## [0.2.5] - 2026-05-09

### Changed

- changed the Go module dependencies to their latest versions

## [0.2.4] - 2026-05-08

### Changed

- changed the Go version to `1.26.3` and updated all module dependencies

## [0.2.3] - 2026-04-28

### Changed

- refreshed `CLAUDE.md` and `.github/copilot-instructions.md` to correct dependency listing (`sirupsen/logrus` replaced `golang.org/x/text`), document `fetchers_test.go`, and add the README updater to the architecture section
- refreshed `CLAUDE.md` to add `LOG_LEVEL` env var, document the two Claude CI workflows, and fix stale documentation section; updated `.github/copilot-instructions.md` to fix repository structure tree

## [0.2.2] - 2026-04-19

### Fixed

- fixed `recalculate` workflow inserting a duplicate collapsed `<details>` block for the current year in `README.md`, which opened an unwanted PR when `TARGET_YEAR` matched the current year

## [0.2.1] - 2026-04-15

### Changed

- changed the Go version to `1.26.2` and updated all module dependencies

## [0.2.0] - 2026-03-30

### Added

- added `Makefile` with `test` and `generate` targets
- added `recalculate` workflow mode with `workflow_dispatch` to re-fetch and replace stats for a specific year
- added Azure DevOps language detection via file extension analysis from repo trees
- added parallel platform fetching using goroutines for faster execution
- added unit tests for helper functions (`formatNumber`, `mergeContributions`, `mergeLanguages`)
- added unit tests for SVG generators with XML validation (22 test cases)

### Changed

- changed `accumulateByYear` to merge languages across all snapshots (max bytes per language) instead of only keeping the latest snapshot
- changed language fetching to only include repos with activity in the last year (GitHub: `pushed_at`, GitLab: `last_activity_at`)
- changed the Go module dependencies to their latest versions
- refactored SVG generators to separate rendering logic from file I/O for testability

### Fixed

- fixed Azure DevOps connection data API version to use `7.0-preview`
- fixed contribution and token heatmaps to always render full-year width for consistent sizing

### Removed

- removed "Contributed to (last year)" metric from the stats card

## [0.1.0] - 2026-03-12

### Added

- added the boilerplate missing files to the project
- created Azure DevOps Stats workflow to generate the SVG picture
- created GitLab Stats workflow to generate the SVG picture

### Changed

- changed the Go version to `1.26.0` and updated all module dependencies
- changed the Go version to `1.26.1` and updated all module dependencies
- corrected the `README.md` to point to the new created stats card
- corrected the workflow indentation to generate the SVG picture
- updated GoLang version to 1.23.4 and all dependencies to the latest version

