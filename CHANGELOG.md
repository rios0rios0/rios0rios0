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

## [0.2.0] - 2026-03-27

### Added

- added `Makefile` with `test` and `generate` targets
- added unit tests for helper functions (`formatNumber`, `mergeContributions`, `mergeLanguages`)
- added unit tests for SVG generators with XML validation (22 test cases)

### Changed

- refactored SVG generators to separate rendering logic from file I/O for testability

### Fixed

- fixed Azure DevOps connection data API version to use `7.0-preview`

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

