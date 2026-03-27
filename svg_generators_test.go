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
			{PlatformGitHub, &PlatformStats{TotalCommits: 1000, TotalPRsOrMRs: 50, TotalIssuesOrWIs: 30}},
			{PlatformGitLab, &PlatformStats{TotalCommits: 200, TotalPRsOrMRs: 10, TotalIssuesOrWIs: 5}},
		}

		// when
		result := renderCombinedStatsSVG(stats)

		// then
		assertValidSVGXML(t, result)
	})

	t.Run("should contain platform colors in the output", func(t *testing.T) {
		// given
		stats := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{TotalCommits: 100, TotalPRsOrMRs: 10, TotalIssuesOrWIs: 5}},
			{PlatformGitLab, &PlatformStats{TotalCommits: 50, TotalPRsOrMRs: 5, TotalIssuesOrWIs: 2}},
		}

		// when
		result := renderCombinedStatsSVG(stats)

		// then
		assert.Contains(t, result, PlatformGitHub.Color())
		assert.Contains(t, result, PlatformGitLab.Color())
	})

	t.Run("should produce valid XML with single platform", func(t *testing.T) {
		// given
		stats := []NamedPlatformStats{
			{PlatformAzureDevOps, &PlatformStats{TotalCommits: 500, TotalPRsOrMRs: 20, TotalIssuesOrWIs: 10}},
		}

		// when
		result := renderCombinedStatsSVG(stats)

		// then
		assertValidSVGXML(t, result)
		assert.Contains(t, result, PlatformAzureDevOps.Color())
	})

	t.Run("should produce valid XML with zero values", func(t *testing.T) {
		// given
		stats := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{TotalCommits: 0, TotalPRsOrMRs: 0, TotalIssuesOrWIs: 0}},
		}

		// when
		result := renderCombinedStatsSVG(stats)

		// then
		assertValidSVGXML(t, result)
	})

	t.Run("should contain the unified card border style", func(t *testing.T) {
		// given
		stats := []NamedPlatformStats{
			{PlatformGitHub, &PlatformStats{TotalCommits: 1, TotalPRsOrMRs: 1, TotalIssuesOrWIs: 1}},
		}

		// when
		result := renderCombinedStatsSVG(stats)

		// then
		assert.Contains(t, result, `stroke-opacity="0.2"`)
		assert.Contains(t, result, `fill="#151515"`)
	})
}

func TestRenderTokensLineGraph(t *testing.T) {
	t.Parallel()

	t.Run("should return error when tokens slice is empty", func(t *testing.T) {
		// given
		var tokens []TokenUsage

		// when
		_, err := renderTokensLineGraph(tokens)

		// then
		assert.Error(t, err)
	})

	t.Run("should produce valid XML with multiple data points", func(t *testing.T) {
		// given
		tokens := []TokenUsage{
			{Date: "2026-01-01", Tokens: 1000},
			{Date: "2026-01-02", Tokens: 2500},
			{Date: "2026-01-03", Tokens: 1800},
			{Date: "2026-01-04", Tokens: 3200},
			{Date: "2026-01-05", Tokens: 2900},
		}

		// when
		result, err := renderTokensLineGraph(tokens)

		// then
		require.NoError(t, err)
		assertValidSVGXML(t, result)
	})

	t.Run("should produce valid XML with a single data point", func(t *testing.T) {
		// given
		tokens := []TokenUsage{
			{Date: "2026-03-15", Tokens: 5000},
		}

		// when
		result, err := renderTokensLineGraph(tokens)

		// then
		require.NoError(t, err)
		assertValidSVGXML(t, result)
	})

	t.Run("should contain date labels in the output", func(t *testing.T) {
		// given
		tokens := []TokenUsage{
			{Date: "2026-01-10", Tokens: 1000},
			{Date: "2026-01-15", Tokens: 2000},
			{Date: "2026-01-20", Tokens: 3000},
		}

		// when
		result, err := renderTokensLineGraph(tokens)

		// then
		require.NoError(t, err)
		assert.Contains(t, result, "01-10")
		assert.Contains(t, result, "01-20")
	})
}

func TestRenderLanguagesBarChart(t *testing.T) {
	t.Parallel()

	t.Run("should return error when languages map is empty", func(t *testing.T) {
		// given
		languages := map[string]map[PlatformName]int64{}

		// when
		_, err := renderLanguagesBarChart(languages)

		// then
		assert.Error(t, err)
	})

	t.Run("should return error when all language byte counts are zero", func(t *testing.T) {
		// given
		languages := map[string]map[PlatformName]int64{
			"Go":     {PlatformGitHub: 0},
			"Python": {PlatformGitLab: 0},
		}

		// when
		_, err := renderLanguagesBarChart(languages)

		// then
		assert.Error(t, err)
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
		assert.Contains(t, result, PlatformGitHub.Color())
		assert.Contains(t, result, PlatformGitLab.Color())
		assert.Contains(t, result, PlatformAzureDevOps.Color())
	})

	t.Run("should limit to top 10 languages when more are provided", func(t *testing.T) {
		// given
		languages := map[string]map[PlatformName]int64{
			"Go": {PlatformGitHub: 150000}, "Python": {PlatformGitHub: 140000}, "JavaScript": {PlatformGitHub: 130000},
			"Java": {PlatformGitHub: 120000}, "TypeScript": {PlatformGitHub: 110000}, "C": {PlatformGitHub: 100000},
			"C++": {PlatformGitHub: 90000}, "Rust": {PlatformGitHub: 80000}, "Ruby": {PlatformGitHub: 70000},
			"PHP": {PlatformGitHub: 60000}, "Swift": {PlatformGitHub: 50000}, "Kotlin": {PlatformGitHub: 40000},
		}

		// when
		result, err := renderLanguagesBarChart(languages)

		// then
		require.NoError(t, err)
		assertValidSVGXML(t, result)
		assert.NotContains(t, result, "Kotlin")
		assert.NotContains(t, result, "Swift")
		assert.Contains(t, result, "Go")
		assert.Contains(t, result, "PHP")
	})

	t.Run("should show 100 percent for a single language", func(t *testing.T) {
		// given
		languages := map[string]map[PlatformName]int64{
			"Go": {PlatformGitHub: 100000},
		}

		// when
		result, err := renderLanguagesBarChart(languages)

		// then
		require.NoError(t, err)
		assert.Contains(t, result, "100.0%")
	})
}

func TestRenderContributionHeatmap(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)

	t.Run("should produce valid XML with platform-attributed contributions", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{
			"2026-03-25": {PlatformGitHub: 3, PlatformGitLab: 2},
			"2026-03-26": {PlatformAzureDevOps: 10},
			"2026-03-20": {PlatformGitHub: 3},
		}

		// when
		result := renderContributionHeatmap(contributions, fixedNow)

		// then
		assertValidSVGXML(t, result)
	})

	t.Run("should produce valid XML with empty contributions", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{}

		// when
		result := renderContributionHeatmap(contributions, fixedNow)

		// then
		assertValidSVGXML(t, result)
	})

	t.Run("should contain per-platform breakdown in tooltips", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{
			"2026-03-25": {PlatformGitHub: 3, PlatformGitLab: 4},
		}

		// when
		result := renderContributionHeatmap(contributions, fixedNow)

		// then
		assert.Contains(t, result, "2026-03-25: 7 (GitHub: 3, GitLab: 4)")
	})

	t.Run("should use platform-specific colors for single-platform days", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{
			"2026-03-25": {PlatformAzureDevOps: 10},
		}

		// when
		result := renderContributionHeatmap(contributions, fixedNow)

		// then
		scale := PlatformAzureDevOps.ColorScale()
		containsAnyScale := false
		for _, c := range scale {
			if strings.Contains(result, c) {
				containsAnyScale = true
				break
			}
		}
		assert.True(t, containsAnyScale, "should contain Azure DevOps color scale")
	})

	t.Run("should contain month labels", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{}

		// when
		result := renderContributionHeatmap(contributions, fixedNow)

		// then
		assert.Contains(t, result, "Jan")
		assert.Contains(t, result, "Mar")
	})

	t.Run("should contain day labels", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{}

		// when
		result := renderContributionHeatmap(contributions, fixedNow)

		// then
		assert.Contains(t, result, "Mon")
		assert.Contains(t, result, "Wed")
		assert.Contains(t, result, "Fri")
	})

	t.Run("should contain platform legend", func(t *testing.T) {
		// given
		contributions := map[string]map[PlatformName]int{}

		// when
		result := renderContributionHeatmap(contributions, fixedNow)

		// then
		assert.Contains(t, result, "GitHub")
		assert.Contains(t, result, "GitLab")
		assert.Contains(t, result, "Azure DevOps")
	})
}
