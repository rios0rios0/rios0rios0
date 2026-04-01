//go:build unit

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatNumber(t *testing.T) {
	t.Parallel()

	t.Run("should return the number as-is when below 1000", func(t *testing.T) {
		// given
		values := map[int]string{
			0:   "0",
			1:   "1",
			42:  "42",
			999: "999",
		}

		for input, expected := range values {
			// when
			result := formatNumber(input)

			// then
			assert.Equal(t, expected, result)
		}
	})

	t.Run("should format with K suffix when in thousands", func(t *testing.T) {
		// given
		values := map[int]string{
			1000:  "1.0K",
			1500:  "1.5K",
			10000: "10.0K",
			99999: "100.0K",
		}

		for input, expected := range values {
			// when
			result := formatNumber(input)

			// then
			assert.Equal(t, expected, result)
		}
	})

	t.Run("should format with M suffix when in millions", func(t *testing.T) {
		// given
		values := map[int]string{
			1000000:  "1.0M",
			2500000:  "2.5M",
			10000000: "10.0M",
		}

		for input, expected := range values {
			// when
			result := formatNumber(input)

			// then
			assert.Equal(t, expected, result)
		}
	})

	t.Run("should format with B suffix when in billions", func(t *testing.T) {
		// given
		values := map[int]string{
			1000000000: "1.0B",
			2500000000: "2.5B",
		}

		for input, expected := range values {
			// when
			result := formatNumber(input)

			// then
			assert.Equal(t, expected, result)
		}
	})
}

func TestTopNLanguagesForPlatform(t *testing.T) {
	t.Parallel()

	t.Run("should return empty map for empty input", func(t *testing.T) {
		// given
		langs := map[string]int64{}

		// when
		result := topNLanguagesForPlatform(langs, 5)

		// then
		assert.Empty(t, result)
	})

	t.Run("should return empty map when all values are zero", func(t *testing.T) {
		// given
		langs := map[string]int64{"Go": 0, "Python": 0}

		// when
		result := topNLanguagesForPlatform(langs, 5)

		// then
		assert.Empty(t, result)
	})

	t.Run("should return all languages when fewer than N", func(t *testing.T) {
		// given
		langs := map[string]int64{"Go": 700, "Python": 300}

		// when
		result := topNLanguagesForPlatform(langs, 5)

		// then
		assert.Len(t, result, 2)
		assert.Equal(t, int64(7000), result["Go"])
		assert.Equal(t, int64(3000), result["Python"])
	})

	t.Run("should keep only top N languages by value", func(t *testing.T) {
		// given
		langs := map[string]int64{
			"Go": 500, "Python": 300, "Java": 200,
			"Rust": 100, "Shell": 50, "Ruby": 30, "C": 20,
		}

		// when
		result := topNLanguagesForPlatform(langs, 5)

		// then
		assert.Len(t, result, 5)
		assert.Contains(t, result, "Go")
		assert.Contains(t, result, "Python")
		assert.Contains(t, result, "Java")
		assert.Contains(t, result, "Rust")
		assert.Contains(t, result, "Shell")
		assert.NotContains(t, result, "Ruby")
		assert.NotContains(t, result, "C")
	})

	t.Run("should normalize values to percentage scale", func(t *testing.T) {
		// given
		langs := map[string]int64{"Go": 500, "Python": 500}

		// when
		result := topNLanguagesForPlatform(langs, 5)

		// then
		assert.Equal(t, int64(5000), result["Go"])
		assert.Equal(t, int64(5000), result["Python"])
	})
}

func TestAggregateLanguagesByPlatform(t *testing.T) {
	t.Parallel()

	t.Run("should return empty map when no stats are provided", func(t *testing.T) {
		// given
		var named []NamedPlatformStats

		// when
		result := aggregateLanguagesByPlatform(named)

		// then
		assert.Empty(t, result)
	})

	t.Run("should normalize and nest languages by platform from multiple sources", func(t *testing.T) {
		// given
		named := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{Languages: map[string]int64{"Go": 50000, "Python": 30000}}},
			{PlatformGitLab, &PlatformStats{Languages: map[string]int64{"Go": 20000, "Java": 10000}}},
		}

		// when
		result := aggregateLanguagesByPlatform(named)

		// then
		// GitHub: Go=50000/80000*10000=6250, Python=30000/80000*10000=3750
		assert.Equal(t, int64(6250), result["Go"][PlatformGitHub])
		assert.Equal(t, int64(3750), result["Python"][PlatformGitHub])
		// GitLab: Go=20000/30000*10000=6666, Java=10000/30000*10000=3333
		assert.Equal(t, int64(6666), result["Go"][PlatformGitLab])
		assert.Equal(t, int64(3333), result["Java"][PlatformGitLab])
	})

	t.Run("should give equal weight to platforms with different raw scales", func(t *testing.T) {
		// given
		named := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{Languages: map[string]int64{"Go": 5000000}}},
			{PlatformAzureDevOps, &PlatformStats{Languages: map[string]int64{"Go": 50}}},
		}

		// when
		result := aggregateLanguagesByPlatform(named)

		// then
		// Both platforms have Go as 100% of their total, so both normalize to 10000
		assert.Equal(t, int64(10000), result["Go"][PlatformGitHub])
		assert.Equal(t, int64(10000), result["Go"][PlatformAzureDevOps])
	})

	t.Run("should filter to top 5 per platform when more than 5 languages exist", func(t *testing.T) {
		// given
		named := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{Languages: map[string]int64{
				"Go": 500, "Python": 400, "Java": 300,
				"Rust": 200, "Shell": 100, "Ruby": 50, "C": 25,
			}}},
		}

		// when
		result := aggregateLanguagesByPlatform(named)

		// then
		assert.Len(t, result, 5)
		assert.Contains(t, result, "Go")
		assert.Contains(t, result, "Python")
		assert.Contains(t, result, "Java")
		assert.Contains(t, result, "Rust")
		assert.Contains(t, result, "Shell")
		assert.NotContains(t, result, "Ruby")
		assert.NotContains(t, result, "C")
	})
}

func TestClipDailyContributionsToRange(t *testing.T) {
	t.Parallel()

	t.Run("should keep entries within range and remove entries outside", func(t *testing.T) {
		// given
		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
		stats := &PlatformStats{
			DailyContributions: map[string]int{
				"2025-12-31": 3,
				"2026-01-01": 5,
				"2026-06-15": 2,
				"2026-12-31": 1,
				"2027-01-01": 4,
			},
		}

		// when
		clipDailyContributionsToRange(stats, from, to)

		// then
		assert.Equal(t, 3, len(stats.DailyContributions))
		assert.Equal(t, 5, stats.DailyContributions["2026-01-01"])
		assert.Equal(t, 2, stats.DailyContributions["2026-06-15"])
		assert.Equal(t, 1, stats.DailyContributions["2026-12-31"])
		assert.NotContains(t, stats.DailyContributions, "2025-12-31")
		assert.NotContains(t, stats.DailyContributions, "2027-01-01")
	})

	t.Run("should handle empty contributions map", func(t *testing.T) {
		// given
		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
		stats := &PlatformStats{
			DailyContributions: map[string]int{},
		}

		// when
		clipDailyContributionsToRange(stats, from, to)

		// then
		assert.Empty(t, stats.DailyContributions)
	})

	t.Run("should remove entries with unparseable dates", func(t *testing.T) {
		// given
		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
		stats := &PlatformStats{
			DailyContributions: map[string]int{
				"invalid-date": 1,
				"2026-06-15":   2,
			},
		}

		// when
		clipDailyContributionsToRange(stats, from, to)

		// then
		assert.Equal(t, 1, len(stats.DailyContributions))
		assert.Equal(t, 2, stats.DailyContributions["2026-06-15"])
	})
}

func TestAggregateContributionsByPlatform(t *testing.T) {
	t.Parallel()

	t.Run("should return empty map when no stats are provided", func(t *testing.T) {
		// given
		var named []NamedPlatformStats

		// when
		result := aggregateContributionsByPlatform(named)

		// then
		assert.Empty(t, result)
	})

	t.Run("should nest contributions by platform and date", func(t *testing.T) {
		// given
		named := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{DailyContributions: map[string]int{"2026-01-01": 5, "2026-01-02": 3}}},
			{PlatformGitLab, &PlatformStats{DailyContributions: map[string]int{"2026-01-01": 2, "2026-01-03": 7}}},
		}

		// when
		result := aggregateContributionsByPlatform(named)

		// then
		assert.Equal(t, 5, result["2026-01-01"][PlatformGitHub])
		assert.Equal(t, 2, result["2026-01-01"][PlatformGitLab])
		assert.Equal(t, 3, result["2026-01-02"][PlatformGitHub])
		assert.Equal(t, 7, result["2026-01-03"][PlatformGitLab])
	})
}

func TestLoadStatsHistory(t *testing.T) {
	t.Parallel()

	t.Run("should return empty history when file does not exist", func(t *testing.T) {
		// given
		path := filepath.Join(t.TempDir(), "nonexistent.json")

		// when
		result, err := loadStatsHistory(path)

		// then
		require.NoError(t, err)
		assert.Equal(t, 1, result.Version)
		assert.Empty(t, result.Snapshots)
	})

	t.Run("should load valid history file", func(t *testing.T) {
		// given
		dir := t.TempDir()
		path := filepath.Join(dir, "history.json")
		content := `{"version":1,"snapshots":[{"date":"2026-03-27","platforms":{"GitHub":{"total_commits":100,"total_prs_or_mrs":10,"total_issues_or_wis":5,"languages":{"Go":50000},"daily_contributions":{"2026-03-27":5}}}}]}`
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))

		// when
		result, err := loadStatsHistory(path)

		// then
		require.NoError(t, err)
		assert.Equal(t, 1, len(result.Snapshots))
		assert.Equal(t, 100, result.Snapshots[0].Platforms[PlatformGitHub].TotalCommits)
	})
}

func TestAddSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("should add a new snapshot", func(t *testing.T) {
		// given
		history := &StatsHistory{Version: 1}
		stats := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{TotalCommits: 100, Languages: map[string]int64{"Go": 50000}, DailyContributions: map[string]int{"2026-03-27": 5}}},
		}

		// when
		addSnapshot(history, "2026-03-27", stats)

		// then
		assert.Equal(t, 1, len(history.Snapshots))
		assert.Equal(t, "2026-03-27", history.Snapshots[0].Date)
		assert.Equal(t, 100, history.Snapshots[0].Platforms[PlatformGitHub].TotalCommits)
	})

	t.Run("should replace existing snapshot for the same date", func(t *testing.T) {
		// given
		history := &StatsHistory{Version: 1}
		stats1 := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{TotalCommits: 100, Languages: map[string]int64{}, DailyContributions: map[string]int{}}},
		}
		stats2 := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{TotalCommits: 200, Languages: map[string]int64{}, DailyContributions: map[string]int{}}},
		}
		addSnapshot(history, "2026-03-27", stats1)

		// when
		addSnapshot(history, "2026-03-27", stats2)

		// then
		assert.Equal(t, 1, len(history.Snapshots))
		assert.Equal(t, 200, history.Snapshots[0].Platforms[PlatformGitHub].TotalCommits)
	})
}

func TestAccumulateByYear(t *testing.T) {
	t.Parallel()

	t.Run("should accumulate stats for a single year", func(t *testing.T) {
		// given
		history := &StatsHistory{
			Version: 1,
			Snapshots: []DailySnapshot{
				{
					Date: "2026-03-27",
					Platforms: map[PlatformName]PlatformSnapshot{
						PlatformGitHub: {
							TotalCommits: 100, TotalPRsOrMRs: 10, TotalIssuesOrWIs: 5,
							Languages:          map[string]int64{"Go": 50000},
							DailyContributions: map[string]int{"2026-03-27": 5, "2026-03-26": 3},
						},
					},
				},
			},
		}

		// when
		result := accumulateByYear(history)

		// then
		assert.Contains(t, result, 2026)
		assert.Equal(t, 1, len(result[2026]))
		assert.Equal(t, 100, result[2026][0].Stats.TotalCommits)
		assert.Equal(t, 5, result[2026][0].Stats.DailyContributions["2026-03-27"])
	})

	t.Run("should distribute contributions to correct years", func(t *testing.T) {
		// given
		history := &StatsHistory{
			Version: 1,
			Snapshots: []DailySnapshot{
				{
					Date: "2026-01-02",
					Platforms: map[PlatformName]PlatformSnapshot{
						PlatformGitHub: {
							TotalCommits: 50,
							Languages:    map[string]int64{},
							DailyContributions: map[string]int{
								"2025-12-31": 3,
								"2026-01-01": 7,
								"2026-01-02": 2,
							},
						},
					},
				},
			},
		}

		// when
		result := accumulateByYear(history)

		// then
		assert.Contains(t, result, 2025)
		assert.Contains(t, result, 2026)
		assert.Equal(t, 3, result[2025][0].Stats.DailyContributions["2025-12-31"])
		assert.Equal(t, 7, result[2026][0].Stats.DailyContributions["2026-01-01"])
	})

	t.Run("should take max contribution count across snapshots", func(t *testing.T) {
		// given
		history := &StatsHistory{
			Version: 1,
			Snapshots: []DailySnapshot{
				{
					Date: "2026-03-26",
					Platforms: map[PlatformName]PlatformSnapshot{
						PlatformGitHub: {TotalCommits: 90, Languages: map[string]int64{}, DailyContributions: map[string]int{"2026-03-25": 3}},
					},
				},
				{
					Date: "2026-03-27",
					Platforms: map[PlatformName]PlatformSnapshot{
						PlatformGitHub: {TotalCommits: 100, Languages: map[string]int64{}, DailyContributions: map[string]int{"2026-03-25": 5}},
					},
				},
			},
		}

		// when
		result := accumulateByYear(history)

		// then
		assert.Equal(t, 100, result[2026][0].Stats.TotalCommits)
		assert.Equal(t, 5, result[2026][0].Stats.DailyContributions["2026-03-25"])
	})

	t.Run("should use absolute language values for single snapshot", func(t *testing.T) {
		// given
		history := &StatsHistory{
			Version: 1,
			Snapshots: []DailySnapshot{
				{
					Date: "2026-03-27",
					Platforms: map[PlatformName]PlatformSnapshot{
						PlatformGitHub: {
							TotalCommits:       100,
							Languages:          map[string]int64{"Go": 50000, "Python": 30000},
							DailyContributions: map[string]int{},
						},
					},
				},
			},
		}

		// when
		result := accumulateByYear(history)

		// then
		assert.Equal(t, int64(50000), result[2026][0].Stats.Languages["Go"])
		assert.Equal(t, int64(30000), result[2026][0].Stats.Languages["Python"])
	})

	t.Run("should use max language bytes across multiple snapshots", func(t *testing.T) {
		// given
		history := &StatsHistory{
			Version: 1,
			Snapshots: []DailySnapshot{
				{
					Date: "2026-01-15",
					Platforms: map[PlatformName]PlatformSnapshot{
						PlatformGitHub: {
							TotalCommits:       50,
							Languages:          map[string]int64{"Go": 100000, "Python": 50000},
							DailyContributions: map[string]int{},
						},
					},
				},
				{
					Date: "2026-03-27",
					Platforms: map[PlatformName]PlatformSnapshot{
						PlatformGitHub: {
							TotalCommits:       150,
							Languages:          map[string]int64{"Go": 180000, "Python": 55000, "Rust": 20000},
							DailyContributions: map[string]int{},
						},
					},
				},
			},
		}

		// when
		result := accumulateByYear(history)

		// then
		assert.Equal(t, int64(180000), result[2026][0].Stats.Languages["Go"])
		assert.Equal(t, int64(55000), result[2026][0].Stats.Languages["Python"])
		assert.Equal(t, int64(20000), result[2026][0].Stats.Languages["Rust"])
	})

	t.Run("should preserve language from earlier snapshot when later snapshot has zero", func(t *testing.T) {
		// given
		history := &StatsHistory{
			Version: 1,
			Snapshots: []DailySnapshot{
				{
					Date: "2026-01-15",
					Platforms: map[PlatformName]PlatformSnapshot{
						PlatformGitHub: {
							Languages:          map[string]int64{"Go": 100000, "OldLang": 50000},
							DailyContributions: map[string]int{},
						},
					},
				},
				{
					Date: "2026-03-27",
					Platforms: map[PlatformName]PlatformSnapshot{
						PlatformGitHub: {
							Languages:          map[string]int64{"Go": 120000},
							DailyContributions: map[string]int{},
						},
					},
				},
			},
		}

		// when
		result := accumulateByYear(history)

		// then
		assert.Equal(t, int64(120000), result[2026][0].Stats.Languages["Go"])
		assert.Equal(t, int64(50000), result[2026][0].Stats.Languages["OldLang"])
	})
}

func TestRemoveSnapshotsForYear(t *testing.T) {
	t.Parallel()

	t.Run("should remove all snapshots for the target year", func(t *testing.T) {
		// given
		history := &StatsHistory{
			Version: 1,
			Snapshots: []DailySnapshot{
				{Date: "2025-03-15", Platforms: map[PlatformName]PlatformSnapshot{}},
				{Date: "2025-06-20", Platforms: map[PlatformName]PlatformSnapshot{}},
				{Date: "2026-01-10", Platforms: map[PlatformName]PlatformSnapshot{}},
			},
		}

		// when
		removeSnapshotsForYear(history, 2025)

		// then
		require.Equal(t, 1, len(history.Snapshots))
		assert.Equal(t, "2026-01-10", history.Snapshots[0].Date)
	})

	t.Run("should preserve all snapshots when year has no matches", func(t *testing.T) {
		// given
		history := &StatsHistory{
			Version: 1,
			Snapshots: []DailySnapshot{
				{Date: "2026-03-27", Platforms: map[PlatformName]PlatformSnapshot{}},
			},
		}

		// when
		removeSnapshotsForYear(history, 2025)

		// then
		require.Equal(t, 1, len(history.Snapshots))
		assert.Equal(t, "2026-03-27", history.Snapshots[0].Date)
	})

	t.Run("should handle empty history gracefully", func(t *testing.T) {
		// given
		history := &StatsHistory{Version: 1}

		// when
		removeSnapshotsForYear(history, 2025)

		// then
		assert.Empty(t, history.Snapshots)
	})
}

func TestFetchAzureDevOpsLanguages(t *testing.T) {
	t.Parallel()

	t.Run("should accumulate weighted language bytes from tree entries", func(t *testing.T) {
		// given
		repoMetaJSON, _ := json.Marshal(map[string]string{"defaultBranch": "refs/heads/main"})
		allCommitsJSON, _ := json.Marshal(map[string]interface{}{"count": 10}) // 10 total commits
		latestCommitJSON, _ := json.Marshal(map[string]interface{}{
			"value": []map[string]string{{"commitId": "sha123"}},
		})
		commitDetailJSON, _ := json.Marshal(map[string]string{"treeId": "abc123"})
		treeJSON, _ := json.Marshal(map[string]interface{}{
			"truncated": false,
			"treeEntries": []map[string]interface{}{
				{"relativePath": "main.tf", "gitObjectType": "blob", "size": 5000},
				{"relativePath": "variables.tf", "gitObjectType": "blob", "size": 3000},
				{"relativePath": "src/app.ts", "gitObjectType": "blob", "size": 2000},
				{"relativePath": "src", "gitObjectType": "tree", "size": 0},
				{"relativePath": "README.md", "gitObjectType": "blob", "size": 500},
			},
		})

		callIndex := 0
		responses := [][]byte{repoMetaJSON, allCommitsJSON, latestCommitJSON, commitDetailJSON, treeJSON}

		newRequest := func(method, url string, body io.Reader) (*http.Request, error) {
			return http.NewRequest(method, url, body)
		}
		doRequest := func(req *http.Request) ([]byte, int, error) {
			idx := callIndex
			callIndex++
			if idx < len(responses) {
				return responses[idx], http.StatusOK, nil
			}
			return nil, http.StatusNotFound, fmt.Errorf("unexpected call %d", idx)
		}

		stats := &PlatformStats{Languages: make(map[string]int64)}
		// User made 5 out of 10 total commits -> weight = 0.5
		repos := []adoRepoRef{{ProjectID: "proj1", RepoID: "repo1", UserCommits: 5}}

		// when
		fetchAzureDevOpsLanguages(newRequest, doRequest, "myorg", repos, stats)

		// then (bytes * 0.5 weight)
		assert.Equal(t, int64(4000), stats.Languages["HCL"])
		assert.Equal(t, int64(1000), stats.Languages["TypeScript"])
		assert.Equal(t, int64(250), stats.Languages["Markdown"])
		assert.Zero(t, stats.Languages["tree"])
	})

	t.Run("should exclude vendored and generated paths from language counts", func(t *testing.T) {
		// given
		repoMetaJSON, _ := json.Marshal(map[string]string{"defaultBranch": "refs/heads/main"})
		allCommitsJSON, _ := json.Marshal(map[string]interface{}{"count": 1})
		latestCommitJSON, _ := json.Marshal(map[string]interface{}{
			"value": []map[string]string{{"commitId": "sha456"}},
		})
		commitDetailJSON, _ := json.Marshal(map[string]string{"treeId": "tree456"})
		treeJSON, _ := json.Marshal(map[string]interface{}{
			"truncated": false,
			"treeEntries": []map[string]interface{}{
				{"relativePath": "src/main.go", "gitObjectType": "blob", "size": 5000},
				{"relativePath": "node_modules/lodash/index.js", "gitObjectType": "blob", "size": 100000},
				{"relativePath": "vendor/github.com/pkg/errors/errors.go", "gitObjectType": "blob", "size": 20000},
				{"relativePath": "dist/bundle.js", "gitObjectType": "blob", "size": 50000},
				{"relativePath": "package-lock.json", "gitObjectType": "blob", "size": 80000},
				{"relativePath": "assets/app.min.js", "gitObjectType": "blob", "size": 30000},
				{"relativePath": "Pods/SomePod/lib.swift", "gitObjectType": "blob", "size": 15000},
				{"relativePath": "src/utils.ts", "gitObjectType": "blob", "size": 3000},
			},
		})

		callIndex := 0
		responses := [][]byte{repoMetaJSON, allCommitsJSON, latestCommitJSON, commitDetailJSON, treeJSON}

		newRequest := func(method, url string, body io.Reader) (*http.Request, error) {
			return http.NewRequest(method, url, body)
		}
		doRequest := func(req *http.Request) ([]byte, int, error) {
			idx := callIndex
			callIndex++
			if idx < len(responses) {
				return responses[idx], http.StatusOK, nil
			}
			return nil, http.StatusNotFound, fmt.Errorf("unexpected call %d", idx)
		}

		stats := &PlatformStats{Languages: make(map[string]int64)}
		repos := []adoRepoRef{{ProjectID: "proj1", RepoID: "repo1", UserCommits: 1}}

		// when
		fetchAzureDevOpsLanguages(newRequest, doRequest, "myorg", repos, stats)

		// then - only non-vendored files should be counted
		assert.Equal(t, int64(5000), stats.Languages["Go"])
		assert.Equal(t, int64(3000), stats.Languages["TypeScript"])
		assert.Zero(t, stats.Languages["JavaScript"]) // node_modules + dist excluded
		assert.Zero(t, stats.Languages["JSON"])        // package-lock.json excluded
		assert.Zero(t, stats.Languages["Swift"])       // Pods/ excluded
	})

	t.Run("should skip repos with no commits", func(t *testing.T) {
		// given
		repoMetaJSON, _ := json.Marshal(map[string]string{"defaultBranch": "refs/heads/main"})
		allCommitsJSON, _ := json.Marshal(map[string]interface{}{"count": 0})
		commitsJSON, _ := json.Marshal(map[string]interface{}{"value": []map[string]string{}})

		callIndex := 0
		responses := [][]byte{repoMetaJSON, allCommitsJSON, commitsJSON}

		newRequest := func(method, url string, body io.Reader) (*http.Request, error) {
			return http.NewRequest(method, url, body)
		}
		doRequest := func(req *http.Request) ([]byte, int, error) {
			idx := callIndex
			callIndex++
			if idx < len(responses) {
				return responses[idx], http.StatusOK, nil
			}
			return nil, http.StatusNotFound, fmt.Errorf("unexpected call %d", idx)
		}

		stats := &PlatformStats{Languages: make(map[string]int64)}
		repos := []adoRepoRef{{ProjectID: "proj1", RepoID: "repo1"}}

		// when
		fetchAzureDevOpsLanguages(newRequest, doRequest, "myorg", repos, stats)

		// then
		assert.Empty(t, stats.Languages)
	})
}

func TestFormatYearBlock(t *testing.T) {
	t.Parallel()

	t.Run("should produce well-formatted HTML block with correct URLs", func(t *testing.T) {
		// given
		year := 2025
		ghUsername := "testuser"

		// when
		result := formatYearBlock(year, ghUsername)

		// then
		assert.Contains(t, result, "<details>")
		assert.Contains(t, result, "<summary>2025</summary>")
		assert.Contains(t, result, "https://raw.githubusercontent.com/testuser/testuser/stats/combined_stats_2025.svg")
		assert.Contains(t, result, "https://raw.githubusercontent.com/testuser/testuser/stats/top_languages_2025.svg")
		assert.Contains(t, result, "https://raw.githubusercontent.com/testuser/testuser/stats/contributions_2025.svg")
		assert.Contains(t, result, `height="220"`)
		assert.Contains(t, result, "</details>")
		// Contributions image should NOT have height="220"
		lines := strings.Split(result, "\n")
		for _, line := range lines {
			if strings.Contains(line, "contributions_2025.svg") {
				assert.NotContains(t, line, `height="220"`)
			}
		}
	})
}

func TestUpdateReadmeYearSections(t *testing.T) {
	t.Parallel()

	t.Run("should insert new year in correct descending position", func(t *testing.T) {
		// given
		dir := t.TempDir()
		readmePath := filepath.Join(dir, "README.md")
		content := "<details>\n\t<summary>2025</summary>\n\t<div align=\"center\">\n\t\t<img src=\"x\" />\n\t</div>\n</details>\n" +
			"<details>\n\t<summary>2023</summary>\n\t<div align=\"center\">\n\t\t<img src=\"x\" />\n\t</div>\n</details>\n"
		os.WriteFile(readmePath, []byte(content), 0644)

		// when
		updateReadmeYearSections(readmePath, []int{2023, 2024, 2025}, "testuser")

		// then
		data, _ := os.ReadFile(readmePath)
		result := string(data)
		pos2025 := strings.Index(result, "<summary>2025</summary>")
		pos2024 := strings.Index(result, "<summary>2024</summary>")
		pos2023 := strings.Index(result, "<summary>2023</summary>")
		assert.Greater(t, pos2024, pos2025, "2024 should come after 2025")
		assert.Greater(t, pos2023, pos2024, "2023 should come after 2024")
	})

	t.Run("should skip when all years already exist", func(t *testing.T) {
		// given
		dir := t.TempDir()
		readmePath := filepath.Join(dir, "README.md")
		content := "<details>\n\t<summary>2025</summary>\n\t<div align=\"center\">\n\t\t<img src=\"x\" />\n\t</div>\n</details>\n"
		os.WriteFile(readmePath, []byte(content), 0644)

		// when
		updateReadmeYearSections(readmePath, []int{2025}, "testuser")

		// then
		data, _ := os.ReadFile(readmePath)
		assert.Equal(t, content, string(data))
	})

	t.Run("should handle missing README gracefully", func(t *testing.T) {
		// given
		dir := t.TempDir()
		readmePath := filepath.Join(dir, "NONEXISTENT.md")

		// when / then (should not panic)
		updateReadmeYearSections(readmePath, []int{2025}, "testuser")
	})

	t.Run("should append at end when new year is oldest", func(t *testing.T) {
		// given
		dir := t.TempDir()
		readmePath := filepath.Join(dir, "README.md")
		content := "<details>\n\t<summary>2025</summary>\n\t<div align=\"center\">\n\t\t<img src=\"x\" />\n\t</div>\n</details>\n"
		os.WriteFile(readmePath, []byte(content), 0644)

		// when
		updateReadmeYearSections(readmePath, []int{2024, 2025}, "testuser")

		// then
		data, _ := os.ReadFile(readmePath)
		result := string(data)
		pos2025 := strings.Index(result, "<summary>2025</summary>")
		pos2024 := strings.Index(result, "<summary>2024</summary>")
		assert.Greater(t, pos2024, pos2025, "2024 should come after 2025")
	})

	t.Run("should handle unreadable README gracefully", func(t *testing.T) {
		// given - use a directory path as the file path to cause a non-NotExist read error
		dir := t.TempDir()
		readmePath := dir // a directory, not a file

		// when / then (should not panic)
		updateReadmeYearSections(readmePath, []int{2025}, "testuser")
	})

	t.Run("should handle unwritable README gracefully", func(t *testing.T) {
		// given
		dir := t.TempDir()
		readmePath := filepath.Join(dir, "README.md")
		content := "<details>\n\t<summary>2025</summary>\n\t<div align=\"center\">\n\t\t<img src=\"x\" />\n\t</div>\n</details>\n"
		os.WriteFile(readmePath, []byte(content), 0644)
		// Make file unwritable
		os.Chmod(readmePath, 0444)

		// when / then (should not panic, just log)
		updateReadmeYearSections(readmePath, []int{2024, 2025}, "testuser")

		// cleanup
		os.Chmod(readmePath, 0644)
	})

	t.Run("should skip when GitHub username is empty", func(t *testing.T) {
		// given
		dir := t.TempDir()
		readmePath := filepath.Join(dir, "README.md")
		content := "<details>\n\t<summary>2025</summary>\n\t<div align=\"center\">\n\t\t<img src=\"x\" />\n\t</div>\n</details>\n"
		os.WriteFile(readmePath, []byte(content), 0644)

		// when
		updateReadmeYearSections(readmePath, []int{2024, 2025}, "")

		// then
		data, _ := os.ReadFile(readmePath)
		assert.Equal(t, content, string(data))
	})
}

func TestPlatformColorDefault(t *testing.T) {
	t.Parallel()

	t.Run("should return grey for unknown platform", func(t *testing.T) {
		// given
		p := PlatformName("unknown")

		// when
		color := p.Color()

		// then
		assert.Equal(t, "#8b949e", color)
	})
}

func TestPlatformToComboDefault(t *testing.T) {
	t.Parallel()

	t.Run("should return 0 for unknown platform", func(t *testing.T) {
		// given
		p := PlatformName("unknown")

		// when
		combo := platformToCombo(p)

		// then
		assert.Equal(t, PlatformCombo(0), combo)
	})
}

func TestAccumulateByYearEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("should track max repos across snapshots", func(t *testing.T) {
		// given
		history := &StatsHistory{
			Version: 1,
			Snapshots: []DailySnapshot{
				{
					Date: "2026-01-15",
					Platforms: map[PlatformName]PlatformSnapshot{
						PlatformGitHub: {
							TotalRepos:         5,
							Languages:          map[string]int64{},
							DailyContributions: map[string]int{},
						},
					},
				},
				{
					Date: "2026-03-15",
					Platforms: map[PlatformName]PlatformSnapshot{
						PlatformGitHub: {
							TotalRepos:         10,
							Languages:          map[string]int64{},
							DailyContributions: map[string]int{},
						},
					},
				},
			},
		}

		// when
		result := accumulateByYear(history)

		// then
		assert.Equal(t, 10, result[2026][0].Stats.TotalRepos)
	})

	t.Run("should skip contributions with invalid date", func(t *testing.T) {
		// given
		history := &StatsHistory{
			Version: 1,
			Snapshots: []DailySnapshot{
				{
					Date: "2026-01-15",
					Platforms: map[PlatformName]PlatformSnapshot{
						PlatformGitHub: {
							Languages:          map[string]int64{},
							DailyContributions: map[string]int{"bad-date": 5, "2026-03-15": 3},
						},
					},
				},
			},
		}

		// when
		result := accumulateByYear(history)

		// then
		assert.Equal(t, 3, result[2026][0].Stats.DailyContributions["2026-03-15"])
	})
}

func TestComboColorScaleCoverage(t *testing.T) {
	t.Parallel()

	t.Run("should return scale for all single-platform combos", func(t *testing.T) {
		// given
		combos := []PlatformCombo{comboGitHub, comboGitLab, comboAzureDevOps}

		for _, c := range combos {
			// when
			scale := comboColorScale(c)

			// then
			assert.NotEmpty(t, scale[0])
		}
	})

	t.Run("should return blended scale for multi-platform combos", func(t *testing.T) {
		// given
		combo := comboGitHub | comboGitLab

		// when
		scale := comboColorScale(combo)

		// then
		assert.NotEmpty(t, scale[0])
	})

	t.Run("should return grey for empty combo", func(t *testing.T) {
		// when
		scale := comboColorScale(0)

		// then
		assert.Equal(t, "#2a2f35", scale[0])
	})
}

func TestLoadStatsHistoryErrors(t *testing.T) {
	t.Parallel()

	t.Run("should return error when file contains invalid JSON", func(t *testing.T) {
		// given
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.json")
		os.WriteFile(path, []byte("not json"), 0644)

		// when
		history, err := loadStatsHistory(path)

		// then
		require.Error(t, err)
		assert.Nil(t, history)
	})

	t.Run("should return error when file exists but is unreadable", func(t *testing.T) {
		// given - use a directory as the path to trigger a non-NotExist read error
		dir := t.TempDir()

		// when
		history, err := loadStatsHistory(dir)

		// then
		require.Error(t, err)
		assert.Nil(t, history)
	})
}

func TestColorScale(t *testing.T) {
	t.Parallel()

	t.Run("should return distinct scales for each platform", func(t *testing.T) {
		// given
		platforms := []PlatformName{PlatformGitHub, PlatformGitLab, PlatformAzureDevOps}

		for _, p := range platforms {
			// when
			scale := p.ColorScale()

			// then
			assert.NotEmpty(t, scale[0])
			assert.NotEmpty(t, scale[3])
			assert.NotEqual(t, scale[0], scale[3])
		}
	})

	t.Run("should return grey scale for unknown platform", func(t *testing.T) {
		// given
		p := PlatformName("unknown")

		// when
		scale := p.ColorScale()

		// then
		assert.Equal(t, "#2a2f35", scale[0])
	})
}

func TestSaveStatsHistory(t *testing.T) {
	t.Parallel()

	t.Run("should save history to file as JSON", func(t *testing.T) {
		// given
		dir := t.TempDir()
		path := filepath.Join(dir, "history.json")
		history := &StatsHistory{
			Version: 1,
			Snapshots: []DailySnapshot{
				{Date: "2026-03-15", Platforms: map[PlatformName]PlatformSnapshot{}},
			},
		}

		// when
		err := saveStatsHistory(history, path)

		// then
		require.NoError(t, err)
		data, _ := os.ReadFile(path)
		var loaded StatsHistory
		require.NoError(t, json.Unmarshal(data, &loaded))
		assert.Equal(t, 1, len(loaded.Snapshots))
		assert.Equal(t, "2026-03-15", loaded.Snapshots[0].Date)
	})
}

func TestSaveStatsHistoryError(t *testing.T) {
	t.Parallel()

	t.Run("should return error when path is invalid", func(t *testing.T) {
		// given
		history := &StatsHistory{Version: 1}

		// when
		err := saveStatsHistory(history, "/nonexistent/dir/history.json")

		// then
		require.Error(t, err)
	})
}

func TestLoadTokenUsage(t *testing.T) {
	t.Parallel()

	t.Run("should load and sort token usage from file", func(t *testing.T) {
		// given
		dir := t.TempDir()
		path := filepath.Join(dir, "tokens.json")
		tokens := []TokenUsage{
			{Date: "2026-03-16", Tokens: 200},
			{Date: "2026-03-15", Tokens: 100},
		}
		data, _ := json.Marshal(tokens)
		os.WriteFile(path, data, 0644)

		// when
		result, err := loadTokenUsage(path)

		// then
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Equal(t, "2026-03-15", result[0].Date)
		assert.Equal(t, "2026-03-16", result[1].Date)
	})

	t.Run("should return error when file does not exist", func(t *testing.T) {
		// when
		result, err := loadTokenUsage("/nonexistent/path.json")

		// then
		require.Error(t, err)
		assert.Nil(t, result)
	})
}

func TestGetEnvOrDefault(t *testing.T) {
	t.Run("should return default when env var is not set", func(t *testing.T) {
		// when
		result := getEnvOrDefault("NONEXISTENT_TEST_VAR_XYZ", "fallback")

		// then
		assert.Equal(t, "fallback", result)
	})

	t.Run("should return env var value when set", func(t *testing.T) {
		// given
		t.Setenv("TEST_GET_ENV_OR_DEFAULT", "custom_value")

		// when
		result := getEnvOrDefault("TEST_GET_ENV_OR_DEFAULT", "fallback")

		// then
		assert.Equal(t, "custom_value", result)
	})
}

func TestGenerateCombinedStatsSVG(t *testing.T) {
	t.Parallel()

	t.Run("should write SVG file to disk", func(t *testing.T) {
		// given
		dir := t.TempDir()
		path := filepath.Join(dir, "combined.svg")
		stats := []NamedPlatformStats{
			{Platform: PlatformGitHub, Stats: &PlatformStats{
				TotalCommits: 10, Languages: make(map[string]int64),
				DailyContributions: make(map[string]int),
			}},
		}

		// when
		err := GenerateCombinedStatsSVG(stats, path)

		// then
		require.NoError(t, err)
		data, _ := os.ReadFile(path)
		assert.Contains(t, string(data), "<svg")
	})
}

func TestGenerateTokensHeatmap(t *testing.T) {
	t.Parallel()

	t.Run("should write SVG file to disk", func(t *testing.T) {
		// given
		dir := t.TempDir()
		path := filepath.Join(dir, "tokens.svg")
		tokens := []TokenUsage{
			{Date: "2026-03-15", Tokens: 100},
			{Date: "2026-03-16", Tokens: 200},
		}

		// when
		err := GenerateTokensHeatmap(tokens, path)

		// then
		require.NoError(t, err)
		data, _ := os.ReadFile(path)
		assert.Contains(t, string(data), "<svg")
	})

	t.Run("should return error with empty tokens", func(t *testing.T) {
		// given
		dir := t.TempDir()
		path := filepath.Join(dir, "tokens.svg")

		// when
		err := GenerateTokensHeatmap([]TokenUsage{}, path)

		// then
		require.Error(t, err)
	})
}

func TestGenerateLanguagesBarChart(t *testing.T) {
	t.Parallel()

	t.Run("should write SVG file to disk", func(t *testing.T) {
		// given
		dir := t.TempDir()
		path := filepath.Join(dir, "languages.svg")
		langs := map[string]map[PlatformName]int64{
			"Go": {PlatformGitHub: 50000},
		}

		// when
		err := GenerateLanguagesBarChart(langs, path)

		// then
		require.NoError(t, err)
		data, _ := os.ReadFile(path)
		assert.Contains(t, string(data), "<svg")
	})
}

func TestGenerateContributionHeatmap(t *testing.T) {
	t.Parallel()

	t.Run("should write SVG file to disk", func(t *testing.T) {
		// given
		dir := t.TempDir()
		path := filepath.Join(dir, "contributions.svg")
		contribs := map[string]map[PlatformName]int{
			"2026-03-15": {PlatformGitHub: 3},
		}
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

		// when
		err := GenerateContributionHeatmap(contribs, start, end, path)

		// then
		require.NoError(t, err)
		data, _ := os.ReadFile(path)
		assert.Contains(t, string(data), "<svg")
	})
}

func TestLanguageColor(t *testing.T) {
	t.Parallel()

	t.Run("should return known color for Go", func(t *testing.T) {
		// when
		color := languageColor("Go")

		// then
		assert.NotEqual(t, "#8b949e", color)
	})

	t.Run("should return default color for unknown language", func(t *testing.T) {
		// when
		color := languageColor("NonexistentLang12345")

		// then
		assert.Equal(t, "#8b949e", color)
	})
}
