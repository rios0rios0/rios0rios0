# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go application that fetches user statistics from GitHub, GitLab, and Azure DevOps APIs and generates SVG visualizations for a GitHub profile README. Persists daily snapshots to `stats_history.json` for historical accumulation, then generates per-year SVG widgets. Runs daily via GitHub Actions, outputting to the `stats` branch.

Single-file Go app (`main.go` only -- no `cmd/`, `internal/`, or multi-package structure). Requires Go 1.26+. Direct dependencies: `golang.org/x/text` (number formatting) and `stretchr/testify` (testing).

## Build and Test Commands

```bash
# Run all unit tests (build tag required)
make test

# Run a single test by name
go test -tags=unit -v -count=1 -run TestFunctionName ./...

# Generate SVGs locally (requires platform credentials as env vars)
make generate

# Build binary
go build -o stats-generator main.go

# Format and vet
go fmt ./... && go vet ./...
```

The Makefile only has `test` and `generate` targets. There are no `lint` or `sast` targets in this project.

## Environment Variables

Platform credentials (at least one required):

| Variable | Platform |
|---|---|
| `GITHUB_USERNAME`, `GH_TOKEN` | GitHub |
| `GITLAB_USERNAME`, `GITLAB_ACCESS_TOKEN` | GitLab |
| `AZURE_DEVOPS_ORG`, `AZURE_DEVOPS_ACCESS_TOKEN` | Azure DevOps |

Path configuration (defaults work for local development):

| Variable | Default | Purpose |
|---|---|---|
| `RUN_MODE` | `daily` | Operating mode: `daily`, `bootstrap`, or `recalculate` |
| `TARGET_YEAR` | (none) | Year to recalculate (required when `RUN_MODE=recalculate`) |
| `STATS_HISTORY_PATH` | `stats_history.json` | Path to read/write historical snapshots |
| `SVG_OUTPUT_DIR` | `.` | Directory for generated SVG files |

## Architecture

Single-package monolith (`package main` in `main.go`). Key components:

- **History system** (`StatsHistory`, `DailySnapshot`, `PlatformSnapshot`): JSON-based persistence. Each daily run saves a snapshot with per-platform stats. `accumulateByYear()` builds per-year views by scanning all snapshots, merging languages (max bytes per language across snapshots) and collecting contributions by year (max per date).
- **Platform types** (`PlatformName`, `NamedPlatformStats`): Typed platform identity with color/color-scale methods. Order: GitHub, GitLab, Azure DevOps.
- **Platform fetchers** (`FetchGitHubStats`, `FetchGitLabStats`, `FetchAzureDevOpsStats`): HTTP clients returning `*PlatformStats`. Run in parallel via goroutines. GitHub uses GraphQL for contributions. Language data is filtered to repos with activity in the last year (GitHub: `pushed_at`, GitLab: `last_activity_at`, Azure DevOps: file extension analysis from repo trees). All fail gracefully (skip platform on error).
- **SVG renderers**: Pure functions returning SVG strings. Each has a `Generate*` wrapper for disk I/O.
  - `renderCombinedStatsSVG([]NamedPlatformStats)` -- stats card with stacked bars
  - `renderTokensHeatmap([]TokenUsage)` -- token usage line graph (named "heatmap" historically)
  - `renderLanguagesBarChart(map[string]map[PlatformName]int64)` -- stacked bar chart
  - `renderContributionHeatmap(contribs, startDate, endDate)` -- heatmap with full year range
- **Run modes**: `daily` (today only, reuses languages), `bootstrap` (full current year), `recalculate` (full target year, replaces all snapshots for that year via `removeSnapshotsForYear`, regenerates SVGs for all years)
- **`main()` flow**: Load history -> fetch platforms (parallel) -> save snapshot -> accumulate by year -> generate per-year SVGs (full year range) -> copy current year to `_final.svg` -> generate tokens graph

Platform colors: GitHub `#238636`, GitLab `#e24329`, Azure DevOps `#0078d4`. Unified card chrome: `rx="4.5"`, `fill="#151515"`, `stroke="#e4e2e2"`, `stroke-opacity="0.2"`.

## Testing

- Build tag `//go:build unit` on all test files; tests won't run without `-tags=unit`
- Uses `stretchr/testify` for assertions (`assert`, `require`)
- Tests are parallel (`t.Parallel()` + `t.Run()`)
- BDD structure with `// given`, `// when`, `// then` comments
- SVG output validated as well-formed XML via `assertValidSVGXML` helper
- Two test files: `helpers_test.go` (formatNumber, aggregation, history persistence, year accumulation) and `svg_generators_test.go` (all four SVG renderers, year tabs)

## Generated Output Files

Per-year SVGs (e.g., `combined_stats_2026.svg`) plus `_final.svg` aliases pointing to the current year:

| Pattern | Content |
|---|---|
| `combined_stats_{year}.svg` | Stats card with stacked bars per metric |
| `top_languages_{year}.svg` | Language bar chart stacked by platform |
| `contributions_{year}.svg` | Contribution heatmap (Jan 1 - Dec 31 or today) |
| `claude_tokens_final.svg` | Token usage line graph (not year-scoped) |
| `stats_history.json` | Accumulated daily snapshots |

## CI/CD

Three workflows in `.github/workflows/`:
- **`update-stats.yml`**: Runs daily at midnight UTC and on push to main. `RUN_MODE=daily`.
- **`bootstrap-stats.yml`**: Manual dispatch only. `RUN_MODE=bootstrap`. Full current-year fetch with languages.
- **`recalculate-stats.yml`**: Manual dispatch only with `year` input. `RUN_MODE=recalculate`. Re-fetches all data for the given year, replaces that year's snapshots, and regenerates SVGs for all years.

All workflows check out `main`, restore `stats_history.json` from the `stats` branch, run the generator, and force-push an orphan commit to `stats`.

## Stale Documentation

`.github/copilot-instructions.md` and `CONTRIBUTING.md` are outdated -- they reference the old template-based SVG approach, claim no tests exist, and list old workflow files. Use this CLAUDE.md as the source of truth.
