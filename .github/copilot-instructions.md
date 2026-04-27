# Stats Generator

Go application that fetches user statistics from GitHub, GitLab, and Azure DevOps APIs and generates SVG visualizations for a GitHub profile README. Persists daily snapshots to `stats_history.json` for historical accumulation, then generates per-year SVG widgets. Runs daily via GitHub Actions, outputting to the `stats` branch.

Single-file Go app (`main.go` only). Requires Go 1.26+. Direct dependencies: `sirupsen/logrus` (logging) and `stretchr/testify` (testing).

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

| Variable                                         | Platform     |
|--------------------------------------------------|--------------|
| `GITHUB_USERNAME`, `GH_TOKEN`                    | GitHub       |
| `GITLAB_USERNAME`, `GITLAB_ACCESS_TOKEN`         | GitLab       |
| `AZURE_DEVOPS_ORG`, `AZURE_DEVOPS_ACCESS_TOKEN`  | Azure DevOps |

Path configuration (defaults work for local development):

| Variable             | Default              | Purpose                                                    |
|----------------------|----------------------|------------------------------------------------------------|
| `RUN_MODE`           | `daily`              | Operating mode: `daily`, `bootstrap`, or `recalculate`     |
| `TARGET_YEAR`        | (none)               | Year to recalculate (required when `RUN_MODE=recalculate`) |
| `STATS_HISTORY_PATH` | `stats_history.json` | Path to read/write historical snapshots                    |
| `SVG_OUTPUT_DIR`     | `.`                  | Directory for generated SVG files                          |
| `README_PATH`        | `README.md`          | Path to README for auto-inserting new year sections        |

## Architecture

Single-package monolith (`package main` in `main.go`). Key components:

- **History system** (`StatsHistory`, `DailySnapshot`, `PlatformSnapshot`): JSON-based persistence. Each daily run saves a snapshot with per-platform stats. `accumulateByYear()` builds per-year views by scanning all snapshots.
- **Platform fetchers** (`FetchGitHubStats`, `FetchGitLabStats`, `FetchAzureDevOpsStats`): HTTP clients returning `*PlatformStats`. Run in parallel via goroutines. All fail gracefully (skip platform on error).
- **SVG renderers**: Pure functions returning SVG strings. `renderCombinedStatsSVG`, `renderTokensHeatmap`, `renderLanguagesBarChart`, `renderContributionHeatmap`.
- **README updater** (`updateReadmeYearSections`): After generating per-year SVGs, auto-inserts new year `<details>` blocks into `README.md` in descending order.
- **Run modes**: `daily` (today only, reuses languages), `bootstrap` (full current year), `recalculate` (full target year, replaces all snapshots for that year).

## Testing

- Build tag `//go:build unit` on all test files; tests won't run without `-tags=unit`
- Uses `stretchr/testify` for assertions (`assert`, `require`)
- Tests are parallel (`t.Parallel()` + `t.Run()`)
- BDD structure with `// given`, `// when`, `// then` comments
- Three test files: `fetchers_test.go` (platform fetcher HTTP mocking), `helpers_test.go` (helpers and utilities), and `svg_generators_test.go` (SVG renderers)

## Generated Output Files

Per-year SVGs plus `_final.svg` aliases pointing to the current year:

| Pattern                     | Content                                        |
|-----------------------------|------------------------------------------------|
| `combined_stats_{year}.svg` | Stats card with stacked bars per metric        |
| `top_languages_{year}.svg`  | Language bar chart stacked by platform         |
| `contributions_{year}.svg`  | Contribution heatmap (Jan 1 - Dec 31 or today) |
| `claude_tokens_final.svg`   | Token usage line graph (not year-scoped)       |
| `stats_history.json`        | Accumulated daily snapshots                    |

## CI/CD

Three workflows in `.github/workflows/`:
- **`update-stats.yml`**: Runs daily at midnight UTC. `RUN_MODE=daily`.
- **`bootstrap-stats.yml`**: Manual dispatch only. `RUN_MODE=bootstrap`.
- **`recalculate-stats.yml`**: Manual dispatch with `year` input. `RUN_MODE=recalculate`.

All workflows check out `main`, restore `stats_history.json` from the `stats` branch, run the generator, and force-push an orphan commit to `stats`.

## Repository Structure

```
├── main.go                           # Single-file application
├── fetchers_test.go                  # Unit tests for platform fetchers
├── helpers_test.go                   # Unit tests for helpers and utilities
├── svg_generators_test.go            # Unit tests for SVG renderers
├── go.mod / go.sum                   # Go module definition
├── Makefile                          # Build targets (test, generate)
├── README.md                         # Profile README with per-year stats
├── CLAUDE.md                         # AI assistant instructions (source of truth)
├── CHANGELOG.md                      # Project changelog
├── CONTRIBUTING.md                   # Contribution guidelines
├── stats_history.json                # Daily snapshots (on stats branch)
├── claude_tokens.json                # Token usage data (on stats branch)
├── .github/
│   ├── copilot-instructions.md       # This file
│   └── workflows/
│       ├── update-stats.yml          # Daily stats workflow
│       ├── bootstrap-stats.yml       # Bootstrap workflow
│       └── recalculate-stats.yml     # Recalculate workflow
└── .assets/                          # Custom SVG icons
```
