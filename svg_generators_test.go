//go:build unit

package main

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertValidSVGXML(t *testing.T, svgContent string) {
	t.Helper()
	decoder := xml.NewDecoder(strings.NewReader(svgContent))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		require.NoError(t, err, "SVG content is not valid XML:\n%s", svgContent)
	}
	assert.Contains(t, svgContent, `xmlns="http://www.w3.org/2000/svg"`)
}

func TestRenderCombinedStatsSVG(t *testing.T) {
	t.Parallel()

	t.Run("should produce valid XML with multi-platform stats", func(t *testing.T) {
		// given
		stats := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{TotalCommits: 1000, TotalPRsOrMRs: 50, TotalIssuesOrWIs: 30, TotalRepos: 20, Languages: map[string]int64{"Go": 80000}, DailyContributions: map[string]int{"2026-03-25": 5}}},
			{PlatformGitLab, &PlatformStats{TotalCommits: 200, TotalPRsOrMRs: 10, TotalIssuesOrWIs: 5, TotalRepos: 5, Languages: map[string]int64{"Python": 40000}, DailyContributions: map[string]int{"2026-03-26": 3}}},
		}

		// when
		result := renderCombinedStatsSVG(stats)

		// then
		assertValidSVGXML(t, result)
		assert.Contains(t, result, `width="495"`)
	})

	t.Run("should contain platform colors in the output", func(t *testing.T) {
		// given
		stats := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{TotalCommits: 100, TotalPRsOrMRs: 10, TotalIssuesOrWIs: 5, TotalRepos: 3, Languages: map[string]int64{"Go": 4000}, DailyContributions: map[string]int{}}},
			{PlatformGitLab, &PlatformStats{TotalCommits: 50, TotalPRsOrMRs: 5, TotalIssuesOrWIs: 2, TotalRepos: 2, Languages: map[string]int64{}, DailyContributions: map[string]int{}}},
		}

		// when
		result := renderCombinedStatsSVG(stats)

		// then
		assert.Contains(t, result, PlatformGitHub.Color())
		assert.Contains(t, result, PlatformGitLab.Color())
	})

	t.Run("should produce valid XML with zero values", func(t *testing.T) {
		// given
		stats := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{TotalCommits: 0, TotalPRsOrMRs: 0, TotalIssuesOrWIs: 0, TotalRepos: 0, Languages: map[string]int64{}, DailyContributions: map[string]int{}}},
		}

		// when
		result := renderCombinedStatsSVG(stats)

		// then
		assertValidSVGXML(t, result)
	})

	t.Run("should contain stat rows for repos and lines of code", func(t *testing.T) {
		// given
		stats := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{
				TotalCommits: 100, TotalPRsOrMRs: 10, TotalIssuesOrWIs: 5,
				TotalRepos: 15,
				Languages:  map[string]int64{"Go": 80000, "Python": 40000},
				DailyContributions: map[string]int{
					"2026-03-24": 3,
					"2026-03-25": 5,
					"2026-03-26": 2,
				},
			}},
		}

		// when
		result := renderCombinedStatsSVG(stats)

		// then
		assertValidSVGXML(t, result)
		assert.Contains(t, result, "Total Repositories")
		assert.Contains(t, result, "Lines of Code")
		assert.NotContains(t, result, "Longest Streak")
	})
}

func TestRenderTokensHeatmap(t *testing.T) {
	t.Parallel()

	t.Run("should return error when tokens slice is empty", func(t *testing.T) {
		// given
		var tokens []TokenUsage

		// when
		_, err := renderTokensHeatmap(tokens)

		// then
		assert.Error(t, err)
	})

	t.Run("should produce valid XML with multiple data points", func(t *testing.T) {
		// given
		tokens := []TokenUsage{
			{Date: "2026-01-01", Tokens: 1000},
			{Date: "2026-01-02", Tokens: 2500},
			{Date: "2026-01-03", Tokens: 1800},
		}

		// when
		result, err := renderTokensHeatmap(tokens)

		// then
		require.NoError(t, err)
		assertValidSVGXML(t, result)
	})

	t.Run("should produce valid XML with a single data point", func(t *testing.T) {
		// given
		tokens := []TokenUsage{{Date: "2026-03-15", Tokens: 5000}}

		// when
		result, err := renderTokensHeatmap(tokens)

		// then
		require.NoError(t, err)
		assertValidSVGXML(t, result)
	})

	t.Run("should contain purple color scale for token intensity", func(t *testing.T) {
		// given
		tokens := []TokenUsage{
			{Date: "2026-01-01", Tokens: 100},
			{Date: "2026-01-02", Tokens: 5000},
		}

		// when
		result, err := renderTokensHeatmap(tokens)

		// then
		require.NoError(t, err)
		assert.Contains(t, result, "#8884d8")
	})

	t.Run("should contain intensity legend", func(t *testing.T) {
		// given
		tokens := []TokenUsage{{Date: "2026-01-01", Tokens: 1000}}

		// when
		result, err := renderTokensHeatmap(tokens)

		// then
		require.NoError(t, err)
		assert.Contains(t, result, "Less")
		assert.Contains(t, result, "More")
	})
}

func TestRenderLanguagesBarChart(t *testing.T) {
	t.Parallel()

	t.Run("should produce placeholder SVG when languages map is empty", func(t *testing.T) {
		// given
		languages := map[string]map[PlatformName]int64{}

		// when
		result, err := renderLanguagesBarChart(languages)

		// then
		require.NoError(t, err)
		assertValidSVGXML(t, result)
		assert.Contains(t, result, "No language data available")
	})

	t.Run("should produce placeholder SVG when all language byte counts are zero", func(t *testing.T) {
		// given
		languages := map[string]map[PlatformName]int64{
			"Go":     {PlatformGitHub: 0},
			"Python": {PlatformGitLab: 0},
		}

		// when
		result, err := renderLanguagesBarChart(languages)

		// then
		require.NoError(t, err)
		assertValidSVGXML(t, result)
		assert.Contains(t, result, "No language data available")
	})

	t.Run("should produce valid XML with platform-attributed languages", func(t *testing.T) {
		// given
		languages := map[string]map[PlatformName]int64{
			"Go":         {PlatformGitHub: 30000, PlatformGitLab: 20000},
			"Python":     {PlatformGitHub: 15000, PlatformAzureDevOps: 10000},
			"JavaScript": {PlatformGitLab: 20000},
		}

		// when
		result, err := renderLanguagesBarChart(languages)

		// then
		require.NoError(t, err)
		assertValidSVGXML(t, result)
		assert.Contains(t, result, `width="495"`)
	})

	t.Run("should show per-platform tooltip percentages for multi-platform language", func(t *testing.T) {
		// given
		languages := map[string]map[PlatformName]int64{
			"Go": {PlatformGitHub: 30000, PlatformGitLab: 20000},
		}

		// when
		result, err := renderLanguagesBarChart(languages)

		// then
		require.NoError(t, err)
		assert.Contains(t, result, "<title>GitHub: 60.0%</title>")
		assert.Contains(t, result, "<title>GitLab: 40.0%</title>")
	})

	t.Run("should show 100 percent for a single language", func(t *testing.T) {
		// given
		languages := map[string]map[PlatformName]int64{"Go": {PlatformGitHub: 100000}}

		// when
		result, err := renderLanguagesBarChart(languages)

		// then
		require.NoError(t, err)
		assert.Contains(t, result, "100.0%")
	})
}

func TestRenderContributionHeatmap(t *testing.T) {
	t.Parallel()

	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)

	t.Run("should produce valid XML with platform-attributed contributions", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{
			"2026-03-25": {PlatformGitHub: 3, PlatformGitLab: 2},
			"2026-03-26": {PlatformAzureDevOps: 10},
		}

		// when
		result := renderContributionHeatmap(contributions, startDate, endDate)

		// then
		assertValidSVGXML(t, result)
	})

	t.Run("should produce valid XML with empty contributions", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{}

		// when
		result := renderContributionHeatmap(contributions, startDate, endDate)

		// then
		assertValidSVGXML(t, result)
	})

	t.Run("should contain per-platform breakdown in tooltips", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{
			"2026-03-25": {PlatformGitHub: 3, PlatformGitLab: 4},
		}

		// when
		result := renderContributionHeatmap(contributions, startDate, endDate)

		// then
		assert.Contains(t, result, "2026-03-25: 7 (GitHub: 3, GitLab: 4)")
	})

	t.Run("should use blended color for multi-platform days", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{
			"2026-03-25": {PlatformGitHub: 3, PlatformGitLab: 4},
		}

		// when
		result := renderContributionHeatmap(contributions, startDate, endDate)

		// then
		ghGlScale := comboColorScale(comboGitHub | comboGitLab)
		containsBlend := false
		for _, c := range ghGlScale {
			if strings.Contains(result, c) {
				containsBlend = true
				break
			}
		}
		assert.True(t, containsBlend, "should contain GitHub+GitLab blended color")
	})

	t.Run("should show combo legend entries for multi-platform data", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{
			"2026-03-25": {PlatformGitHub: 3, PlatformGitLab: 4},
			"2026-03-26": {PlatformAzureDevOps: 10},
		}

		// when
		result := renderContributionHeatmap(contributions, startDate, endDate)

		// then
		assert.Contains(t, result, "GitHub + GitLab")
		assert.Contains(t, result, "Azure DevOps")
	})

	t.Run("should contain month and day labels", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{}

		// when
		result := renderContributionHeatmap(contributions, startDate, endDate)

		// then
		assert.Contains(t, result, "Jan")
		assert.Contains(t, result, "Mon")
		assert.Contains(t, result, "Wed")
		assert.Contains(t, result, "Fri")
	})
}

func TestHeatmapGrid(t *testing.T) {
	t.Parallel()

	t.Run("should standardize the width to 53 weeks when the range is shorter than a year", func(t *testing.T) {
		// given
		startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)

		// when
		grid := newHeatmapGrid(startDate, endDate)

		// then
		assert.Equal(t, 13, grid.weeks)
		assert.Equal(t, 933, grid.width)
		assert.Equal(t, 207, grid.height)
	})

	t.Run("should widen past 53 weeks when the range spans more than a year", func(t *testing.T) {
		// given
		startDate := time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC)
		endDate := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)

		// when
		grid := newHeatmapGrid(startDate, endDate)

		// then
		assert.Equal(t, 56, grid.weeks)
		assert.Equal(t, 981, grid.width)
	})

	t.Run("should keep at least one week when the end date precedes the start date", func(t *testing.T) {
		// given
		startDate := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
		endDate := startDate.AddDate(0, 0, -30)

		// when
		grid := newHeatmapGrid(startDate, endDate)

		// then
		assert.Equal(t, 1, grid.weeks)
	})

	t.Run("should label the weekday rows and every month start", func(t *testing.T) {
		// given
		grid := newHeatmapGrid(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC))

		// when
		labels := grid.renderLabels()

		// then
		assert.Contains(t, labels, `class="day-label">Mon</text>`)
		assert.Contains(t, labels, `class="day-label">Wed</text>`)
		assert.Contains(t, labels, `class="day-label">Fri</text>`)
		assert.Equal(t, 3, strings.Count(labels, "day-label"))
		assert.Contains(t, labels, `class="month-label">Jan</text>`)
		assert.Contains(t, labels, `class="month-label">Feb</text>`)
		assert.Contains(t, labels, `class="month-label">Mar</text>`)
		assert.Equal(t, 3, strings.Count(labels, "month-label"))
	})

	t.Run("should draw one cell per day and skip dates after the end date", func(t *testing.T) {
		// given
		grid := newHeatmapGrid(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC))
		var seen []string

		// when
		cells := grid.renderCells(func(dateStr string) (string, string) {
			seen = append(seen, dateStr)
			return "#123456", dateStr + " tip"
		})

		// then
		assert.Len(t, seen, 10)
		assert.Equal(t, "2026-03-01", seen[0])
		assert.Equal(t, "2026-03-10", seen[9])
		assert.Equal(t, 10, strings.Count(cells, "<rect"))
		assert.Contains(t, cells, `<rect x="60" y="55" width="13" height="13" rx="2" fill="#123456"><title>2026-03-01 tip</title></rect>`)
		assert.Contains(t, cells, `<rect x="76" y="87" width="13" height="13" rx="2" fill="#123456"><title>2026-03-10 tip</title></rect>`)
	})

	t.Run("should wrap the body in the card chrome with the given title", func(t *testing.T) {
		// given
		grid := newHeatmapGrid(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC))

		// when
		svg := grid.renderSVG("My Title", `<text>body</text>`)

		// then
		assertValidSVGXML(t, svg)
		assert.Contains(t, svg, `width="933" height="207" viewBox="0 0 933 207"`)
		assert.Contains(t, svg, `class="title">My Title</text>`)
		assert.Contains(t, svg, `<text>body</text>`)
	})
}

func TestIntensityColor(t *testing.T) {
	t.Parallel()

	t.Run("should map each quartile to the matching shade", func(t *testing.T) {
		// given
		scale := [4]string{"q1", "q2", "q3", "q4"}
		expected := map[int]string{1: "q1", 25: "q1", 26: "q2", 50: "q2", 51: "q3", 75: "q3", 76: "q4", 100: "q4"}

		// when
		actual := make(map[int]string, len(expected))
		for count := range expected {
			actual[count] = intensityColor(scale, count, 100)
		}

		// then
		assert.Equal(t, expected, actual)
	})
}

func TestComboColorScale(t *testing.T) {
	t.Parallel()

	t.Run("should return distinct scales for each combo", func(t *testing.T) {
		// given
		combos := []PlatformCombo{
			comboGitHub,
			comboGitLab,
			comboAzureDevOps,
			comboGitHub | comboGitLab,
			comboGitHub | comboAzureDevOps,
			comboGitLab | comboAzureDevOps,
			comboGitHub | comboGitLab | comboAzureDevOps,
		}

		// when
		scales := make(map[string]bool)
		for _, combo := range combos {
			scale := comboColorScale(combo)
			scales[scale[3]] = true
		}

		// then
		assert.Equal(t, 7, len(scales), "each combo should have a unique brightest color")
	})
}
