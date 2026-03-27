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

const testCombinedStatsTemplate = `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="175" viewBox="0 0 400 175" fill="none">
<rect x="0.5" y="0.5" rx="4.5" height="99%%" width="399" fill="#151515"/>
<text data-testid="commits">%[1]d</text>
<text data-testid="prs">%[2]d</text>
<text data-testid="issues">%[3]d</text>
<text data-testid="contribs">%[4]d</text>
</svg>`

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

	t.Run("should produce valid XML with known values", func(t *testing.T) {
		// given
		commits, prs, issues, contribs := 1234, 56, 78, 1368

		// when
		result := renderCombinedStatsSVG(testCombinedStatsTemplate, commits, prs, issues, contribs)

		// then
		assertValidSVGXML(t, result)
	})

	t.Run("should contain the expected stat values in the output", func(t *testing.T) {
		// given
		commits, prs, issues, contribs := 5000, 120, 45, 5165

		// when
		result := renderCombinedStatsSVG(testCombinedStatsTemplate, commits, prs, issues, contribs)

		// then
		assert.Contains(t, result, "5,000")
		assert.Contains(t, result, "120")
		assert.Contains(t, result, "45")
		assert.Contains(t, result, "5,165")
	})

	t.Run("should render a single percent sign not double in height attribute", func(t *testing.T) {
		// given
		commits, prs, issues, contribs := 1, 2, 3, 4

		// when
		result := renderCombinedStatsSVG(testCombinedStatsTemplate, commits, prs, issues, contribs)

		// then
		assert.Contains(t, result, `height="99%"`)
		assert.NotContains(t, result, `99%%`)
	})

	t.Run("should produce valid XML with zero values", func(t *testing.T) {
		// given
		commits, prs, issues, contribs := 0, 0, 0, 0

		// when
		result := renderCombinedStatsSVG(testCombinedStatsTemplate, commits, prs, issues, contribs)

		// then
		assertValidSVGXML(t, result)
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
		languages := map[string]int64{}

		// when
		_, err := renderLanguagesBarChart(languages)

		// then
		assert.Error(t, err)
	})

	t.Run("should return error when all language byte counts are zero", func(t *testing.T) {
		// given
		languages := map[string]int64{"Go": 0, "Python": 0}

		// when
		_, err := renderLanguagesBarChart(languages)

		// then
		assert.Error(t, err)
	})

	t.Run("should produce valid XML with multiple languages", func(t *testing.T) {
		// given
		languages := map[string]int64{
			"Go":         50000,
			"Python":     30000,
			"JavaScript": 20000,
		}

		// when
		result, err := renderLanguagesBarChart(languages)

		// then
		require.NoError(t, err)
		assertValidSVGXML(t, result)
	})

	t.Run("should limit to top 10 languages when more are provided", func(t *testing.T) {
		// given
		languages := map[string]int64{
			"Go": 150000, "Python": 140000, "JavaScript": 130000,
			"Java": 120000, "TypeScript": 110000, "C": 100000,
			"C++": 90000, "Rust": 80000, "Ruby": 70000,
			"PHP": 60000, "Swift": 50000, "Kotlin": 40000,
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
		languages := map[string]int64{"Go": 100000}

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

	t.Run("should produce valid XML with contributions", func(t *testing.T) {
		// given
		contributions := map[string]int{
			"2026-03-25": 5,
			"2026-03-26": 10,
			"2026-03-20": 3,
		}

		// when
		result := renderContributionHeatmap(contributions, fixedNow)

		// then
		assertValidSVGXML(t, result)
	})

	t.Run("should produce valid XML with empty contributions", func(t *testing.T) {
		// given
		contributions := map[string]int{}

		// when
		result := renderContributionHeatmap(contributions, fixedNow)

		// then
		assertValidSVGXML(t, result)
	})

	t.Run("should contain the correct date in cell titles", func(t *testing.T) {
		// given
		contributions := map[string]int{
			"2026-03-25": 7,
		}

		// when
		result := renderContributionHeatmap(contributions, fixedNow)

		// then
		assert.Contains(t, result, "2026-03-25: 7 contributions")
	})

	t.Run("should contain month labels", func(t *testing.T) {
		// given
		contributions := map[string]int{}

		// when
		result := renderContributionHeatmap(contributions, fixedNow)

		// then
		assert.Contains(t, result, "Jan")
		assert.Contains(t, result, "Mar")
	})

	t.Run("should contain day labels", func(t *testing.T) {
		// given
		contributions := map[string]int{}

		// when
		result := renderContributionHeatmap(contributions, fixedNow)

		// then
		assert.Contains(t, result, "Mon")
		assert.Contains(t, result, "Wed")
		assert.Contains(t, result, "Fri")
	})
}
