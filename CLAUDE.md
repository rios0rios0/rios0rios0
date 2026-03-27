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

- **Platform fetchers** (`FetchGitHubStats`, `FetchGitLabStats`, `FetchAzureDevOpsStats`): HTTP clients that call platform APIs and return `PlatformStats`
- **SVG renderers** (`renderCombinedStatsSVG`, `renderTokensLineGraph`, `renderLanguagesBarChart`, `renderContributionHeatmap`): Pure functions that take data and return SVG strings. Each has a `Generate*` wrapper that writes to disk
- **Helpers** (`formatNumber`, `mergeContributions`, `mergeLanguages`, `loadTokenUsage`): Utility functions used across renderers
- **`main()`**: Orchestrates fetching from all configured platforms, merges stats, and generates all SVG files

The `render*` functions are separated from `Generate*` wrappers specifically for testability -- tests call `render*` directly without filesystem side effects.

## Testing

- Build tag `//go:build unit` on all test files; tests won't run without `-tags=unit`
- Uses `stretchr/testify` for assertions (`assert`, `require`)
- Tests are parallel (`t.Parallel()` + `t.Run()`)
- BDD structure with `// given`, `// when`, `// then` comments
- SVG output validated as well-formed XML via `assertValidSVGXML` helper
- Two test files: `helpers_test.go` (formatNumber, mergeContributions, mergeLanguages) and `svg_generators_test.go` (all four SVG renderers)

## Generated Output Files

| File | Content |
|---|---|
| `combined_stats_final.svg` | Unified stats card (commits, PRs, issues across platforms) |
| `claude_tokens_final.svg` | Token usage line graph from `claude_tokens.json` |
| `top_languages_final.svg` | Language bar chart by bytes |
| `contributions_final.svg` | 52-week contribution heatmap |

## CI/CD

Single workflow (`.github/workflows/update-stats.yml`) runs daily at midnight UTC. Generates SVGs, amends the last commit, and force-pushes to the `stats` branch. Can be triggered manually via `workflow_dispatch`.
