//go:build unit

package main

import (
	"os"
	"path/filepath"
	"testing"

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

	t.Run("should nest languages by platform from multiple sources", func(t *testing.T) {
		// given
		named := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{Languages: map[string]int64{"Go": 50000, "Python": 30000}}},
			{PlatformGitLab, &PlatformStats{Languages: map[string]int64{"Go": 20000, "Java": 10000}}},
		}

		// when
		result := aggregateLanguagesByPlatform(named)

		// then
		assert.Equal(t, int64(50000), result["Go"][PlatformGitHub])
		assert.Equal(t, int64(20000), result["Go"][PlatformGitLab])
		assert.Equal(t, int64(30000), result["Python"][PlatformGitHub])
		assert.Equal(t, int64(10000), result["Java"][PlatformGitLab])
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
		os.WriteFile(path, []byte(content), 0644)

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
}
