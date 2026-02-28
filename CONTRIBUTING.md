# Contributing

Contributions are welcome. By participating, you agree to maintain a respectful and constructive environment.

For coding standards, testing patterns, architecture guidelines, commit conventions, and all
development practices, refer to the **[Development Guide](https://github.com/rios0rios0/guide/wiki)**.

## Prerequisites

- [Go](https://go.dev/dl/) 1.26.0+

## Development Workflow

1. Fork and clone the repository
2. Create a branch: `git checkout -b feat/my-change`
3. Download module dependencies:
   ```bash
   go mod download
   ```
4. Build the stats generator binary:
   ```bash
   go build -o stats-generator main.go
   ```
5. Run the program locally (requires environment variables for at least one platform):
   ```bash
   # For GitLab stats
   GITLAB_USERNAME=your_username GITLAB_ACCESS_TOKEN=your_token go run main.go

   # For Azure DevOps stats
   AZURE_DEVOPS_ORG=your_org AZURE_DEVOPS_ACCESS_TOKEN=your_token go run main.go
   ```
6. Verify the generated SVG output files (`gitlab_stats_final.svg` or `azure_devops_stats_final.svg`)
7. Run tests:
   ```bash
   go test ./...
   ```
8. Update `CHANGELOG.md` under `[Unreleased]`
9. Commit following the [commit conventions](https://github.com/rios0rios0/guide/wiki/Life-Cycle/Git-Flow)
10. Open a pull request against `main`
