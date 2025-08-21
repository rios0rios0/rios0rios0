# GitLab Stats Generator

A simple Go application that fetches GitLab user statistics via the GitLab API and generates an SVG visualization for GitHub profile README files. The application runs as a GitHub Actions workflow that updates the stats daily.

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

## Working Effectively

### Bootstrap and Build Process
- Download dependencies: `go mod download` -- completes in under 2 seconds
- Build the application: `go build -o bin/gitlab-stats main.go` -- takes 10-20 seconds. NEVER CANCEL.
- Run directly: `go run main.go` -- builds and runs in under 1 second for execution
- Clean dependencies: `go mod tidy` -- completes in under 1 second

### Environment Requirements
- **REQUIRED**: Go 1.23.4 or compatible (application uses `go 1.23.4` in go.mod)
- **REQUIRED**: `GITLAB_USERNAME` environment variable
- **REQUIRED**: `GITLAB_ACCESS_TOKEN` environment variable (GitLab personal access token)
- **REQUIRED**: Network access to `gitlab.com` API endpoints
- **REQUIRED**: `gitlab_stats.svg` template file must exist in the working directory

### Dependencies
- Single external dependency: `golang.org/x/text v0.21.0` for internationalization
- Total dependency graph: 6 modules (very lightweight)
- No additional build tools or linters required

## Build and Test Commands

### Essential Commands
```bash
# Install dependencies
go mod download

# Build application
mkdir -p bin
go build -o bin/gitlab-stats main.go

# Run application
GITLAB_USERNAME=your_username GITLAB_ACCESS_TOKEN=your_token go run main.go

# Clean workspace
go clean -cache
rm -rf bin/
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
- **VALIDATION**: Application validates by checking environment variables on startup
- **ERROR HANDLING**: Application exits with status 1 and descriptive error messages on failure

## Running the Application

### Environment Setup
```bash
export GITLAB_USERNAME=your_gitlab_username
export GITLAB_ACCESS_TOKEN=your_personal_access_token
```

### Execution
```bash
# Direct execution with environment variables
GITLAB_USERNAME=username GITLAB_ACCESS_TOKEN=token go run main.go

# OR using built binary
./bin/gitlab-stats
```

### Expected Output
- **SUCCESS**: Prints "SVG generated successfully..." and creates `gitlab_stats_final.svg`
- **FAILURE**: Prints descriptive error message and exits with status 1
- **NETWORK ERROR**: "dial tcp: lookup gitlab.com" indicates network connectivity issues

## Validation Scenarios

### ALWAYS Test These Scenarios After Making Changes
1. **Build Test**: `go build -o bin/gitlab-stats main.go` must complete successfully
2. **Environment Validation**: Run without environment variables to verify error handling
3. **Template Validation**: Ensure `gitlab_stats.svg` template exists and is valid
4. **Dependency Check**: `go mod tidy` and `go mod download` must complete without errors

### Manual Testing Steps
1. Build the application: `go build -o bin/gitlab-stats main.go`
2. Test error handling: `./bin/gitlab-stats` (should fail with descriptive message)
3. Verify template exists: `ls -la gitlab_stats.svg`
4. Check generated output format by examining `gitlab_stats.svg` template placeholders

## GitHub Actions Workflow

### Workflow Configuration
- **File**: `.github/workflows/update-gitlab-stats.yml`
- **Schedule**: Runs daily at midnight UTC (`0 0 * * *`)
- **Manual Trigger**: Can be triggered via `workflow_dispatch`
- **Go Version**: Uses `actions/setup-go@v4` with version `1.23.4`
- **Dependencies**: Cached using `cache-dependency-path: go.sum`

### Workflow Validation
- ALWAYS ensure secrets `GITLAB_USERNAME` and `GITLAB_ACCESS_TOKEN` are configured in GitHub repository settings
- Workflow pushes changes to `gitlab-stats` branch, not main branch
- Build process: `go run main.go` (no separate build step required)

## Repository Structure

### Key Files
```
├── main.go                          # Main application (192 lines)
├── go.mod                          # Go module definition (Go 1.23.4)
├── go.sum                          # Dependency checksums
├── gitlab_stats.svg                # SVG template with placeholders
├── .github/workflows/              # GitHub Actions
│   └── update-gitlab-stats.yml     # Daily update workflow
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
- `bin/gitlab-stats` - Compiled binary (gitignored)
- `gitlab_stats_final.svg` - Generated output file

## Common Tasks Reference

### Repository Root Contents
```
.assets/
.editorconfig
.git/
.github/
.gitignore
CHANGELOG.md
LICENSE
README.md
gitlab_stats.svg
go.mod
go.sum
main.go
```

### Go Module Info
```
module github.com/rios0rios0/rios0rios0
go 1.23.4
require golang.org/x/text v0.21.0
```

### Application Functionality
- Fetches GitLab user statistics via REST API
- Processes: commits, issues, merge requests from last 12 months
- Uses pagination to handle large datasets (100 items per page)
- Generates SVG using Go's text template with printf-style formatting
- Template placeholders: `%[1]s` (title), `%[2]d` (commits), `%[3]d` (MRs), `%[4]d` (issues), `%[5]d` (total contributions)

## Troubleshooting

### Common Issues
- **"environment variables are required"**: Set `GITLAB_USERNAME` and `GITLAB_ACCESS_TOKEN`
- **"dial tcp: lookup gitlab.com"**: Network connectivity or DNS issues
- **"user not found"**: Invalid GitLab username or token permissions
- **"status code 401"**: Invalid or expired GitLab access token

### File Permissions
- Ensure `gitlab_stats.svg` template is readable
- Build directory `bin/` must be writable
- Generated `gitlab_stats_final.svg` requires write permissions in working directory

### Dependencies
- If build fails, run `go mod download` to refresh dependencies
- If module issues occur, run `go mod tidy` to clean up
- All dependencies are publicly available, no private repositories required