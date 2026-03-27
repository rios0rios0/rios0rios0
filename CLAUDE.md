# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go application that fetches user statistics from GitHub, GitLab, and Azure DevOps APIs and generates SVG visualizations (combined stats, Claude token usage line graph, top languages bar chart, contribution heatmap) for a GitHub profile README. Runs daily via GitHub Actions, outputting SVGs to a `stats` branch.

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

The application runs whichever platforms have credentials set. At least one platform must be configured:

| Variable | Platform |
|---|---|
| `GITHUB_USERNAME`, `GH_TOKEN` | GitHub |
| `GITLAB_USERNAME`, `GITLAB_ACCESS_TOKEN` | GitLab |
| `AZURE_DEVOPS_ORG`, `AZURE_DEVOPS_ACCESS_TOKEN` | Azure DevOps |

## Architecture

Single-package monolith (`package main` in `main.go`) with no domain/infrastructure separation. All logic lives in one file:

- **Platform types** (`PlatformName`, `NamedPlatformStats`): Typed platform identity with color and color-scale methods. Platform order is fixed: GitHub, GitLab, Azure DevOps
- **Platform fetchers** (`FetchGitHubStats`, `FetchGitLabStats`, `FetchAzureDevOpsStats`): HTTP clients that call platform APIs and return `*PlatformStats`
- **SVG renderers**: Pure functions that return SVG strings. Each has a `Generate*` wrapper that writes to disk
  - `renderCombinedStatsSVG([]NamedPlatformStats)` -- code-generated stats card with per-platform stacked bars
  - `renderTokensLineGraph([]TokenUsage)` -- line graph (no platform attribution)
  - `renderLanguagesBarChart(map[string]map[PlatformName]int64)` -- stacked bars by platform per language
  - `renderContributionHeatmap(map[string]map[PlatformName]int, time.Time)` -- heatmap with platform-colored cells
- **Helpers** (`formatNumber`, `aggregateLanguagesByPlatform`, `aggregateContributionsByPlatform`, `renderPlatformLegend`, `loadTokenUsage`)
- **`main()`**: Fetches from configured platforms into `[]NamedPlatformStats`, then passes per-platform data to renderers

Platform colors: GitHub `#8b949e` (gray), GitLab `#e24329` (orange), Azure DevOps `#0078d4` (blue). All SVG cards share unified chrome: `rx="4.5"`, `fill="#151515"`, `stroke="#e4e2e2"`, `stroke-opacity="0.2"`.

## Testing

- Build tag `//go:build unit` on all test files; tests won't run without `-tags=unit`
- Uses `stretchr/testify` for assertions (`assert`, `require`)
- Tests are parallel (`t.Parallel()` + `t.Run()`)
- BDD structure with `// given`, `// when`, `// then` comments
- SVG output validated as well-formed XML via `assertValidSVGXML` helper
- Two test files: `helpers_test.go` (formatNumber, aggregation functions) and `svg_generators_test.go` (all four SVG renderers)

## Generated Output Files

| File | Content |
|---|---|
| `combined_stats_final.svg` | Unified stats card (commits, PRs, issues across platforms) |
| `claude_tokens_final.svg` | Token usage line graph from `claude_tokens.json` |
| `top_languages_final.svg` | Language bar chart by bytes |
| `contributions_final.svg` | 52-week contribution heatmap |

## CI/CD

Single workflow (`.github/workflows/update-stats.yml`) runs daily at midnight UTC. Generates SVGs, amends the last commit, and force-pushes to the `stats` branch. Can be triggered manually via `workflow_dispatch`.
