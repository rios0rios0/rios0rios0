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

func TestMergeContributions(t *testing.T) {
	t.Parallel()

	t.Run("should return empty map when no maps are provided", func(t *testing.T) {
		// given
		// no input maps

		// when
		result := mergeContributions()

		// then
		assert.Empty(t, result)
	})

	t.Run("should return the same map when a single map is provided", func(t *testing.T) {
		// given
		m := map[string]int{"2026-01-01": 5, "2026-01-02": 3}

		// when
		result := mergeContributions(m)

		// then
		assert.Equal(t, 5, result["2026-01-01"])
		assert.Equal(t, 3, result["2026-01-02"])
	})

	t.Run("should sum values for overlapping keys across multiple maps", func(t *testing.T) {
		// given
		m1 := map[string]int{"2026-01-01": 5, "2026-01-02": 3}
		m2 := map[string]int{"2026-01-01": 2, "2026-01-03": 7}
		m3 := map[string]int{"2026-01-01": 1, "2026-01-02": 4}

		// when
		result := mergeContributions(m1, m2, m3)

		// then
		assert.Equal(t, 8, result["2026-01-01"])
		assert.Equal(t, 7, result["2026-01-02"])
		assert.Equal(t, 7, result["2026-01-03"])
	})
}

func TestMergeLanguages(t *testing.T) {
	t.Parallel()

	t.Run("should return empty map when no maps are provided", func(t *testing.T) {
		// given
		// no input maps

		// when
		result := mergeLanguages()

		// then
		assert.Empty(t, result)
	})

	t.Run("should return the same map when a single map is provided", func(t *testing.T) {
		// given
		m := map[string]int64{"Go": 50000, "Python": 30000}

		// when
		result := mergeLanguages(m)

		// then
		assert.Equal(t, int64(50000), result["Go"])
		assert.Equal(t, int64(30000), result["Python"])
	})

	t.Run("should sum bytes for overlapping languages across multiple maps", func(t *testing.T) {
		// given
		m1 := map[string]int64{"Go": 50000, "Python": 30000}
		m2 := map[string]int64{"Go": 20000, "Java": 10000}
		m3 := map[string]int64{"Python": 5000, "Java": 15000}

		// when
		result := mergeLanguages(m1, m2, m3)

		// then
		assert.Equal(t, int64(70000), result["Go"])
		assert.Equal(t, int64(35000), result["Python"])
		assert.Equal(t, int64(25000), result["Java"])
	})
}
