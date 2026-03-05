# Stats Generator

A Go application that fetches user statistics from GitLab and Azure DevOps via their respective APIs and generates SVG visualizations for GitHub profile README files. The application runs as GitHub Actions workflows that update the stats daily.

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

## Working Effectively

### Bootstrap and Build Process
- Download dependencies: `go mod download` -- completes in under 2 seconds
- Build the application: `go build -o stats-generator main.go` -- takes 10-20 seconds. NEVER CANCEL.
- Run directly: `go run main.go` -- builds and runs in under 1 second for execution
- Clean dependencies: `go mod tidy` -- completes in under 1 second

### Environment Requirements
- **REQUIRED**: Go 1.26.0 or compatible (application uses `go 1.26.0` in go.mod)
- **REQUIRED**: At least one platform's credentials must be set (GitLab or Azure DevOps)
- **REQUIRED**: Network access to `gitlab.com` and/or `dev.azure.com` API endpoints
- **GitLab**: `GITLAB_USERNAME` and `GITLAB_ACCESS_TOKEN` environment variables; `gitlab_stats.svg` template file must exist in the working directory
- **Azure DevOps**: `AZURE_DEVOPS_ORG` and `AZURE_DEVOPS_ACCESS_TOKEN` environment variables; `azure_devops_stats.svg` template file must exist in the working directory

### Dependencies
- Single external dependency: `golang.org/x/text v0.34.0` for internationalization
- No transitive dependencies (go.sum contains only 2 entries)
- No additional build tools or linters required

## Build and Test Commands

### Essential Commands
```bash
# Install dependencies
go mod download

# Build application
go build -o stats-generator main.go

# Run application (GitLab)
GITLAB_USERNAME=your_username GITLAB_ACCESS_TOKEN=your_token go run main.go

# Run application (Azure DevOps)
AZURE_DEVOPS_ORG=your_org AZURE_DEVOPS_ACCESS_TOKEN=your_token go run main.go

# Clean workspace
go clean -cache
rm -f stats-generator
```

### Code Quality Commands
```bash
# Format code (silent if no changes needed)
go fmt ./...

# Vet code for potential issues
go vet ./...

# Tidy up dependencies
go mod tidy
```

### Testing
- **NO UNIT TESTS**: This repository contains no test files
- **VALIDATION**: Application validates by checking that at least one platform's credentials are set
- **ERROR HANDLING**: Application exits with status 1 and descriptive error messages on failure

## Running the Application

### Environment Setup
```bash
# For GitLab stats
export GITLAB_USERNAME=your_gitlab_username
export GITLAB_ACCESS_TOKEN=your_personal_access_token

# For Azure DevOps stats
export AZURE_DEVOPS_ORG=your_organization
export AZURE_DEVOPS_ACCESS_TOKEN=your_personal_access_token
```

### Execution
```bash
# GitLab stats only
GITLAB_USERNAME=username GITLAB_ACCESS_TOKEN=token go run main.go

# Azure DevOps stats only
AZURE_DEVOPS_ORG=org AZURE_DEVOPS_ACCESS_TOKEN=token go run main.go

# Both platforms at once
GITLAB_USERNAME=username GITLAB_ACCESS_TOKEN=token \
  AZURE_DEVOPS_ORG=org AZURE_DEVOPS_ACCESS_TOKEN=token go run main.go

# OR using built binary
./stats-generator
```

### Expected Output
- **GitLab SUCCESS**: Prints "GitLab SVG generated successfully..." and creates `gitlab_stats_final.svg`
- **Azure DevOps SUCCESS**: Prints "Azure DevOps SVG generated successfully..." and creates `azure_devops_stats_final.svg`
- **NO CREDENTIALS**: Prints "No platform credentials configured. Set GITLAB_USERNAME/GITLAB_ACCESS_TOKEN or AZURE_DEVOPS_ORG/AZURE_DEVOPS_ACCESS_TOKEN environment variables." and exits with status 1
- **FAILURE**: Prints descriptive error message and exits with status 1
- **NETWORK ERROR**: "dial tcp: lookup gitlab.com" or similar indicates network connectivity issues

## Validation Scenarios

### ALWAYS Test These Scenarios After Making Changes
1. **Build Test**: `go build -o stats-generator main.go` must complete successfully
2. **Environment Validation**: Run without environment variables to verify error handling
3. **Template Validation**: Ensure `gitlab_stats.svg` and/or `azure_devops_stats.svg` templates exist and are valid
4. **Dependency Check**: `go mod tidy` and `go mod download` must complete without errors

### Manual Testing Steps
1. Build the application: `go build -o stats-generator main.go`
2. Test error handling: `./stats-generator` (should fail with descriptive message)
3. Verify templates exist: `ls -la gitlab_stats.svg azure_devops_stats.svg`
4. Check generated output format by examining the SVG template placeholders

## GitHub Actions Workflows

### GitLab Stats Workflow
- **File**: `.github/workflows/update-gitlab-stats.yml`
- **Schedule**: Runs daily at midnight UTC (`0 0 * * *`)
- **Manual Trigger**: Can be triggered via `workflow_dispatch`
- **Go Version**: Uses `actions/setup-go@v4` with version `1.23.4`
- **Dependencies**: Cached using `cache-dependency-path: go.sum`
- **Secrets required**: `GITLAB_USERNAME`, `GITLAB_ACCESS_TOKEN`
- **Pushes to**: `gitlab-stats` branch (amends last commit and force-pushes)

### Azure DevOps Stats Workflow
- **File**: `.github/workflows/update-azure-devops-stats.yml`
- **Schedule**: Runs daily at midnight UTC (`0 0 * * *`)
- **Manual Trigger**: Can be triggered via `workflow_dispatch`
- **Go Version**: Uses `actions/setup-go@v4` with version `1.23.4`
- **Dependencies**: Cached using `cache-dependency-path: go.sum`
- **Secrets required**: `AZURE_DEVOPS_ORG`, `AZURE_DEVOPS_ACCESS_TOKEN`
- **Pushes to**: `azure-devops-stats` branch (amends last commit and force-pushes)

### Workflow Validation
- Both workflows amend the last commit and force-push HEAD to their dedicated stats branch
- Build process: `go run main.go` (no separate build step required)

## Repository Structure

### Key Files
```
├── main.go                          # Main application (480 lines)
├── go.mod                          # Go module definition (Go 1.26.0)
├── go.sum                          # Dependency checksums
├── gitlab_stats.svg                # GitLab SVG template with placeholders
├── azure_devops_stats.svg          # Azure DevOps SVG template with placeholders
├── CONTRIBUTING.md                 # Contribution guidelines
├── .github/workflows/              # GitHub Actions
│   ├── update-gitlab-stats.yml     # Daily GitLab stats update workflow
│   └── update-azure-devops-stats.yml # Daily Azure DevOps stats update workflow
├── .assets/                        # Custom SVG icons for README
│   ├── dependency-track.svg
│   ├── horusec.svg
│   ├── kali-linux.svg
│   ├── owasp.svg
│   └── semgrep.svg
├── README.md                       # Profile README with skills tables
├── CHANGELOG.md                    # Project changelog
└── LICENSE                         # MIT License
```

### Build Artifacts
- `stats-generator` - Compiled binary (gitignored)
- `gitlab_stats_final.svg` - Generated GitLab output file
- `azure_devops_stats_final.svg` - Generated Azure DevOps output file

## Common Tasks Reference

### Repository Root Contents
```
.assets/
.editorconfig
.git/
.github/
.gitignore
CHANGELOG.md
CONTRIBUTING.md
LICENSE
README.md
azure_devops_stats.svg
gitlab_stats.svg
go.mod
go.sum
main.go
```

### Go Module Info
```
module github.com/rios0rios0/rios0rios0
go 1.26.0
require golang.org/x/text v0.34.0
```

### Application Functionality
- **GitLab**: Fetches user statistics via GitLab REST API (commits, issues, merge requests from last 12 months)
- **Azure DevOps**: Fetches user statistics via Azure DevOps REST API (commits, pull requests, work items from last 12 months)
- Both platforms use pagination to handle large datasets (100 items per page)
- Generates SVG using Go's `fmt.Sprintf`-style formatting via `golang.org/x/text` printer
- Shared `GenerateSVG` function used for both platforms
- Template placeholders: `%[1]s` (title), `%[2]d` (commits), `%[3]d` (PRs/MRs), `%[4]d` (issues/work items), `%[5]d` (total contributions)
- The application runs whichever platforms have credentials set, and exits with an error only if no credentials are configured

## Troubleshooting

### Common Issues
- **"No platform credentials configured"**: Set at least one of `GITLAB_USERNAME`/`GITLAB_ACCESS_TOKEN` or `AZURE_DEVOPS_ORG`/`AZURE_DEVOPS_ACCESS_TOKEN`
- **"dial tcp: lookup gitlab.com"**: Network connectivity or DNS issues
- **"user not found"**: Invalid GitLab username or token permissions
- **"status code 401"**: Invalid or expired access token (GitLab or Azure DevOps)
- **"error fetching connection data"**: Invalid Azure DevOps organization name or access token

### File Permissions
- Ensure `gitlab_stats.svg` and `azure_devops_stats.svg` templates are readable
- Generated output SVG files require write permissions in working directory

### Dependencies
- If build fails, run `go mod download` to refresh dependencies
- If module issues occur, run `go mod tidy` to clean up
- All dependencies are publicly available, no private repositories required