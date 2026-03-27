# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go application that fetches user statistics from GitHub, GitLab, and Azure DevOps APIs and generates SVG visualizations for a GitHub profile README. Persists daily snapshots to `stats_history.json` for historical accumulation, then generates per-year SVG widgets. Runs daily via GitHub Actions, outputting to the `stats` branch.

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
| `STATS_HISTORY_PATH` | `stats_history.json` | Path to read/write historical snapshots |
| `SVG_OUTPUT_DIR` | `.` | Directory for generated SVG files |

## Architecture

Single-package monolith (`package main` in `main.go`). Key components:

- **History system** (`StatsHistory`, `DailySnapshot`, `PlatformSnapshot`): JSON-based persistence. Each daily run saves a snapshot with per-platform stats. `accumulateByYear()` builds per-year views by scanning all snapshots, merging languages (max bytes per language across snapshots) and collecting contributions by year (max per date).
- **Platform types** (`PlatformName`, `NamedPlatformStats`): Typed platform identity with color/color-scale methods. Order: GitHub, GitLab, Azure DevOps.
- **Platform fetchers** (`FetchGitHubStats`, `FetchGitLabStats`, `FetchAzureDevOpsStats`): HTTP clients returning `*PlatformStats`. Run in parallel via goroutines. GitHub uses GraphQL for contributions. Language data is filtered to repos with activity in the last year (GitHub: `pushed_at`, GitLab: `last_activity_at`, Azure DevOps: file extension analysis from repo trees). All fail gracefully (skip platform on error).
- **SVG renderers**: Pure functions returning SVG strings. Each has a `Generate*` wrapper for disk I/O.
  - `renderCombinedStatsSVG([]NamedPlatformStats)` -- stats card with stacked bars
  - `renderTokensHeatmap([]TokenUsage)` -- heatmap (full year width)
  - `renderLanguagesBarChart(map[string]map[PlatformName]int64)` -- stacked bar chart
  - `renderContributionHeatmap(contribs, startDate, endDate)` -- heatmap with full year range
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

Workflow (`.github/workflows/update-stats.yml`) runs daily at midnight UTC and on push to main. Dual-checkout strategy: checks out `stats` branch first (for `stats_history.json`), then `main` into `src/` subdirectory. Regular commits (not amend+force-push) to preserve history.
