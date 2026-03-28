//go:build unit

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

	t.Run("should compute language delta between earliest and latest snapshots", func(t *testing.T) {
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
		assert.Equal(t, int64(80000), result[2026][0].Stats.Languages["Go"])
		assert.Equal(t, int64(5000), result[2026][0].Stats.Languages["Python"])
		assert.Equal(t, int64(20000), result[2026][0].Stats.Languages["Rust"])
	})

	t.Run("should clamp negative language delta to zero when repo deleted", func(t *testing.T) {
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
		assert.Equal(t, int64(20000), result[2026][0].Stats.Languages["Go"])
		assert.Equal(t, int64(0), result[2026][0].Stats.Languages["OldLang"])
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

	t.Run("should accumulate language bytes from tree entries", func(t *testing.T) {
		// given
		repoMetaJSON, _ := json.Marshal(map[string]string{"defaultBranch": "refs/heads/main"})
		commitsJSON, _ := json.Marshal(map[string]interface{}{
			"value": []map[string]string{{"treeId": "abc123"}},
		})
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
		responses := [][]byte{repoMetaJSON, commitsJSON, treeJSON}

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
		assert.Equal(t, int64(8000), stats.Languages["HCL"])
		assert.Equal(t, int64(2000), stats.Languages["TypeScript"])
		assert.Equal(t, int64(500), stats.Languages["Markdown"])
		assert.Zero(t, stats.Languages["tree"])
	})

	t.Run("should skip repos with no commits", func(t *testing.T) {
		// given
		repoMetaJSON, _ := json.Marshal(map[string]string{"defaultBranch": "refs/heads/main"})
		commitsJSON, _ := json.Marshal(map[string]interface{}{"value": []map[string]string{}})

		callIndex := 0
		responses := [][]byte{repoMetaJSON, commitsJSON}

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
