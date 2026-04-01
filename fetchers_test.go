//go:build unit

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// roundTripFunc adapts a function to the http.RoundTripper interface,
// allowing tests to intercept all HTTP calls made by fetcher functions.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) { return 0, fmt.Errorf("read error") }

func mockClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func jsonResponse(status int, body interface{}) *http.Response {
	data, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(string(data))),
		Header:     http.Header{},
	}
}

// --- GitHub ---

func TestFetchGitHubStats(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)

	t.Run("should return stats from GraphQL response", func(t *testing.T) {
		// given
		gqlResp := map[string]interface{}{
			"data": map[string]interface{}{
				"user": map[string]interface{}{
					"contributionsCollection": map[string]interface{}{
						"totalCommitContributions":      42,
						"totalPullRequestContributions": 10,
						"totalIssueContributions":       5,
						"contributionCalendar": map[string]interface{}{
							"weeks": []interface{}{
								map[string]interface{}{
									"contributionDays": []interface{}{
										map[string]interface{}{"date": "2026-03-15", "contributionCount": 3},
										map[string]interface{}{"date": "2026-03-16", "contributionCount": 0},
									},
								},
							},
						},
						"commitContributionsByRepository": []interface{}{
							map[string]interface{}{
								"contributions": map[string]interface{}{"totalCount": 20},
								"repository": map[string]interface{}{
									"name":      "repo1",
									"isPrivate": false,
									"owner":     map[string]interface{}{"login": "testuser"},
								},
							},
						},
					},
				},
			},
		}

		callCount := 0
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			callCount++
			if req.URL.Host == "api.github.com" && req.URL.Path == "/graphql" {
				return jsonResponse(http.StatusOK, gqlResp), nil
			}
			// Language API call for repo1
			if strings.Contains(req.URL.Path, "/repos/testuser/repo1/languages") {
				return jsonResponse(http.StatusOK, map[string]int64{"Go": 50000, "Python": 20000}), nil
			}
			return jsonResponse(http.StatusNotFound, nil), nil
		})

		// when
		stats, err := FetchGitHubStats(client, "testuser", "fake-token", from, to, false)

		// then
		require.NoError(t, err)
		assert.Equal(t, 42, stats.TotalCommits)
		assert.Equal(t, 10, stats.TotalPRsOrMRs)
		assert.Equal(t, 5, stats.TotalIssuesOrWIs)
		assert.Equal(t, 1, stats.TotalRepos)
		assert.Equal(t, 3, stats.DailyContributions["2026-03-15"])
		assert.Greater(t, callCount, 0)
	})

	t.Run("should skip languages when skipLanguages is true", func(t *testing.T) {
		// given
		gqlResp := map[string]interface{}{
			"data": map[string]interface{}{
				"user": map[string]interface{}{
					"contributionsCollection": map[string]interface{}{
						"totalCommitContributions":        5,
						"totalPullRequestContributions":   1,
						"totalIssueContributions":         0,
						"contributionCalendar":            map[string]interface{}{"weeks": []interface{}{}},
						"commitContributionsByRepository": []interface{}{},
					},
				},
			},
		}

		callCount := 0
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			callCount++
			return jsonResponse(http.StatusOK, gqlResp), nil
		})

		// when
		stats, err := FetchGitHubStats(client, "testuser", "fake-token", from, to, true)

		// then
		require.NoError(t, err)
		assert.Equal(t, 5, stats.TotalCommits)
		assert.Equal(t, 1, callCount, "should only call GraphQL, not languages")
	})

	t.Run("should warn when repos count hits 100 cap", func(t *testing.T) {
		// given - build 100 repos to trigger the warning branch
		repos := make([]interface{}, 100)
		for i := range repos {
			repos[i] = map[string]interface{}{
				"contributions": map[string]interface{}{"totalCount": 1},
				"repository": map[string]interface{}{
					"name": fmt.Sprintf("repo%d", i), "isPrivate": false,
					"owner": map[string]interface{}{"login": "testuser"},
				},
			}
		}
		gqlResp := map[string]interface{}{
			"data": map[string]interface{}{
				"user": map[string]interface{}{
					"contributionsCollection": map[string]interface{}{
						"totalCommitContributions":        100,
						"totalPullRequestContributions":   0,
						"totalIssueContributions":         0,
						"contributionCalendar":            map[string]interface{}{"weeks": []interface{}{}},
						"commitContributionsByRepository": repos,
					},
				},
			},
		}

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/graphql" {
				return jsonResponse(http.StatusOK, gqlResp), nil
			}
			return jsonResponse(http.StatusOK, map[string]int64{"Go": 1000}), nil
		})

		// when
		stats, err := FetchGitHubStats(client, "testuser", "token", from, to, true)

		// then
		require.NoError(t, err)
		assert.Equal(t, 100, stats.TotalRepos)
	})

	t.Run("should handle language fetch error gracefully", func(t *testing.T) {
		// given
		gqlResp := map[string]interface{}{
			"data": map[string]interface{}{
				"user": map[string]interface{}{
					"contributionsCollection": map[string]interface{}{
						"totalCommitContributions":      5,
						"totalPullRequestContributions": 0,
						"totalIssueContributions":       0,
						"contributionCalendar":          map[string]interface{}{"weeks": []interface{}{}},
						"commitContributionsByRepository": []interface{}{
							map[string]interface{}{
								"contributions": map[string]interface{}{"totalCount": 5},
								"repository": map[string]interface{}{
									"name": "repo1", "isPrivate": false,
									"owner": map[string]interface{}{"login": "testuser"},
								},
							},
						},
					},
				},
			},
		}

		callCount := 0
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			callCount++
			if callCount == 1 {
				return jsonResponse(http.StatusOK, gqlResp), nil
			}
			return nil, fmt.Errorf("language API down")
		})

		// when
		stats, err := FetchGitHubStats(client, "testuser", "token", from, to, false)

		// then
		require.NoError(t, err)
		assert.Equal(t, 5, stats.TotalCommits)
	})

	t.Run("should return error on non-200 status", func(t *testing.T) {
		// given
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusUnauthorized, map[string]string{"message": "Bad credentials"}), nil
		})

		// when
		stats, err := FetchGitHubStats(client, "testuser", "bad-token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
		assert.Contains(t, err.Error(), "401")
	})

	t.Run("should return error on body read failure", func(t *testing.T) {
		// given
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(&errorReader{}),
				Header:     http.Header{},
			}, nil
		})

		// when
		stats, err := FetchGitHubStats(client, "testuser", "token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})

	t.Run("should return error on invalid JSON response", func(t *testing.T) {
		// given
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not json")),
				Header:     http.Header{},
			}, nil
		})

		// when
		stats, err := FetchGitHubStats(client, "testuser", "token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})

	t.Run("should exclude private repos from language fetching", func(t *testing.T) {
		// given
		gqlResp := map[string]interface{}{
			"data": map[string]interface{}{
				"user": map[string]interface{}{
					"contributionsCollection": map[string]interface{}{
						"totalCommitContributions":      10,
						"totalPullRequestContributions": 0,
						"totalIssueContributions":       0,
						"contributionCalendar":          map[string]interface{}{"weeks": []interface{}{}},
						"commitContributionsByRepository": []interface{}{
							map[string]interface{}{
								"contributions": map[string]interface{}{"totalCount": 5},
								"repository": map[string]interface{}{
									"name": "private-repo", "isPrivate": true,
									"owner": map[string]interface{}{"login": "testuser"},
								},
							},
							map[string]interface{}{
								"contributions": map[string]interface{}{"totalCount": 5},
								"repository": map[string]interface{}{
									"name": "public-repo", "isPrivate": false,
									"owner": map[string]interface{}{"login": "testuser"},
								},
							},
						},
					},
				},
			},
		}

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/graphql" {
				return jsonResponse(http.StatusOK, gqlResp), nil
			}
			return jsonResponse(http.StatusOK, map[string]int64{"Go": 5000}), nil
		})

		// when
		stats, err := FetchGitHubStats(client, "testuser", "token", from, to, false)

		// then
		require.NoError(t, err)
		assert.NotEmpty(t, stats.Languages)
	})

	t.Run("should return error on HTTP transport failure", func(t *testing.T) {
		// given
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("DNS resolution failed")
		})

		// when
		stats, err := FetchGitHubStats(client, "testuser", "token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})
}

func TestFetchGitHubLanguages(t *testing.T) {
	t.Parallel()

	t.Run("should weight languages by commit proportion", func(t *testing.T) {
		// given
		repoContribs := []repoContribution{
			{},
			{},
		}
		repoContribs[0].Contributions.TotalCount = 8
		repoContribs[0].Repository.Name = "repo1"
		repoContribs[0].Repository.Owner.Login = "owner1"
		repoContribs[1].Contributions.TotalCount = 2
		repoContribs[1].Repository.Name = "repo2"
		repoContribs[1].Repository.Owner.Login = "owner1"

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/repos/owner1/repo1/languages") {
				return jsonResponse(http.StatusOK, map[string]int64{"Go": 100000}), nil
			}
			if strings.Contains(req.URL.Path, "/repos/owner1/repo2/languages") {
				return jsonResponse(http.StatusOK, map[string]int64{"Python": 50000}), nil
			}
			return jsonResponse(http.StatusNotFound, nil), nil
		})

		stats := &PlatformStats{Languages: make(map[string]int64)}

		// when
		err := fetchGitHubLanguages(client, "testuser", "token", repoContribs, stats)

		// then
		require.NoError(t, err)
		assert.Equal(t, int64(80000), stats.Languages["Go"])
		assert.Equal(t, int64(10000), stats.Languages["Python"])
	})

	t.Run("should return error when all language requests fail", func(t *testing.T) {
		// given
		repoContribs := []repoContribution{{}}
		repoContribs[0].Contributions.TotalCount = 5
		repoContribs[0].Repository.Name = "repo1"
		repoContribs[0].Repository.Owner.Login = "owner1"

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound, nil), nil
		})
		stats := &PlatformStats{Languages: make(map[string]int64)}

		// when
		err := fetchGitHubLanguages(client, "testuser", "token", repoContribs, stats)

		// then
		require.Error(t, err)
		assert.Empty(t, stats.Languages)
	})

	t.Run("should return error on HTTP failure for all repos", func(t *testing.T) {
		// given
		repoContribs := []repoContribution{{}}
		repoContribs[0].Contributions.TotalCount = 5
		repoContribs[0].Repository.Name = "repo1"
		repoContribs[0].Repository.Owner.Login = "owner1"

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		})
		stats := &PlatformStats{Languages: make(map[string]int64)}

		// when
		err := fetchGitHubLanguages(client, "testuser", "token", repoContribs, stats)

		// then
		require.Error(t, err)
		assert.Empty(t, stats.Languages)
	})

	t.Run("should return error when body read fails for all repos", func(t *testing.T) {
		// given
		repoContribs := []repoContribution{{}}
		repoContribs[0].Contributions.TotalCount = 5
		repoContribs[0].Repository.Name = "repo1"
		repoContribs[0].Repository.Owner.Login = "owner1"

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(&errorReader{}),
				Header:     http.Header{},
			}, nil
		})
		stats := &PlatformStats{Languages: make(map[string]int64)}

		// when
		err := fetchGitHubLanguages(client, "testuser", "token", repoContribs, stats)

		// then
		require.Error(t, err)
	})

	t.Run("should return error when JSON is invalid for all repos", func(t *testing.T) {
		// given
		repoContribs := []repoContribution{{}}
		repoContribs[0].Contributions.TotalCount = 5
		repoContribs[0].Repository.Name = "repo1"
		repoContribs[0].Repository.Owner.Login = "owner1"

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not json")),
				Header:     http.Header{},
			}, nil
		})
		stats := &PlatformStats{Languages: make(map[string]int64)}

		// when
		err := fetchGitHubLanguages(client, "testuser", "token", repoContribs, stats)

		// then
		require.Error(t, err)
	})

	t.Run("should return nil when no commits", func(t *testing.T) {
		// given
		stats := &PlatformStats{Languages: make(map[string]int64)}

		// when
		err := fetchGitHubLanguages(nil, "testuser", "token", nil, stats)

		// then
		require.NoError(t, err)
		assert.Empty(t, stats.Languages)
	})
}

// --- GitLab ---

func TestFetchGitLabStats(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)

	t.Run("should return stats from events", func(t *testing.T) {
		// given
		users := []map[string]interface{}{
			{"id": 123, "name": "Test User"},
		}
		events := []map[string]interface{}{
			{"action_name": "pushed to", "target_type": "", "created_at": "2026-03-15T10:00:00Z", "push_data": map[string]interface{}{"commit_count": 3}},
			{"action_name": "opened", "target_type": "MergeRequest", "created_at": "2026-03-16T10:00:00Z", "push_data": map[string]interface{}{"commit_count": 0}},
			{"action_name": "opened", "target_type": "Issue", "created_at": "2026-03-17T10:00:00Z", "push_data": map[string]interface{}{"commit_count": 0}},
		}

		eventsCallCount := 0
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/users") && req.URL.Query().Get("username") != "" {
				return jsonResponse(http.StatusOK, users), nil
			}
			if strings.Contains(req.URL.Path, "/events") {
				eventsCallCount++
				if eventsCallCount == 1 {
					return jsonResponse(http.StatusOK, events), nil
				}
				return jsonResponse(http.StatusOK, []interface{}{}), nil
			}
			return jsonResponse(http.StatusOK, []interface{}{}), nil
		})

		// when
		stats, err := FetchGitLabStats(client, "testuser", "fake-token", from, to, true)

		// then
		require.NoError(t, err)
		assert.Equal(t, 3, stats.TotalCommits)
		assert.Equal(t, 1, stats.TotalPRsOrMRs)
		assert.Equal(t, 1, stats.TotalIssuesOrWIs)
		assert.Equal(t, 3, stats.DailyContributions["2026-03-15"])
	})

	t.Run("should fetch languages when skipLanguages is false", func(t *testing.T) {
		// given
		users := []map[string]interface{}{{"id": 123, "name": "Test User"}}
		events := []map[string]interface{}{
			{"action_name": "pushed to", "target_type": "", "created_at": "2026-03-15T10:00:00Z", "push_data": map[string]interface{}{"commit_count": 1}},
		}
		projects := []map[string]interface{}{
			{"id": 1, "last_activity_at": "2026-06-15T00:00:00Z", "statistics": map[string]interface{}{"repository_size": 100000}},
		}
		languages := map[string]float64{"Go": 100.0}

		eventsCallCount := 0
		projectsCallCount := 0
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "/users") && req.URL.Query().Get("username") != "" {
				return jsonResponse(http.StatusOK, users), nil
			}
			if strings.Contains(path, "/events") {
				eventsCallCount++
				if eventsCallCount == 1 {
					return jsonResponse(http.StatusOK, events), nil
				}
				return jsonResponse(http.StatusOK, []interface{}{}), nil
			}
			if strings.Contains(path, "/projects") && !strings.Contains(path, "/languages") {
				projectsCallCount++
				if projectsCallCount == 1 {
					return jsonResponse(http.StatusOK, projects), nil
				}
				return jsonResponse(http.StatusOK, []interface{}{}), nil
			}
			if strings.Contains(path, "/languages") {
				return jsonResponse(http.StatusOK, languages), nil
			}
			return jsonResponse(http.StatusOK, []interface{}{}), nil
		})

		// when
		stats, err := FetchGitLabStats(client, "testuser", "fake-token", from, to, false)

		// then
		require.NoError(t, err)
		assert.Equal(t, 1, stats.TotalCommits)
		assert.NotEmpty(t, stats.Languages)
	})

	t.Run("should return error when user not found", func(t *testing.T) {
		// given
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, []interface{}{}), nil
		})

		// when
		stats, err := FetchGitLabStats(client, "nonexistent", "fake-token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
		assert.Contains(t, err.Error(), "user not found")
	})

	t.Run("should return error on events HTTP failure", func(t *testing.T) {
		// given
		users := []map[string]interface{}{{"id": 123, "name": "Test User"}}

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/users") && req.URL.Query().Get("username") != "" {
				return jsonResponse(http.StatusOK, users), nil
			}
			return nil, fmt.Errorf("connection refused")
		})

		// when
		stats, err := FetchGitLabStats(client, "testuser", "fake-token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})

	t.Run("should return error on events non-200 status", func(t *testing.T) {
		// given
		users := []map[string]interface{}{{"id": 123, "name": "Test User"}}

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/users") && req.URL.Query().Get("username") != "" {
				return jsonResponse(http.StatusOK, users), nil
			}
			return jsonResponse(http.StatusInternalServerError, nil), nil
		})

		// when
		stats, err := FetchGitLabStats(client, "testuser", "fake-token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})

	t.Run("should return error on user HTTP failure", func(t *testing.T) {
		// given
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("DNS resolution failed")
		})

		// when
		stats, err := FetchGitLabStats(client, "testuser", "fake-token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})

	t.Run("should return error on events body read failure", func(t *testing.T) {
		// given
		users := []map[string]interface{}{{"id": 123, "name": "Test User"}}

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/users") && req.URL.Query().Get("username") != "" {
				return jsonResponse(http.StatusOK, users), nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(&errorReader{}),
				Header:     http.Header{},
			}, nil
		})

		// when
		stats, err := FetchGitLabStats(client, "testuser", "fake-token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})

	t.Run("should return error on events invalid JSON", func(t *testing.T) {
		// given
		users := []map[string]interface{}{{"id": 123, "name": "Test User"}}

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/users") && req.URL.Query().Get("username") != "" {
				return jsonResponse(http.StatusOK, users), nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not json")),
				Header:     http.Header{},
			}, nil
		})

		// when
		stats, err := FetchGitLabStats(client, "testuser", "fake-token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})

	t.Run("should return error on user body read failure", func(t *testing.T) {
		// given
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(&errorReader{}),
				Header:     http.Header{},
			}, nil
		})

		// when
		stats, err := FetchGitLabStats(client, "testuser", "fake-token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})

	t.Run("should return error on user invalid JSON", func(t *testing.T) {
		// given
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not json")),
				Header:     http.Header{},
			}, nil
		})

		// when
		stats, err := FetchGitLabStats(client, "testuser", "fake-token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})

	t.Run("should return error on non-200 status for user lookup", func(t *testing.T) {
		// given
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusUnauthorized, nil), nil
		})

		// when
		stats, err := FetchGitLabStats(client, "testuser", "bad-token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})
}

func TestFetchGitLabLanguages(t *testing.T) {
	t.Parallel()

	t.Run("should accumulate weighted language bytes from projects", func(t *testing.T) {
		// given
		since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		projects := []map[string]interface{}{
			{
				"id":               1,
				"last_activity_at": "2026-06-15T00:00:00Z",
				"statistics":       map[string]interface{}{"repository_size": 100000},
			},
			{
				"id":               2,
				"last_activity_at": "2020-01-01T00:00:00Z",
				"statistics":       map[string]interface{}{"repository_size": 50000},
			},
		}
		languages := map[string]float64{"Go": 60.0, "Python": 40.0}

		projectsCallCount := 0
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "/projects") && !strings.Contains(path, "/languages") {
				projectsCallCount++
				if projectsCallCount == 1 {
					return jsonResponse(http.StatusOK, projects), nil
				}
				return jsonResponse(http.StatusOK, []interface{}{}), nil
			}
			if strings.Contains(path, "/languages") {
				return jsonResponse(http.StatusOK, languages), nil
			}
			return jsonResponse(http.StatusOK, []interface{}{}), nil
		})

		stats := &PlatformStats{Languages: make(map[string]int64)}

		// when
		err := fetchGitLabLanguages(client, 123, "token", since, stats)

		// then
		require.NoError(t, err)
		assert.Equal(t, int64(60000), stats.Languages["Go"])
		assert.Equal(t, int64(40000), stats.Languages["Python"])
		assert.Equal(t, 1, stats.TotalRepos)
	})

	t.Run("should return error on HTTP failure", func(t *testing.T) {
		// given
		since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		})
		stats := &PlatformStats{Languages: make(map[string]int64)}

		// when
		err := fetchGitLabLanguages(client, 123, "token", since, stats)

		// then
		require.Error(t, err)
	})

	t.Run("should use percentage fallback when repo size is zero", func(t *testing.T) {
		// given
		since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		projects := []map[string]interface{}{
			{
				"id":               1,
				"last_activity_at": "2026-06-15T00:00:00Z",
				"statistics":       map[string]interface{}{"repository_size": 0},
			},
		}
		languages := map[string]float64{"Go": 60.0, "Python": 40.0}

		projectsCallCount := 0
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "/projects") && !strings.Contains(path, "/languages") {
				projectsCallCount++
				if projectsCallCount == 1 {
					return jsonResponse(http.StatusOK, projects), nil
				}
				return jsonResponse(http.StatusOK, []interface{}{}), nil
			}
			if strings.Contains(path, "/languages") {
				return jsonResponse(http.StatusOK, languages), nil
			}
			return jsonResponse(http.StatusOK, []interface{}{}), nil
		})
		stats := &PlatformStats{Languages: make(map[string]int64)}

		// when
		err := fetchGitLabLanguages(client, 123, "token", since, stats)

		// then
		require.NoError(t, err)
		assert.Equal(t, int64(6000), stats.Languages["Go"])
		assert.Equal(t, int64(4000), stats.Languages["Python"])
	})

	t.Run("should handle language fetch HTTP error gracefully", func(t *testing.T) {
		// given
		since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		projects := []map[string]interface{}{
			{
				"id":               1,
				"last_activity_at": "2026-06-15T00:00:00Z",
				"statistics":       map[string]interface{}{"repository_size": 100000},
			},
		}

		projectsCallCount := 0
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "/projects") && !strings.Contains(path, "/languages") {
				projectsCallCount++
				if projectsCallCount == 1 {
					return jsonResponse(http.StatusOK, projects), nil
				}
				return jsonResponse(http.StatusOK, []interface{}{}), nil
			}
			if strings.Contains(path, "/languages") {
				return nil, fmt.Errorf("connection refused")
			}
			return jsonResponse(http.StatusOK, []interface{}{}), nil
		})
		stats := &PlatformStats{Languages: make(map[string]int64)}

		// when
		err := fetchGitLabLanguages(client, 123, "token", since, stats)

		// then
		require.NoError(t, err)
		assert.Empty(t, stats.Languages)
		assert.Equal(t, 1, stats.TotalRepos)
	})

	t.Run("should handle invalid language JSON gracefully", func(t *testing.T) {
		// given
		since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		projects := []map[string]interface{}{
			{
				"id":               1,
				"last_activity_at": "2026-06-15T00:00:00Z",
				"statistics":       map[string]interface{}{"repository_size": 100000},
			},
		}

		projectsCallCount := 0
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "/projects") && !strings.Contains(path, "/languages") {
				projectsCallCount++
				if projectsCallCount == 1 {
					return jsonResponse(http.StatusOK, projects), nil
				}
				return jsonResponse(http.StatusOK, []interface{}{}), nil
			}
			if strings.Contains(path, "/languages") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("not json")),
					Header:     http.Header{},
				}, nil
			}
			return jsonResponse(http.StatusOK, []interface{}{}), nil
		})
		stats := &PlatformStats{Languages: make(map[string]int64)}

		// when
		err := fetchGitLabLanguages(client, 123, "token", since, stats)

		// then
		require.NoError(t, err)
		assert.Empty(t, stats.Languages)
	})

	t.Run("should stop when all projects are too old", func(t *testing.T) {
		// given
		since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		projects := []map[string]interface{}{
			{
				"id":               1,
				"last_activity_at": "2020-01-01T00:00:00Z",
				"statistics":       map[string]interface{}{"repository_size": 50000},
			},
		}
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, projects), nil
		})
		stats := &PlatformStats{Languages: make(map[string]int64)}

		// when
		err := fetchGitLabLanguages(client, 123, "token", since, stats)

		// then
		require.NoError(t, err)
		assert.Empty(t, stats.Languages)
	})
}

// --- Azure DevOps ---

func TestFetchAzureDevOpsStats(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)

	t.Run("should return stats from API responses", func(t *testing.T) {
		// given
		connData := map[string]interface{}{
			"authenticatedUser": map[string]interface{}{
				"id":                  "user-id-123",
				"providerDisplayName": "Test User",
			},
		}
		projects := map[string]interface{}{
			"count": 1,
			"value": []map[string]interface{}{
				{"id": "proj-1", "name": "TestProject"},
			},
		}
		repos := map[string]interface{}{
			"value": []map[string]interface{}{
				{"id": "repo-1"},
			},
		}
		commits := map[string]interface{}{
			"count": 2,
			"value": []map[string]interface{}{
				{"author": map[string]interface{}{"date": "2026-03-15T10:00:00Z"}},
				{"author": map[string]interface{}{"date": "2026-03-16T10:00:00Z"}},
			},
		}
		emptyCommits := map[string]interface{}{
			"count": 0,
			"value": []interface{}{},
		}
		prs := map[string]interface{}{
			"count": 1,
			"value": []map[string]interface{}{
				{"creationDate": "2026-06-01T12:00:00Z"},
			},
		}
		workItems := map[string]interface{}{
			"workItems": []map[string]interface{}{
				{"id": 1},
				{"id": 2},
				{"id": 3},
			},
		}

		commitCallCount := 0
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "connectionData") {
				return jsonResponse(http.StatusOK, connData), nil
			}
			if strings.Contains(path, "_apis/projects") {
				resp := jsonResponse(http.StatusOK, projects)
				return resp, nil
			}
			if strings.Contains(path, "/repositories") && !strings.Contains(path, "/commits") {
				return jsonResponse(http.StatusOK, repos), nil
			}
			if strings.Contains(path, "/commits") {
				commitCallCount++
				if commitCallCount == 1 {
					return jsonResponse(http.StatusOK, commits), nil
				}
				return jsonResponse(http.StatusOK, emptyCommits), nil
			}
			if strings.Contains(path, "/pullrequests") {
				return jsonResponse(http.StatusOK, prs), nil
			}
			if strings.Contains(path, "/wiql") {
				return jsonResponse(http.StatusOK, workItems), nil
			}
			return jsonResponse(http.StatusOK, map[string]interface{}{}), nil
		})

		// when
		stats, err := FetchAzureDevOpsStats(client, "myorg", "fake-token", from, to, true)

		// then
		require.NoError(t, err)
		assert.Equal(t, 2, stats.TotalCommits)
		assert.Equal(t, 1, stats.TotalPRsOrMRs)
		assert.Equal(t, 3, stats.TotalIssuesOrWIs)
		assert.Equal(t, 1, stats.TotalRepos)
		assert.Equal(t, 1, stats.DailyContributions["2026-03-15"])
		assert.Equal(t, 1, stats.DailyContributions["2026-03-16"])
	})

	t.Run("should exercise full repo/commit/PR flow with languages", func(t *testing.T) {
		// given
		connData := map[string]interface{}{
			"authenticatedUser": map[string]interface{}{
				"id":                  "user-id-123",
				"providerDisplayName": "Test User",
			},
		}
		projects := map[string]interface{}{
			"count": 1,
			"value": []map[string]interface{}{
				{"id": "proj-1", "name": "TestProject"},
			},
		}
		repos := map[string]interface{}{
			"value": []map[string]interface{}{
				{"id": "repo-1"},
			},
		}
		commits := map[string]interface{}{
			"count": 1,
			"value": []map[string]interface{}{
				{"author": map[string]interface{}{"date": "2026-05-10T10:00:00Z"}},
			},
		}
		prs := map[string]interface{}{
			"count": 2,
			"value": []map[string]interface{}{
				{"creationDate": "2026-06-01T12:00:00Z"},
				{"creationDate": "2026-07-01T12:00:00Z"},
			},
		}
		workItems := map[string]interface{}{
			"workItems": []map[string]interface{}{{"id": 1}},
		}
		// Mock for fetchAzureDevOpsLanguages
		repoMeta := map[string]string{"defaultBranch": "refs/heads/main"}
		allCommits := map[string]interface{}{"count": 1}
		latestCommit := map[string]interface{}{
			"value": []map[string]string{{"commitId": "sha123"}},
		}
		commitDetail := map[string]string{"treeId": "abc123"}
		tree := map[string]interface{}{
			"truncated": false,
			"treeEntries": []map[string]interface{}{
				{"relativePath": "main.go", "gitObjectType": "blob", "size": 5000},
			},
		}

		commitCallCount := 0
		langPhase := false
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			query := req.URL.RawQuery
			if strings.Contains(path, "connectionData") {
				return jsonResponse(http.StatusOK, connData), nil
			}
			if strings.Contains(path, "_apis/projects") {
				resp := jsonResponse(http.StatusOK, projects)
				return resp, nil
			}
			if strings.Contains(path, "/repositories") && !strings.Contains(path, "/commits") && !strings.Contains(path, "/trees") {
				return jsonResponse(http.StatusOK, repos), nil
			}
			if strings.Contains(path, "/commits") && !langPhase {
				commitCallCount++
				if commitCallCount <= 1 {
					return jsonResponse(http.StatusOK, commits), nil
				}
				return jsonResponse(http.StatusOK, map[string]interface{}{"count": 0, "value": []interface{}{}}), nil
			}
			if strings.Contains(path, "/pullrequests") {
				langPhase = true
				return jsonResponse(http.StatusOK, prs), nil
			}
			if strings.Contains(path, "/wiql") {
				return jsonResponse(http.StatusOK, workItems), nil
			}
			// Language fetching phase (fetchAzureDevOpsLanguages)
			// Note: /commits/{id} (commit detail) must be checked before /commits (commit list)
			if strings.Contains(path, "/trees/") {
				return jsonResponse(http.StatusOK, tree), nil
			}
			if strings.Contains(query, "defaultBranch") || (strings.Contains(path, "/repositories/") && !strings.Contains(path, "/commits") && !strings.Contains(path, "/trees")) {
				return jsonResponse(http.StatusOK, repoMeta), nil
			}
			if langPhase && strings.Contains(path, "/commits/") && !strings.Contains(query, "$top") {
				return jsonResponse(http.StatusOK, commitDetail), nil
			}
			if langPhase && strings.Contains(path, "/commits") && strings.Contains(query, "$top=1") {
				return jsonResponse(http.StatusOK, latestCommit), nil
			}
			if langPhase && strings.Contains(path, "/commits") {
				return jsonResponse(http.StatusOK, allCommits), nil
			}
			return jsonResponse(http.StatusOK, map[string]interface{}{}), nil
		})

		// when
		stats, err := FetchAzureDevOpsStats(client, "myorg", "fake-token", from, to, false)

		// then
		require.NoError(t, err)
		assert.Equal(t, 1, stats.TotalCommits)
		assert.Equal(t, 2, stats.TotalPRsOrMRs)
		assert.Equal(t, 1, stats.TotalIssuesOrWIs)
		assert.Equal(t, 1, stats.TotalRepos)
	})

	t.Run("should handle project with no repos gracefully", func(t *testing.T) {
		// given
		connData := map[string]interface{}{
			"authenticatedUser": map[string]interface{}{
				"id":                  "user-id",
				"providerDisplayName": "Test User",
			},
		}
		projects := map[string]interface{}{
			"count": 1,
			"value": []map[string]interface{}{{"id": "proj-1", "name": "P1"}},
		}
		emptyRepos := map[string]interface{}{"value": []interface{}{}}
		workItems := map[string]interface{}{"workItems": []interface{}{}}

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "connectionData") {
				return jsonResponse(http.StatusOK, connData), nil
			}
			if strings.Contains(path, "_apis/projects") {
				return jsonResponse(http.StatusOK, projects), nil
			}
			if strings.Contains(path, "/repositories") {
				return jsonResponse(http.StatusOK, emptyRepos), nil
			}
			if strings.Contains(path, "/pullrequests") {
				return jsonResponse(http.StatusOK, map[string]interface{}{"count": 0, "value": []interface{}{}}), nil
			}
			if strings.Contains(path, "/wiql") {
				return jsonResponse(http.StatusOK, workItems), nil
			}
			return jsonResponse(http.StatusOK, map[string]interface{}{}), nil
		})

		// when
		stats, err := FetchAzureDevOpsStats(client, "myorg", "fake-token", from, to, true)

		// then
		require.NoError(t, err)
		assert.Equal(t, 0, stats.TotalRepos)
	})

	t.Run("should filter PRs by date range", func(t *testing.T) {
		// given
		connData := map[string]interface{}{
			"authenticatedUser": map[string]interface{}{
				"id":                  "user-id",
				"providerDisplayName": "Test User",
			},
		}
		projects := map[string]interface{}{
			"count": 1,
			"value": []map[string]interface{}{{"id": "proj-1", "name": "P1"}},
		}
		repos := map[string]interface{}{
			"value": []map[string]interface{}{{"id": "repo-1"}},
		}
		emptyCommits := map[string]interface{}{"count": 0, "value": []interface{}{}}
		prs := map[string]interface{}{
			"count": 3,
			"value": []map[string]interface{}{
				{"creationDate": "2026-06-01T12:00:00Z"},
				{"creationDate": "2025-01-01T12:00:00Z"},
				{"creationDate": "invalid-date"},
			},
		}
		workItems := map[string]interface{}{"workItems": []interface{}{}}

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "connectionData") {
				return jsonResponse(http.StatusOK, connData), nil
			}
			if strings.Contains(path, "_apis/projects") {
				return jsonResponse(http.StatusOK, projects), nil
			}
			if strings.Contains(path, "/repositories") && !strings.Contains(path, "/commits") {
				return jsonResponse(http.StatusOK, repos), nil
			}
			if strings.Contains(path, "/commits") {
				return jsonResponse(http.StatusOK, emptyCommits), nil
			}
			if strings.Contains(path, "/pullrequests") {
				return jsonResponse(http.StatusOK, prs), nil
			}
			if strings.Contains(path, "/wiql") {
				return jsonResponse(http.StatusOK, workItems), nil
			}
			return jsonResponse(http.StatusOK, map[string]interface{}{}), nil
		})

		// when
		stats, err := FetchAzureDevOpsStats(client, "myorg", "fake-token", from, to, true)

		// then
		require.NoError(t, err)
		assert.Equal(t, 1, stats.TotalPRsOrMRs)
	})

	t.Run("should handle repos API error gracefully", func(t *testing.T) {
		// given
		connData := map[string]interface{}{
			"authenticatedUser": map[string]interface{}{
				"id":                  "user-id",
				"providerDisplayName": "Test User",
			},
		}
		projects := map[string]interface{}{
			"count": 1,
			"value": []map[string]interface{}{{"id": "proj-1", "name": "P1"}},
		}
		workItems := map[string]interface{}{"workItems": []interface{}{}}

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "connectionData") {
				return jsonResponse(http.StatusOK, connData), nil
			}
			if strings.Contains(path, "_apis/projects") {
				return jsonResponse(http.StatusOK, projects), nil
			}
			if strings.Contains(path, "/repositories") {
				return jsonResponse(http.StatusInternalServerError, nil), nil
			}
			if strings.Contains(path, "/pullrequests") {
				return jsonResponse(http.StatusOK, map[string]interface{}{"count": 0, "value": []interface{}{}}), nil
			}
			if strings.Contains(path, "/wiql") {
				return jsonResponse(http.StatusOK, workItems), nil
			}
			return jsonResponse(http.StatusOK, map[string]interface{}{}), nil
		})

		// when
		stats, err := FetchAzureDevOpsStats(client, "myorg", "fake-token", from, to, true)

		// then
		require.NoError(t, err)
		assert.Equal(t, 0, stats.TotalCommits)
	})

	t.Run("should handle commits API failure gracefully", func(t *testing.T) {
		// given
		connData := map[string]interface{}{
			"authenticatedUser": map[string]interface{}{
				"id": "uid", "providerDisplayName": "User",
			},
		}
		projects := map[string]interface{}{
			"count": 1,
			"value": []map[string]interface{}{{"id": "p1", "name": "P"}},
		}
		repos := map[string]interface{}{
			"value": []map[string]interface{}{{"id": "r1"}},
		}
		workItems := map[string]interface{}{"workItems": []interface{}{}}

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "connectionData") {
				return jsonResponse(http.StatusOK, connData), nil
			}
			if strings.Contains(path, "_apis/projects") {
				return jsonResponse(http.StatusOK, projects), nil
			}
			if strings.Contains(path, "/repositories") && !strings.Contains(path, "/commits") {
				return jsonResponse(http.StatusOK, repos), nil
			}
			if strings.Contains(path, "/commits") {
				return jsonResponse(http.StatusInternalServerError, nil), nil
			}
			if strings.Contains(path, "/pullrequests") {
				return jsonResponse(http.StatusOK, map[string]interface{}{"count": 0, "value": []interface{}{}}), nil
			}
			if strings.Contains(path, "/wiql") {
				return jsonResponse(http.StatusOK, workItems), nil
			}
			return jsonResponse(http.StatusOK, map[string]interface{}{}), nil
		})

		// when
		stats, err := FetchAzureDevOpsStats(client, "myorg", "token", from, to, true)

		// then
		require.NoError(t, err)
		assert.Equal(t, 0, stats.TotalCommits)
	})

	t.Run("should handle PRs API failure gracefully", func(t *testing.T) {
		// given
		connData := map[string]interface{}{
			"authenticatedUser": map[string]interface{}{
				"id": "uid", "providerDisplayName": "User",
			},
		}
		projects := map[string]interface{}{
			"count": 1,
			"value": []map[string]interface{}{{"id": "p1", "name": "P"}},
		}
		repos := map[string]interface{}{
			"value": []map[string]interface{}{{"id": "r1"}},
		}
		emptyCommits := map[string]interface{}{"count": 0, "value": []interface{}{}}
		workItems := map[string]interface{}{"workItems": []interface{}{}}

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "connectionData") {
				return jsonResponse(http.StatusOK, connData), nil
			}
			if strings.Contains(path, "_apis/projects") {
				return jsonResponse(http.StatusOK, projects), nil
			}
			if strings.Contains(path, "/repositories") && !strings.Contains(path, "/commits") {
				return jsonResponse(http.StatusOK, repos), nil
			}
			if strings.Contains(path, "/commits") {
				return jsonResponse(http.StatusOK, emptyCommits), nil
			}
			if strings.Contains(path, "/pullrequests") {
				return jsonResponse(http.StatusInternalServerError, nil), nil
			}
			if strings.Contains(path, "/wiql") {
				return jsonResponse(http.StatusOK, workItems), nil
			}
			return jsonResponse(http.StatusOK, map[string]interface{}{}), nil
		})

		// when
		stats, err := FetchAzureDevOpsStats(client, "myorg", "token", from, to, true)

		// then
		require.NoError(t, err)
		assert.Equal(t, 0, stats.TotalPRsOrMRs)
	})

	t.Run("should handle WIQL failure gracefully", func(t *testing.T) {
		// given
		connData := map[string]interface{}{
			"authenticatedUser": map[string]interface{}{
				"id": "uid", "providerDisplayName": "User",
			},
		}
		projects := map[string]interface{}{
			"count": 0, "value": []interface{}{},
		}

		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "connectionData") {
				return jsonResponse(http.StatusOK, connData), nil
			}
			if strings.Contains(path, "_apis/projects") {
				return jsonResponse(http.StatusOK, projects), nil
			}
			if strings.Contains(path, "/wiql") {
				return jsonResponse(http.StatusInternalServerError, nil), nil
			}
			return jsonResponse(http.StatusOK, map[string]interface{}{}), nil
		})

		// when
		stats, err := FetchAzureDevOpsStats(client, "myorg", "token", from, to, true)

		// then
		require.NoError(t, err)
		assert.Equal(t, 0, stats.TotalIssuesOrWIs)
	})

	t.Run("should return error on connection data failure", func(t *testing.T) {
		// given
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusUnauthorized, nil), nil
		})

		// when
		stats, err := FetchAzureDevOpsStats(client, "myorg", "bad-token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})

	t.Run("should return error on network failure", func(t *testing.T) {
		// given
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		})

		// when
		stats, err := FetchAzureDevOpsStats(client, "myorg", "token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})

	t.Run("should return error on connection data body parse failure", func(t *testing.T) {
		// given
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not json")),
				Header:     http.Header{},
			}, nil
		})

		// when
		stats, err := FetchAzureDevOpsStats(client, "myorg", "token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})

	t.Run("should return error on projects API failure", func(t *testing.T) {
		// given
		connData := map[string]interface{}{
			"authenticatedUser": map[string]interface{}{
				"id": "uid", "providerDisplayName": "User",
			},
		}
		client := mockClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			if strings.Contains(path, "connectionData") {
				return jsonResponse(http.StatusOK, connData), nil
			}
			return jsonResponse(http.StatusInternalServerError, nil), nil
		})

		// when
		stats, err := FetchAzureDevOpsStats(client, "myorg", "token", from, to, true)

		// then
		require.Error(t, err)
		assert.Nil(t, stats)
	})
}
