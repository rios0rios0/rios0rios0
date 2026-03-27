//go:build unit

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
