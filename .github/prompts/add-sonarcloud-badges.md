# Add SonarCloud Badges to All Repositories

## Objective

Add the missing SonarCloud **Coverage** and **Quality Gate** badges to every repository under this workspace that already has the standard shields.io badge block in its `README.md` but is missing the SonarCloud badges.

## Context

All repositories under `rios0rios0` follow a standard README badge block pattern:

```html
<p align="center">
    <a href="...releases/latest"><img ... alt="Latest Release"/></a>
    <a href="...LICENSE"><img ... alt="License"/></a>
    <a href="...actions/workflows/default.yaml"><img ... alt="Build Status"/></a>
    <!-- SonarCloud badges should go HERE -->
</p>
```

Some repos already have the SonarCloud badges (17 repos). The rest (~47 repos) are missing them.

## Badge Format

The two badges to add use this exact format (replace `{REPO}` with the repo directory name):

```html
    <a href="https://sonarcloud.io/summary/overall?id=rios0rios0_{REPO}">
        <img src="https://img.shields.io/sonar/coverage/rios0rios0_{REPO}?server=https%3A%2F%2Fsonarcloud.io&style=for-the-badge&logo=sonarqubecloud" alt="Coverage"/></a>
    <a href="https://sonarcloud.io/summary/overall?id=rios0rios0_{REPO}">
        <img src="https://img.shields.io/sonar/quality_gate/rios0rios0_{REPO}?server=https%3A%2F%2Fsonarcloud.io&style=for-the-badge&logo=sonarqubecloud" alt="Quality Gate"/></a>
```

The SonarCloud project key uses the format `rios0rios0_{REPO}` where `{REPO}` is the repository directory name with hyphens preserved (e.g., `rios0rios0_ronin-to-koinly`).

## Rules

1. **Only modify repos that have the standard `<p align="center">` badge block** with at least one `img.shields.io` badge.
2. **Skip repos that already have SonarCloud badges** (contain `sonarcloud.io` or `sonarqubecloud` in the README).
3. **Insert the two SonarCloud badges** immediately after the last existing badge `</a>` and before `</p>`.
4. **Preserve exact indentation** — use 4 spaces, matching the existing badge lines.
5. **Do not modify anything else** in the README — no reformatting, no other changes.
6. **Follow the bulk operations workflow**: discover repos, apply changes, branch (`chore/add-sonarcloud-badges`), commit (`chore(docs): added SonarCloud coverage and quality gate badges`), push, and create PRs.

## Execution Steps

1. **Discover** all git repositories under the workspace root.
2. **For each repo**, read `README.md` and check:
   - Does it have the `<p align="center">` badge block with `img.shields.io`?
   - Does it already contain `sonarcloud` or `sonarqubecloud`?
   - If it has badges but no SonarCloud → apply the change.
3. **Insert** the two SonarCloud badge lines after the last `</a>` inside the `<p align="center">` block.
4. **Git operations**: stash, fetch, rebase, branch, commit, push, restore (per bulk operations standard).
5. **Create PRs** using the detected vendor CLI.

## Commit Message

```
chore(docs): added SonarCloud coverage and quality gate badges
```

## PR Title and Body

**Title:** `chore(docs): added SonarCloud badges`

**Body:**
```markdown
## Summary
- added SonarCloud Coverage and Quality Gate badges to the README

## Test plan
- [ ] verify badges render correctly on the repository page
- [ ] verify SonarCloud links point to the correct project
```
