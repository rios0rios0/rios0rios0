package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"
)

// PlatformStats holds stats from a single platform
type PlatformStats struct {
	TotalCommits       int
	TotalPRsOrMRs      int
	TotalIssuesOrWIs   int
	TotalRepos         int
	Languages          map[string]int64 // language -> bytes
	DailyContributions map[string]int   // "2025-03-20" -> count
}

// TokenUsage represents daily Claude Code token usage
type TokenUsage struct {
	Date   string `json:"date"`
	Tokens int    `json:"tokens"`
}

// PlatformName identifies a development platform
type PlatformName string

const (
	PlatformGitHub      PlatformName = "GitHub"
	PlatformGitLab      PlatformName = "GitLab"
	PlatformAzureDevOps PlatformName = "Azure DevOps"
)

var platformOrder = []PlatformName{PlatformGitHub, PlatformGitLab, PlatformAzureDevOps}

func (p PlatformName) Color() string {
	switch p {
	case PlatformGitHub:
		return "#238636"
	case PlatformGitLab:
		return "#e24329"
	case PlatformAzureDevOps:
		return "#0078d4"
	default:
		return "#8b949e"
	}
}

func (p PlatformName) ColorScale() [4]string {
	switch p {
	case PlatformGitHub:
		return [4]string{"#0e4429", "#006d32", "#1a7f37", "#238636"}
	case PlatformGitLab:
		return [4]string{"#4d1a10", "#b03820", "#d63e2a", "#e24329"}
	case PlatformAzureDevOps:
		return [4]string{"#0a2d4d", "#0053a0", "#0066c0", "#0078d4"}
	default:
		return [4]string{"#2a2f35", "#5a6068", "#6f777f", "#8b949e"}
	}
}

// Platform combination bitmask for heatmap color blending
type PlatformCombo uint8

const (
	comboGitHub      PlatformCombo = 1 << 0
	comboGitLab      PlatformCombo = 1 << 1
	comboAzureDevOps PlatformCombo = 1 << 2
)

func platformToCombo(p PlatformName) PlatformCombo {
	switch p {
	case PlatformGitHub:
		return comboGitHub
	case PlatformGitLab:
		return comboGitLab
	case PlatformAzureDevOps:
		return comboAzureDevOps
	default:
		return 0
	}
}

var comboColorScales = map[PlatformCombo][4]string{
	comboGitHub:                        {"#0e4429", "#006d32", "#1a7f37", "#238636"},
	comboGitLab:                        {"#4d1a10", "#b03820", "#d63e2a", "#e24329"},
	comboAzureDevOps:                   {"#0a2d4d", "#0053a0", "#0066c0", "#0078d4"},
	comboGitHub | comboGitLab:          {"#2a2a10", "#5a5520", "#7a7530", "#a09540"},
	comboGitHub | comboAzureDevOps:     {"#0a3040", "#106a60", "#1a9070", "#24b880"},
	comboGitLab | comboAzureDevOps:     {"#2a1050", "#5a2090", "#7030b0", "#8840d0"},
	comboGitHub | comboGitLab | comboAzureDevOps: {"#2a3018", "#5a6830", "#7a9040", "#a0c050"},
}

var comboLabels = map[PlatformCombo]string{
	comboGitHub:                        "GitHub",
	comboGitLab:                        "GitLab",
	comboAzureDevOps:                   "Azure DevOps",
	comboGitHub | comboGitLab:          "GitHub + GitLab",
	comboGitHub | comboAzureDevOps:     "GitHub + Azure DevOps",
	comboGitLab | comboAzureDevOps:     "GitLab + Azure DevOps",
	comboGitHub | comboGitLab | comboAzureDevOps: "All Platforms",
}

func comboColorScale(combo PlatformCombo) [4]string {
	if scale, ok := comboColorScales[combo]; ok {
		return scale
	}
	return [4]string{"#2a2f35", "#5a6068", "#6f777f", "#8b949e"}
}

// NamedPlatformStats pairs a PlatformStats with its platform identity
type NamedPlatformStats struct {
	Platform PlatformName
	Stats    *PlatformStats
}

// --- History ---

type PlatformSnapshot struct {
	TotalCommits       int              `json:"total_commits"`
	TotalPRsOrMRs      int              `json:"total_prs_or_mrs"`
	TotalIssuesOrWIs   int              `json:"total_issues_or_wis"`
	TotalRepos         int              `json:"total_repos"`
	Languages          map[string]int64 `json:"languages"`
	DailyContributions map[string]int   `json:"daily_contributions"`
}

type DailySnapshot struct {
	Date      string                          `json:"date"`
	Platforms map[PlatformName]PlatformSnapshot `json:"platforms"`
}

type StatsHistory struct {
	Version   int             `json:"version"`
	Snapshots []DailySnapshot `json:"snapshots"`
}

func loadStatsHistory(path string) (*StatsHistory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &StatsHistory{Version: 1}, nil
		}
		return nil, err
	}
	var history StatsHistory
	if err = json.Unmarshal(data, &history); err != nil {
		return nil, err
	}
	return &history, nil
}

func saveStatsHistory(history *StatsHistory, path string) error {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func addSnapshot(history *StatsHistory, date string, platforms []NamedPlatformStats) {
	snap := DailySnapshot{
		Date:      date,
		Platforms: make(map[PlatformName]PlatformSnapshot),
	}
	for _, ns := range platforms {
		snap.Platforms[ns.Platform] = PlatformSnapshot{
			TotalCommits:       ns.Stats.TotalCommits,
			TotalPRsOrMRs:      ns.Stats.TotalPRsOrMRs,
			TotalIssuesOrWIs:   ns.Stats.TotalIssuesOrWIs,
			TotalRepos:         ns.Stats.TotalRepos,
			Languages:          ns.Stats.Languages,
			DailyContributions: ns.Stats.DailyContributions,
		}
	}
	// Replace existing snapshot for this date or append
	for i, s := range history.Snapshots {
		if s.Date == date {
			history.Snapshots[i] = snap
			return
		}
	}
	history.Snapshots = append(history.Snapshots, snap)
}

func removeSnapshotsForYear(history *StatsHistory, year int) {
	prefix := fmt.Sprintf("%d-", year)
	filtered := history.Snapshots[:0]
	for _, s := range history.Snapshots {
		if !strings.HasPrefix(s.Date, prefix) {
			filtered = append(filtered, s)
		}
	}
	history.Snapshots = filtered
}

func accumulateByYear(history *StatsHistory) map[int][]NamedPlatformStats {
	type accumEntry struct {
		maxCommits    int
		maxPRs        int
		maxIssues     int
		maxRepos      int
		contributions map[string]int   // date -> max count
		maxLangs      map[string]int64 // language -> max bytes across all snapshots
	}

	yearPlatform := make(map[int]map[PlatformName]*accumEntry)

	for _, snap := range history.Snapshots {
		snapYear := 0
		if len(snap.Date) >= 4 {
			fmt.Sscanf(snap.Date, "%d", &snapYear)
		}

		for platform, ps := range snap.Platforms {
			if snapYear > 0 {
				if yearPlatform[snapYear] == nil {
					yearPlatform[snapYear] = make(map[PlatformName]*accumEntry)
				}
				entry := yearPlatform[snapYear][platform]
				if entry == nil {
					entry = &accumEntry{
						contributions: make(map[string]int),
						maxLangs:      make(map[string]int64),
					}
					yearPlatform[snapYear][platform] = entry
				}
				if ps.TotalCommits > entry.maxCommits {
					entry.maxCommits = ps.TotalCommits
				}
				if ps.TotalPRsOrMRs > entry.maxPRs {
					entry.maxPRs = ps.TotalPRsOrMRs
				}
				if ps.TotalIssuesOrWIs > entry.maxIssues {
					entry.maxIssues = ps.TotalIssuesOrWIs
				}
				if ps.TotalRepos > entry.maxRepos {
					entry.maxRepos = ps.TotalRepos
				}
				// Track max language bytes across all snapshots
				for lang, bytes := range ps.Languages {
					if bytes > entry.maxLangs[lang] {
						entry.maxLangs[lang] = bytes
					}
				}
			}

			// Distribute daily contributions to their respective years
			for date, count := range ps.DailyContributions {
				contribYear := 0
				if len(date) >= 4 {
					fmt.Sscanf(date, "%d", &contribYear)
				}
				if contribYear == 0 {
					continue
				}
				if yearPlatform[contribYear] == nil {
					yearPlatform[contribYear] = make(map[PlatformName]*accumEntry)
				}
				entry := yearPlatform[contribYear][platform]
				if entry == nil {
					entry = &accumEntry{
						contributions: make(map[string]int),
						maxLangs:      make(map[string]int64),
					}
					yearPlatform[contribYear][platform] = entry
				}
				if count > entry.contributions[date] {
					entry.contributions[date] = count
				}
			}
		}
	}

	// Convert to []NamedPlatformStats per year
	result := make(map[int][]NamedPlatformStats)
	for year, platforms := range yearPlatform {
		for _, p := range platformOrder {
			entry := platforms[p]
			if entry == nil {
				continue
			}

			// Use max language bytes across all snapshots in this year
			logger.WithFields(logger.Fields{
				"year":      year,
				"platform":  string(p),
				"languages": len(entry.maxLangs),
			}).Debug("language accumulation result")

			result[year] = append(result[year], NamedPlatformStats{
				Platform: p,
				Stats: &PlatformStats{
					TotalCommits:       entry.maxCommits,
					TotalPRsOrMRs:      entry.maxPRs,
					TotalIssuesOrWIs:   entry.maxIssues,
					TotalRepos:         entry.maxRepos,
					Languages:          entry.maxLangs,
					DailyContributions: entry.contributions,
				},
			})
		}
	}
	return result
}

// --- GitHub ---

func FetchGitHubStats(username, token string, from, to time.Time, skipLanguages bool) (*PlatformStats, error) {
	start := time.Now()
	defer func() {
		logger.WithFields(logger.Fields{
			"platform": "GitHub",
			"elapsed":  time.Since(start).String(),
		}).Debug("platform fetch completed")
	}()

	stats := &PlatformStats{
		Languages:          make(map[string]int64),
		DailyContributions: make(map[string]int),
	}

	// Use GraphQL API for contributions + repos committed to
	logger.WithFields(logger.Fields{
		"platform": "GitHub",
		"endpoint": "https://api.github.com/graphql",
		"method":   "POST",
		"from":     from.Format(time.RFC3339),
		"to":       to.Format(time.RFC3339),
	}).Debug("calling GitHub GraphQL API for contributions")
	query := fmt.Sprintf(`{
		"query": "query { user(login: \"%s\") { contributionsCollection(from: \"%s\", to: \"%s\") { totalCommitContributions totalPullRequestContributions totalIssueContributions contributionCalendar { weeks { contributionDays { date contributionCount } } } commitContributionsByRepository(maxRepositories: 100) { contributions { totalCount } repository { name owner { login } isPrivate } } } repositories(first: 100, ownerAffiliations: OWNER, isFork: false, privacy: PUBLIC) { totalCount } } }"
	}`, username, from.Format(time.RFC3339), to.Format(time.RFC3339))

	req, err := http.NewRequest("POST", "https://api.github.com/graphql", strings.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub GraphQL error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var gqlResp struct {
		Data struct {
			User struct {
				ContributionsCollection struct {
					TotalCommitContributions      int `json:"totalCommitContributions"`
					TotalPullRequestContributions int `json:"totalPullRequestContributions"`
					TotalIssueContributions       int `json:"totalIssueContributions"`
					ContributionCalendar          struct {
						Weeks []struct {
							ContributionDays []struct {
								Date              string `json:"date"`
								ContributionCount int    `json:"contributionCount"`
							} `json:"contributionDays"`
						} `json:"weeks"`
					} `json:"contributionCalendar"`
					CommitContributionsByRepository []struct {
						Contributions struct {
							TotalCount int `json:"totalCount"`
						} `json:"contributions"`
						Repository struct {
							Name      string `json:"name"`
							IsPrivate bool   `json:"isPrivate"`
							Owner     struct {
								Login string `json:"login"`
							} `json:"owner"`
						} `json:"repository"`
					} `json:"commitContributionsByRepository"`
				} `json:"contributionsCollection"`
				Repositories struct {
					TotalCount int `json:"totalCount"`
				} `json:"repositories"`
			} `json:"user"`
		} `json:"data"`
	}

	if err = json.Unmarshal(body, &gqlResp); err != nil {
		return nil, fmt.Errorf("GitHub GraphQL parse error: %w, body: %s", err, string(body))
	}

	cc := gqlResp.Data.User.ContributionsCollection
	stats.TotalCommits = cc.TotalCommitContributions
	stats.TotalPRsOrMRs = cc.TotalPullRequestContributions
	stats.TotalIssuesOrWIs = cc.TotalIssueContributions
	stats.TotalRepos = gqlResp.Data.User.Repositories.TotalCount

	var minDate, maxDate string
	var totalDaysWithContribs int
	for _, week := range cc.ContributionCalendar.Weeks {
		for _, day := range week.ContributionDays {
			if day.ContributionCount > 0 {
				d, err := time.Parse("2006-01-02", day.Date)
				if err == nil && !d.Before(from) && !d.After(to) {
					stats.DailyContributions[day.Date] = day.ContributionCount
					totalDaysWithContribs++
					if minDate == "" || day.Date < minDate {
						minDate = day.Date
					}
					if day.Date > maxDate {
						maxDate = day.Date
					}
				}
			}
		}
	}
	logger.WithFields(logger.Fields{
		"platform":         "GitHub",
		"days_with_data":   totalDaysWithContribs,
		"data_range_start": minDate,
		"data_range_end":   maxDate,
	}).Debug("GitHub contributions parsed")

	// Fetch languages weighted by commit activity in the contribution period.
	// commitContributionsByRepository gives repos the user actually committed to,
	// so we weight each repo's language bytes by its share of total commits.
	// Skip language fetching in daily mode as it is the expensive part.
	if !skipLanguages {
		var repoContribs []repoContribution
		for _, rc := range cc.CommitContributionsByRepository {
			if rc.Repository.IsPrivate {
				continue
			}
			var entry repoContribution
			entry.Contributions.TotalCount = rc.Contributions.TotalCount
			entry.Repository.Name = rc.Repository.Name
			entry.Repository.Owner.Login = rc.Repository.Owner.Login
			repoContribs = append(repoContribs, entry)
		}
		if err = fetchGitHubLanguages(client, username, token, repoContribs, stats); err != nil {
			logger.WithFields(logger.Fields{
				"platform": "GitHub",
				"error":    err.Error(),
			}).Warn("could not fetch languages")
		}
	}

	return stats, nil
}

type repoContribution struct {
	Contributions struct {
		TotalCount int `json:"totalCount"`
	} `json:"contributions"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

func fetchGitHubLanguages(client *http.Client, username, token string, repoContribs []repoContribution, stats *PlatformStats) error {
	// Calculate total commits across all contributed repos for weighting
	totalCommits := 0
	for _, rc := range repoContribs {
		totalCommits += rc.Contributions.TotalCount
	}
	if totalCommits == 0 {
		return nil
	}

	successCount := 0
	for _, rc := range repoContribs {
		owner := rc.Repository.Owner.Login
		name := rc.Repository.Name
		commits := rc.Contributions.TotalCount
		weight := float64(commits) / float64(totalCommits)

		langURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/languages", owner, name)
		if logger.IsLevelEnabled(logger.DebugLevel) {
			logger.WithFields(logger.Fields{
				"platform": "GitHub",
				"endpoint": langURL,
				"method":   "GET",
				"repo":     owner + "/" + name,
			}).Debug("fetching repository languages")
		}
		langReq, err := http.NewRequest("GET", langURL, nil)
		if err != nil {
			continue
		}
		langReq.Header.Set("Authorization", "Bearer "+token)

		langResp, err := client.Do(langReq)
		if err != nil {
			continue
		}
		langBody, err := io.ReadAll(langResp.Body)
		langResp.Body.Close()
		if err != nil {
			continue
		}
		if langResp.StatusCode != http.StatusOK {
			continue
		}

		var langs map[string]int64
		if err = json.Unmarshal(langBody, &langs); err != nil {
			continue
		}

		// Weight language bytes by the repo's share of total commits
		for lang, byteCount := range langs {
			stats.Languages[lang] += int64(math.Round(float64(byteCount) * weight))
		}
		successCount++
		stats.TotalRepos++
	}
	if successCount == 0 && len(repoContribs) > 0 {
		return fmt.Errorf("all %d language requests failed", len(repoContribs))
	}
	return nil
}

// --- GitLab ---

func FetchGitLabStats(username, accessToken string, from, to time.Time, skipLanguages bool) (*PlatformStats, error) {
	start := time.Now()
	defer func() {
		logger.WithFields(logger.Fields{
			"platform": "GitLab",
			"elapsed":  time.Since(start).String(),
		}).Debug("platform fetch completed")
	}()

	stats := &PlatformStats{
		Languages:          make(map[string]int64),
		DailyContributions: make(map[string]int),
	}

	client := &http.Client{}

	// Fetch user ID
	userURL := fmt.Sprintf("https://gitlab.com/api/v4/users?username=%s", username)
	logger.WithFields(logger.Fields{
		"platform": "GitLab",
		"endpoint": userURL,
		"method":   "GET",
		"username": username,
	}).Debug("looking up GitLab user ID")
	req, err := http.NewRequest("GET", userURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("PRIVATE-TOKEN", accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error fetching GitLab user: status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var users []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err = json.Unmarshal(body, &users); err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("GitLab user not found")
	}

	userID := users[0].ID

	// Fetch events for the given date range
	page := 1

	for {
		afterParam := from.AddDate(0, 0, -1).Format("2006-01-02")
		beforeParam := to.AddDate(0, 0, 1).Format("2006-01-02")
		eventsURL := fmt.Sprintf("https://gitlab.com/api/v4/users/%d/events?after=%s&before=%s&page=%d&per_page=100",
			userID, afterParam, beforeParam, page)

		logger.WithFields(logger.Fields{
			"platform": "GitLab",
			"endpoint": "users/events",
			"method":   "GET",
			"after":    afterParam,
			"before":   beforeParam,
			"page":     page,
		}).Debug("fetching GitLab events page")
		eventsReq, err := http.NewRequest("GET", eventsURL, nil)
		if err != nil {
			return nil, err
		}
		eventsReq.Header.Add("PRIVATE-TOKEN", accessToken)

		eventsResp, err := client.Do(eventsReq)
		if err != nil {
			return nil, err
		}

		eventsBody, err := io.ReadAll(eventsResp.Body)
		eventsResp.Body.Close()
		if err != nil {
			return nil, err
		}

		if eventsResp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("error fetching GitLab events: status code %d", eventsResp.StatusCode)
		}

		var events []struct {
			Action     string `json:"action_name"`
			TargetType string `json:"target_type"`
			CreatedAt  string `json:"created_at"`
			PushData   struct {
				CommitCount int `json:"commit_count"`
			} `json:"push_data"`
		}
		if err = json.Unmarshal(eventsBody, &events); err != nil {
			return nil, err
		}
		if len(events) == 0 {
			break
		}

		for _, event := range events {
			date := ""
			if len(event.CreatedAt) >= 10 {
				date = event.CreatedAt[:10]
			}

			switch event.TargetType {
			case "Issue":
				stats.TotalIssuesOrWIs++
				if date != "" {
					stats.DailyContributions[date]++
				}
			case "MergeRequest":
				stats.TotalPRsOrMRs++
				if date != "" {
					stats.DailyContributions[date]++
				}
			}
			if strings.Contains(event.Action, "pushed") {
				stats.TotalCommits += event.PushData.CommitCount
				if date != "" {
					stats.DailyContributions[date] += event.PushData.CommitCount
				}
			}
		}

		page++
	}

	// Log contribution date range
	var glMin, glMax string
	for date := range stats.DailyContributions {
		if glMin == "" || date < glMin {
			glMin = date
		}
		if date > glMax {
			glMax = date
		}
	}
	logger.WithFields(logger.Fields{
		"platform":         "GitLab",
		"days_with_data":   len(stats.DailyContributions),
		"data_range_start": glMin,
		"data_range_end":   glMax,
	}).Debug("GitLab contributions parsed")

	// Fetch languages only from projects with recent activity.
	// Skip language fetching in daily mode as it is the expensive part.
	if !skipLanguages {
		if err = fetchGitLabLanguages(client, userID, accessToken, from, stats); err != nil {
			logger.WithFields(logger.Fields{
				"platform": "GitLab",
				"error":    err.Error(),
			}).Warn("could not fetch languages")
		}
	}

	return stats, nil
}

func fetchGitLabLanguages(client *http.Client, userID int, accessToken string, since time.Time, stats *PlatformStats) error {
	page := 1
	for {
		projectsURL := fmt.Sprintf("https://gitlab.com/api/v4/users/%d/projects?per_page=100&page=%d&owned=true&order_by=last_activity_at&sort=desc&statistics=true", userID, page)
		if logger.IsLevelEnabled(logger.DebugLevel) {
			logger.WithFields(logger.Fields{
				"platform":     "GitLab",
				"endpoint":     "users/projects",
				"method":       "GET",
				"page":         page,
				"since_cutoff": since.Format("2006-01-02"),
			}).Debug("fetching GitLab projects page for language analysis")
		}
		req, err := http.NewRequest("GET", projectsURL, nil)
		if err != nil {
			return err
		}
		req.Header.Add("PRIVATE-TOKEN", accessToken)

		resp, err := client.Do(req)
		if err != nil {
			return err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		var projects []struct {
			ID             int    `json:"id"`
			LastActivityAt string `json:"last_activity_at"`
			Statistics     struct {
				RepositorySize int64 `json:"repository_size"`
			} `json:"statistics"`
		}
		if err = json.Unmarshal(body, &projects); err != nil {
			return err
		}
		if len(projects) == 0 {
			break
		}

		allTooOld := true
		for _, proj := range projects {
			stats.TotalRepos++

			// Skip projects not active since the cutoff date
			lastActivity, err := time.Parse(time.RFC3339Nano, proj.LastActivityAt)
			if err != nil {
				lastActivity, err = time.Parse(time.RFC3339, proj.LastActivityAt)
			}
			if err != nil || lastActivity.Before(since) {
				continue
			}
			allTooOld = false
			langURL := fmt.Sprintf("https://gitlab.com/api/v4/projects/%d/languages", proj.ID)
			if logger.IsLevelEnabled(logger.DebugLevel) {
				logger.WithFields(logger.Fields{
					"platform":   "GitLab",
					"endpoint":   langURL,
					"method":     "GET",
					"project_id": proj.ID,
				}).Debug("fetching project languages")
			}
			langReq, err := http.NewRequest("GET", langURL, nil)
			if err != nil {
				continue
			}
			langReq.Header.Add("PRIVATE-TOKEN", accessToken)

			langResp, err := client.Do(langReq)
			if err != nil {
				continue
			}
			langBody, err := io.ReadAll(langResp.Body)
			langResp.Body.Close()
			if err != nil {
				continue
			}

			// GitLab returns percentages; convert to approximate bytes using repository_size
			var langs map[string]float64
			if err = json.Unmarshal(langBody, &langs); err != nil {
				continue
			}
			repoSize := proj.Statistics.RepositorySize
			for lang, pct := range langs {
				if repoSize > 0 {
					stats.Languages[lang] += int64(math.Round(float64(repoSize) * pct / 100.0))
				} else {
					stats.Languages[lang] += int64(pct * 100)
				}
			}
		}

		if allTooOld {
			break
		}
		if len(projects) < 100 {
			break
		}
		page++
	}
	return nil
}

// --- Azure DevOps ---

type adoProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func FetchAzureDevOpsStats(organization, accessToken string, from, to time.Time, skipLanguages bool) (*PlatformStats, error) {
	start := time.Now()
	defer func() {
		logger.WithFields(logger.Fields{
			"platform": "Azure DevOps",
			"elapsed":  time.Since(start).String(),
		}).Debug("platform fetch completed")
	}()

	stats := &PlatformStats{
		Languages:          make(map[string]int64),
		DailyContributions: make(map[string]int),
	}

	client := &http.Client{}
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+accessToken))

	newRequest := func(method, reqURL string, body io.Reader) (*http.Request, error) {
		req, err := http.NewRequest(method, reqURL, body)
		if err != nil {
			return nil, err
		}
		req.Header.Add("Authorization", authHeader)
		return req, nil
	}

	doRequest := func(req *http.Request) ([]byte, int, error) {
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, resp.StatusCode, err
		}
		return respBody, resp.StatusCode, nil
	}

	// Get authenticated user info
	connURL := fmt.Sprintf("https://dev.azure.com/%s/_apis/connectionData?api-version=7.0-preview", url.PathEscape(organization))
	logger.WithFields(logger.Fields{
		"platform": "Azure DevOps",
		"endpoint": connURL,
		"method":   "GET",
	}).Debug("fetching Azure DevOps connection data")
	req, err := newRequest("GET", connURL, nil)
	if err != nil {
		return nil, err
	}

	body, statusCode, err := doRequest(req)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("error fetching Azure DevOps connection data: status code %d", statusCode)
	}

	var connData struct {
		AuthenticatedUser struct {
			ID          string `json:"id"`
			DisplayName string `json:"providerDisplayName"`
		} `json:"authenticatedUser"`
	}
	if err = json.Unmarshal(body, &connData); err != nil {
		return nil, err
	}

	displayName := connData.AuthenticatedUser.DisplayName
	userID := connData.AuthenticatedUser.ID

	fromDate := from.Format("2006-01-02T15:04:05Z")
	toDate := to.Format("2006-01-02T15:04:05Z")

	// Get all projects
	var projects []adoProject

	continuationToken := ""
	for {
		projectsURL := fmt.Sprintf("https://dev.azure.com/%s/_apis/projects?api-version=7.0&$top=100", url.PathEscape(organization))
		if continuationToken != "" {
			projectsURL += "&continuationToken=" + url.QueryEscape(continuationToken)
		}

		logger.WithFields(logger.Fields{
			"platform": "Azure DevOps",
			"endpoint": "projects",
			"method":   "GET",
		}).Debug("fetching Azure DevOps projects page")
		req, err := newRequest("GET", projectsURL, nil)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("error fetching Azure DevOps projects: status code %d", resp.StatusCode)
		}

		var result struct {
			Count int          `json:"count"`
			Value []adoProject `json:"value"`
		}
		if err = json.Unmarshal(respBody, &result); err != nil {
			return nil, err
		}

		projects = append(projects, result.Value...)

		continuationToken = resp.Header.Get("x-ms-continuationtoken")
		if continuationToken == "" || result.Count == 0 {
			break
		}
	}

	// Track repos the user committed to for language detection
	var activeRepos []adoRepoRef

	for _, proj := range projects {
		// Get repos
		reposURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories?api-version=7.0",
			url.PathEscape(organization), url.PathEscape(proj.ID))
		req, err := newRequest("GET", reposURL, nil)
		if err != nil {
			continue
		}

		body, statusCode, err := doRequest(req)
		if err != nil || statusCode != http.StatusOK {
			continue
		}

		var reposResult struct {
			Value []struct {
				ID string `json:"id"`
			} `json:"value"`
		}
		if err = json.Unmarshal(body, &reposResult); err != nil {
			continue
		}

		// Count commits with dates
		for _, repo := range reposResult.Value {
			repoHadCommits := false
			repoUserCommits := 0
			skip := 0
			for {
				commitsURL := fmt.Sprintf(
					"https://dev.azure.com/%s/%s/_apis/git/repositories/%s/commits?searchCriteria.author=%s&searchCriteria.fromDate=%s&searchCriteria.toDate=%s&$top=100&$skip=%d&api-version=7.0",
					url.PathEscape(organization), url.PathEscape(proj.ID), url.PathEscape(repo.ID),
					url.QueryEscape(displayName), url.QueryEscape(fromDate), url.QueryEscape(toDate), skip,
				)
				if logger.IsLevelEnabled(logger.DebugLevel) {
					logger.WithFields(logger.Fields{
						"platform":   "Azure DevOps",
						"endpoint":   "commits",
						"method":     "GET",
						"from":       fromDate,
						"to":         toDate,
						"project_id": proj.ID,
						"repo_id":    repo.ID,
						"skip":       skip,
					}).Debug("fetching Azure DevOps commits")
				}
				req, err := newRequest("GET", commitsURL, nil)
				if err != nil {
					break
				}

				body, statusCode, err := doRequest(req)
				if err != nil || statusCode != http.StatusOK {
					break
				}

				var commitsResult struct {
					Count int `json:"count"`
					Value []struct {
						Author struct {
							Date string `json:"date"`
						} `json:"author"`
					} `json:"value"`
				}
				if err = json.Unmarshal(body, &commitsResult); err != nil {
					break
				}

				stats.TotalCommits += commitsResult.Count
				repoUserCommits += commitsResult.Count
				if commitsResult.Count > 0 {
					repoHadCommits = true
				}
				for _, commit := range commitsResult.Value {
					if len(commit.Author.Date) >= 10 {
						date := commit.Author.Date[:10]
						stats.DailyContributions[date]++
					}
				}

				if commitsResult.Count < 100 {
					break
				}
				skip += 100
			}
			if repoHadCommits {
				activeRepos = append(activeRepos, adoRepoRef{ProjectID: proj.ID, RepoID: repo.ID, UserCommits: repoUserCommits})
			}
		}

		// Count PRs
		skip := 0
		for {
			prsURL := fmt.Sprintf(
				"https://dev.azure.com/%s/%s/_apis/git/pullrequests?searchCriteria.creatorId=%s&searchCriteria.status=all&$top=100&$skip=%d&api-version=7.0",
				url.PathEscape(organization), url.PathEscape(proj.ID), url.PathEscape(userID), skip,
			)
			logger.WithFields(logger.Fields{
				"platform":    "Azure DevOps",
				"endpoint":    "pullrequests",
				"method":      "GET",
				"filter_from": from.Format(time.RFC3339),
				"filter_to":   to.Format(time.RFC3339),
				"project_id":  proj.ID,
				"skip":        skip,
			}).Debug("fetching Azure DevOps pull requests")
			req, err := newRequest("GET", prsURL, nil)
			if err != nil {
				break
			}

			body, statusCode, err := doRequest(req)
			if err != nil || statusCode != http.StatusOK {
				break
			}

			var prsResult struct {
				Count int `json:"count"`
				Value []struct {
					CreationDate string `json:"creationDate"`
				} `json:"value"`
			}
			if err = json.Unmarshal(body, &prsResult); err != nil {
				break
			}

			for _, pr := range prsResult.Value {
				prDate, err := time.Parse(time.RFC3339, pr.CreationDate)
				if err != nil {
					continue
				}
				if !prDate.Before(from) && !prDate.After(to) {
					stats.TotalPRsOrMRs++
					date := prDate.Format("2006-01-02")
					stats.DailyContributions[date]++
				}
			}

			if prsResult.Count < 100 {
				break
			}
			skip += 100
		}
	}

	stats.TotalRepos = len(activeRepos)

	// Fetch languages only from repos the user committed to.
	// Skip language fetching in daily mode as it is the expensive part.
	if !skipLanguages {
		fetchAzureDevOpsLanguages(newRequest, doRequest, organization, activeRepos, stats)
	}

	// Count work items
	wiqlURL := fmt.Sprintf("https://dev.azure.com/%s/_apis/wit/wiql?$top=20000&api-version=7.0", url.PathEscape(organization))
	wiqlQuery := fmt.Sprintf(
		`{"query": "SELECT [System.Id] FROM WorkItems WHERE [System.AssignedTo] = @Me AND [System.CreatedDate] >= '%s' AND [System.CreatedDate] <= '%s'"}`,
		from.Format("2006-01-02"), to.Format("2006-01-02"),
	)
	logger.WithFields(logger.Fields{
		"platform": "Azure DevOps",
		"endpoint": wiqlURL,
		"method":   "POST",
		"from":     from.Format("2006-01-02"),
		"to":       to.Format("2006-01-02"),
	}).Debug("querying Azure DevOps work items via WIQL")
	req, err = newRequest("POST", wiqlURL, strings.NewReader(wiqlQuery))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")

	body, statusCode, err = doRequest(req)
	if err == nil && statusCode == http.StatusOK {
		var wiqlResult struct {
			WorkItems []struct {
				ID int `json:"id"`
			} `json:"workItems"`
		}
		if err = json.Unmarshal(body, &wiqlResult); err == nil {
			stats.TotalIssuesOrWIs = len(wiqlResult.WorkItems)
		}
	}

	// Log contribution date range
	var adoMin, adoMax string
	for date := range stats.DailyContributions {
		if adoMin == "" || date < adoMin {
			adoMin = date
		}
		if date > adoMax {
			adoMax = date
		}
	}
	logger.WithFields(logger.Fields{
		"platform":         "Azure DevOps",
		"days_with_data":   len(stats.DailyContributions),
		"data_range_start": adoMin,
		"data_range_end":   adoMax,
	}).Debug("Azure DevOps contributions parsed")

	return stats, nil
}

// extensionToLanguage maps common file extensions to language names
var extensionToLanguage = map[string]string{
	".go": "Go", ".java": "Java", ".py": "Python", ".js": "JavaScript",
	".ts": "TypeScript", ".tsx": "TypeScript", ".jsx": "JavaScript",
	".cs": "C#", ".cpp": "C++", ".c": "C", ".h": "C", ".hpp": "C++",
	".rb": "Ruby", ".rs": "Rust", ".swift": "Swift", ".kt": "Kotlin",
	".scala": "Scala", ".php": "PHP", ".dart": "Dart", ".lua": "Lua",
	".sh": "Shell", ".bash": "Shell", ".zsh": "Shell", ".ps1": "PowerShell",
	".yaml": "YAML", ".yml": "YAML", ".json": "JSON", ".xml": "XML",
	".html": "HTML", ".css": "CSS", ".scss": "SCSS", ".less": "Less",
	".sql": "SQL", ".tf": "HCL", ".hcl": "HCL",
	".md": "Markdown", ".pas": "Pascal", ".pp": "Pascal", ".lpr": "Pascal",
	".r": "R", ".m": "Objective-C", ".mm": "Objective-C",
	".groovy": "Groovy", ".gradle": "Groovy", ".pl": "Perl",
}

// vendoredPrefixes lists directory prefixes for generated, vendored, or
// dependency paths that should be excluded from language byte counting.
// GitHub's Languages API already excludes these; this brings Azure DevOps
// tree-based estimation in line with that behavior.
var vendoredPrefixes = []string{
	"node_modules/", "vendor/", "dist/", "build/", ".git/",
	"__pycache__/", ".tox/", ".venv/", "venv/",
	"pods/", "carthage/", ".gradle/",
	"target/", "bin/", "obj/",
}

// vendoredFiles lists exact file names (basename) that should be excluded.
var vendoredFiles = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"go.sum": true, "Cargo.lock": true, "Gemfile.lock": true,
	"composer.lock": true, "poetry.lock": true, "pdm.lock": true,
}

func isVendoredOrGenerated(relativePath string) bool {
	lower := strings.ToLower(relativePath)
	for _, prefix := range vendoredPrefixes {
		if strings.HasPrefix(lower, prefix) || strings.Contains(lower, "/"+prefix) {
			return true
		}
	}
	base := filepath.Base(lower)
	if vendoredFiles[base] {
		return true
	}
	if strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".min.css") {
		return true
	}
	return false
}

type adoRepoRef struct {
	ProjectID   string
	RepoID      string
	UserCommits int // commits by the authenticated user in the date range
}

func fetchAzureDevOpsLanguages(
	newRequest func(string, string, io.Reader) (*http.Request, error),
	doRequest func(*http.Request) ([]byte, int, error),
	organization string,
	activeRepos []adoRepoRef,
	stats *PlatformStats,
) {
	for _, ref := range activeRepos {
		// Get repo metadata for default branch
		repoURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s?api-version=7.0",
			url.PathEscape(organization), url.PathEscape(ref.ProjectID), url.PathEscape(ref.RepoID))
		req, err := newRequest("GET", repoURL, nil)
		if err != nil {
			continue
		}

		body, statusCode, err := doRequest(req)
		if err != nil || statusCode != http.StatusOK {
			continue
		}

		var repoMeta struct {
			DefaultBranch string `json:"defaultBranch"`
		}
		if err = json.Unmarshal(body, &repoMeta); err != nil || repoMeta.DefaultBranch == "" {
			continue
		}

		// Strip refs/heads/ prefix for the versionDescriptor parameter
		branch := strings.TrimPrefix(repoMeta.DefaultBranch, "refs/heads/")

		// Fetch total commit count (all authors) for weighting.
		// Use a high $top to approximate; exact count is not critical.
		weight := 1.0
		allCommitsURL := fmt.Sprintf(
			"https://dev.azure.com/%s/%s/_apis/git/repositories/%s/commits?searchCriteria.itemVersion.version=%s&$top=10000&api-version=7.0",
			url.PathEscape(organization), url.PathEscape(ref.ProjectID), url.PathEscape(ref.RepoID),
			url.QueryEscape(branch),
		)
		req, err = newRequest("GET", allCommitsURL, nil)
		if err == nil {
			body, statusCode, err = doRequest(req)
			if err == nil && statusCode == http.StatusOK {
				var allCommitsResult struct {
					Count int `json:"count"`
				}
				if json.Unmarshal(body, &allCommitsResult) == nil && allCommitsResult.Count > 0 && ref.UserCommits > 0 {
					weight = float64(ref.UserCommits) / float64(allCommitsResult.Count)
					if weight > 1.0 {
						weight = 1.0
					}
					logger.WithFields(logger.Fields{
						"platform":     "Azure DevOps",
						"project_id":   ref.ProjectID,
						"repo_id":      ref.RepoID,
						"user_commits": ref.UserCommits,
						"all_commits":  allCommitsResult.Count,
						"weight":       fmt.Sprintf("%.4f", weight),
					}).Debug("computed commit-based weight for language attribution")
				}
			}
		}

		// Get latest commit on default branch, then fetch its detail to obtain treeId
		commitsURL := fmt.Sprintf(
			"https://dev.azure.com/%s/%s/_apis/git/repositories/%s/commits?searchCriteria.itemVersion.version=%s&$top=1&api-version=7.0",
			url.PathEscape(organization), url.PathEscape(ref.ProjectID), url.PathEscape(ref.RepoID),
			url.QueryEscape(branch),
		)
		req, err = newRequest("GET", commitsURL, nil)
		if err != nil {
			continue
		}

		body, statusCode, err = doRequest(req)
		if err != nil || statusCode != http.StatusOK {
			continue
		}

		var commitsResult struct {
			Value []struct {
				CommitID string `json:"commitId"`
			} `json:"value"`
		}
		if err = json.Unmarshal(body, &commitsResult); err != nil || len(commitsResult.Value) == 0 || commitsResult.Value[0].CommitID == "" {
			continue
		}

		commitID := commitsResult.Value[0].CommitID

		// Fetch individual commit detail to get treeId (not available in list response)
		commitURL := fmt.Sprintf(
			"https://dev.azure.com/%s/%s/_apis/git/repositories/%s/commits/%s?api-version=7.0",
			url.PathEscape(organization), url.PathEscape(ref.ProjectID), url.PathEscape(ref.RepoID),
			url.PathEscape(commitID),
		)
		req, err = newRequest("GET", commitURL, nil)
		if err != nil {
			continue
		}

		body, statusCode, err = doRequest(req)
		if err != nil || statusCode != http.StatusOK {
			continue
		}

		var commitDetail struct {
			TreeID string `json:"treeId"`
		}
		if err = json.Unmarshal(body, &commitDetail); err != nil || commitDetail.TreeID == "" {
			continue
		}

		treeID := commitDetail.TreeID

		// Fetch the full tree with file sizes (byte counts)
		treeURL := fmt.Sprintf(
			"https://dev.azure.com/%s/%s/_apis/git/repositories/%s/trees/%s?recursive=true&api-version=7.0",
			url.PathEscape(organization), url.PathEscape(ref.ProjectID), url.PathEscape(ref.RepoID),
			url.PathEscape(treeID),
		)
		logger.WithFields(logger.Fields{
			"platform":   "Azure DevOps",
			"endpoint":   "trees",
			"method":     "GET",
			"project_id": ref.ProjectID,
			"repo_id":    ref.RepoID,
		}).Debug("fetching repository tree for language analysis")
		req, err = newRequest("GET", treeURL, nil)
		if err != nil {
			continue
		}

		body, statusCode, err = doRequest(req)
		if err != nil || statusCode != http.StatusOK {
			continue
		}

		var treeResult struct {
			TreeEntries []struct {
				RelativePath  string `json:"relativePath"`
				GitObjectType string `json:"gitObjectType"`
				Size          int64  `json:"size"`
			} `json:"treeEntries"`
			Truncated bool `json:"truncated"`
		}
		if err = json.Unmarshal(body, &treeResult); err != nil {
			continue
		}

		if treeResult.Truncated {
			logger.WithFields(logger.Fields{
				"platform":   "Azure DevOps",
				"project_id": ref.ProjectID,
				"repo_id":    ref.RepoID,
			}).Warn("tree truncated, language data may be incomplete")
		}

		for _, entry := range treeResult.TreeEntries {
			if entry.GitObjectType != "blob" {
				continue
			}
			if isVendoredOrGenerated(entry.RelativePath) {
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.RelativePath))
			if lang, ok := extensionToLanguage[ext]; ok {
				stats.Languages[lang] += int64(math.Round(float64(entry.Size) * weight))
			}
		}
	}
}

// --- SVG Generators ---

func renderCombinedStatsSVG(platformStats []NamedPlatformStats) string {
	type statRow struct {
		Label  string
		Icon   string
		Values map[PlatformName]int64
		Total  int
	}

	totalCommits, totalPRs, totalIssues, totalRepos := 0, 0, 0, 0
	commitVals := make(map[PlatformName]int64)
	prVals := make(map[PlatformName]int64)
	issueVals := make(map[PlatformName]int64)
	repoVals := make(map[PlatformName]int64)

	for _, ns := range platformStats {
		totalCommits += ns.Stats.TotalCommits
		totalPRs += ns.Stats.TotalPRsOrMRs
		totalIssues += ns.Stats.TotalIssuesOrWIs
		totalRepos += ns.Stats.TotalRepos
		commitVals[ns.Platform] += int64(ns.Stats.TotalCommits)
		prVals[ns.Platform] += int64(ns.Stats.TotalPRsOrMRs)
		issueVals[ns.Platform] += int64(ns.Stats.TotalIssuesOrWIs)
		repoVals[ns.Platform] += int64(ns.Stats.TotalRepos)
	}
	// Estimate LoC from platforms with real byte counts (all platforms).
	const bytesPerLine = 40
	var realBytes int64
	locVals := make(map[PlatformName]int64)
	for _, ns := range platformStats {
		var platBytes int64
		for _, bytes := range ns.Stats.Languages {
			platBytes += bytes
		}
		realBytes += platBytes
		locVals[ns.Platform] = platBytes / bytesPerLine
	}
	linesOfCode := int(realBytes / bytesPerLine)

	iconCommits := `<path fill-rule="evenodd" d="M1.643 3.143L.427 1.927A.25.25 0 000 2.104V5.75c0 .138.112.25.25.25h3.646a.25.25 0 00.177-.427L2.715 4.215a6.5 6.5 0 11-1.18 4.458.75.75 0 10-1.493.154 8.001 8.001 0 101.6-5.684zM7.75 4a.75.75 0 01.75.75v2.992l2.028.812a.75.75 0 01-.557 1.392l-2.5-1A.75.75 0 017 8.25v-3.5A.75.75 0 017.75 4z"/>`
	iconPRs := `<path fill-rule="evenodd" d="M7.177 3.073L9.573.677A.25.25 0 0110 .854v4.792a.25.25 0 01-.427.177L7.177 3.427a.25.25 0 010-.354zM3.75 2.5a.75.75 0 100 1.5.75.75 0 000-1.5zm-2.25.75a2.25 2.25 0 113 2.122v5.256a2.251 2.251 0 11-1.5 0V5.372A2.25 2.25 0 011.5 3.25zM11 2.5h-1V4h1a1 1 0 011 1v5.628a2.251 2.251 0 101.5 0V5A2.5 2.5 0 0011 2.5zm1 10.25a.75.75 0 111.5 0 .75.75 0 01-1.5 0zM3.75 12a.75.75 0 100 1.5.75.75 0 000-1.5z"/>`
	iconIssues := `<path fill-rule="evenodd" d="M8 1.5a6.5 6.5 0 100 13 6.5 6.5 0 000-13zM0 8a8 8 0 1116 0A8 8 0 010 8zm9 3a1 1 0 11-2 0 1 1 0 012 0zm-.25-6.25a.75.75 0 00-1.5 0v3.5a.75.75 0 001.5 0v-3.5z"/>`
	iconRepos := `<path fill-rule="evenodd" d="M2 2.5A2.5 2.5 0 014.5 0h8.75a.75.75 0 01.75.75v12.5a.75.75 0 01-.75.75h-2.5a.75.75 0 110-1.5h1.75v-2h-8a1 1 0 00-1 1v.17a2.5 2.5 0 01-.286-.958A2.495 2.495 0 012 11.5v-9zm10.5-1h-8a1 1 0 00-1 1v6.708A2.486 2.486 0 014.5 9h8V1.5z"/>`
	iconCode := `<path fill-rule="evenodd" d="M4.72 3.22a.75.75 0 011.06 1.06L2.06 8l3.72 3.72a.75.75 0 11-1.06 1.06L.47 8.53a.75.75 0 010-1.06l4.25-4.25zm6.56 0a.75.75 0 10-1.06 1.06L13.94 8l-3.72 3.72a.75.75 0 101.06 1.06l4.25-4.25a.75.75 0 000-1.06l-4.25-4.25z"/>`

	rows := []statRow{
		{"Total Commits", iconCommits, commitVals, totalCommits},
		{"Total PRs / MRs", iconPRs, prVals, totalPRs},
		{"Total Issues / Work Items", iconIssues, issueVals, totalIssues},
		{"Total Repositories", iconRepos, repoVals, totalRepos},
		{"Lines of Code", iconCode, locVals, linesOfCode},
	}

	const barAreaX = 210
	const barAreaW = 150
	const valueX = 445

	var body string
	for i, row := range rows {
		delay := 450 + i*150

		body += fmt.Sprintf(`<g class="stagger" style="animation-delay: %dms" transform="translate(25, %d)">`, delay, 45+i*28)
		body += fmt.Sprintf(`<svg data-testid="icon" class="icon" viewBox="0 0 16 16" version="1.1" width="16" height="16">%s</svg>`, row.Icon)
		body += fmt.Sprintf(`<text class="stat" x="25" y="12.5">%s</text>`, row.Label)

		// Stacked bar
		var barTotal int64
		for _, v := range row.Values {
			barTotal += v
		}
		if barTotal > 0 {
			bx := barAreaX
			remaining := barAreaW
			nonZero := 0
			for _, p := range platformOrder {
				if row.Values[p] > 0 {
					nonZero++
				}
			}
			for _, p := range platformOrder {
				v := row.Values[p]
				if v == 0 || remaining <= 0 || nonZero <= 0 {
					continue
				}
				var segW int
				if nonZero == 1 {
					segW = remaining
				} else {
					segW = int(float64(v) / float64(barTotal) * float64(barAreaW))
					if segW < 2 {
						segW = 2
					}
					if segW > remaining {
						segW = remaining
					}
				}
				body += fmt.Sprintf(`<rect x="%d" y="0" width="%d" height="16" rx="2" fill="%s"><title>%s: %d</title></rect>`, bx, segW, p.Color(), string(p), v)
				bx += segW
				remaining -= segW
				nonZero--
			}
		}

		// Value
		body += fmt.Sprintf(`<text class="stat bold" x="%d" y="12.5" text-anchor="end" data-testid="value">%s</text>`, valueX, formatNumber(row.Total))
		body += `</g>`
	}

	legend := renderPlatformLegend(25, 230)

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="495" height="260" viewBox="0 0 495 260" fill="none" role="img">
<title>Combined Stats</title>
<style>
	.header { font: 600 14px 'Segoe UI', Ubuntu, Sans-Serif; fill: #fff; }
	.stat { font: 400 12px 'Segoe UI', Ubuntu, Sans-Serif; fill: #c9d1d9; }
	.bold { font-weight: 700; font-size: 12px; fill: #9f9f9f; }
	.icon { fill: #79ff97; display: block; }
	.legend-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
	.stagger { opacity: 0; animation: fadeInAnimation 0.3s ease-in-out forwards; }
	@keyframes fadeInAnimation { from { opacity: 0; } to { opacity: 1; } }
</style>
<rect data-testid="card-bg" x="0.5" y="0.5" rx="4.5" height="99%%" width="494" fill="#151515" stroke="#e4e2e2" stroke-opacity="0.2"/>
<g data-testid="card-title" transform="translate(25, 25)">
	<text x="0" y="0" class="header" data-testid="header">Stats (across all platforms)</text>
</g>
%s
%s
</svg>`, body, legend)
}

func GenerateCombinedStatsSVG(platformStats []NamedPlatformStats, outputPath string) error {
	svgContent := renderCombinedStatsSVG(platformStats)
	return os.WriteFile(outputPath, []byte(svgContent), 0644)
}

func renderTokensHeatmap(tokens []TokenUsage) (string, error) {
	if len(tokens) == 0 {
		return "", fmt.Errorf("no token data")
	}

	// Build date->tokens map
	tokenMap := make(map[string]int)
	for _, t := range tokens {
		tokenMap[t.Date] = t.Tokens
	}

	// Find date range
	minDate, _ := time.Parse("2006-01-02", tokens[0].Date)
	maxDate, _ := time.Parse("2006-01-02", tokens[len(tokens)-1].Date)
	for _, t := range tokens {
		d, err := time.Parse("2006-01-02", t.Date)
		if err != nil {
			continue
		}
		if d.Before(minDate) {
			minDate = d
		}
		if d.After(maxDate) {
			maxDate = d
		}
	}

	// Ensure at least a full year range for consistent sizing
	startDate := time.Date(minDate.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(minDate.Year(), 12, 31, 23, 59, 59, 0, time.UTC)
	if maxDate.After(endDate) {
		endDate = maxDate
	}

	// For a calendar-year view (Jan 1 start), do not rewind to the previous
	// Sunday as that would include days from the previous year.
	isCalendarYear := startDate.Day() == 1 && startDate.Month() == time.January
	if !isCalendarYear {
		for startDate.Weekday() != time.Sunday {
			startDate = startDate.AddDate(0, 0, -1)
		}
	}

	// Find max tokens for scaling
	maxTokens := 1
	for _, t := range tokens {
		if t.Tokens > maxTokens {
			maxTokens = t.Tokens
		}
	}

	purpleScale := [4]string{"#1a1030", "#3d2070", "#6840a0", "#8884d8"}
	getColor := func(count int) string {
		if count == 0 {
			return "#161b22"
		}
		ratio := float64(count) / float64(maxTokens)
		switch {
		case ratio <= 0.25:
			return purpleScale[0]
		case ratio <= 0.50:
			return purpleScale[1]
		case ratio <= 0.75:
			return purpleScale[2]
		default:
			return purpleScale[3]
		}
	}

	cellSize := 13
	cellGap := 3
	padLeft := 60
	padTop := 55
	padBottom := 20
	legendHeight := 20

	totalDays := int(endDate.Sub(startDate).Hours()/24) + 1
	weeks := (totalDays + 6) / 7
	if weeks < 1 {
		weeks = 1
	}
	// Standardize to 53 weeks width (full year) for consistent sizing
	displayWeeks := 53
	if weeks > displayWeeks {
		displayWeeks = weeks
	}
	width := padLeft + displayWeeks*(cellSize+cellGap) + 25
	height := padTop + 7*(cellSize+cellGap) + padBottom + legendHeight

	var cells string
	dayLabels := []string{"", "Mon", "", "Wed", "", "Fri", ""}
	for i, label := range dayLabels {
		if label != "" {
			y := padTop + i*(cellSize+cellGap) + cellSize - 2
			cells += fmt.Sprintf(`<text x="25" y="%d" class="day-label">%s</text>`, y, label)
		}
	}

	// Month labels
	currentDate := startDate
	lastMonth := -1
	monthNames := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	for w := 0; w < weeks; w++ {
		d := currentDate.AddDate(0, 0, w*7)
		month := int(d.Month()) - 1
		if month != lastMonth {
			x := padLeft + w*(cellSize+cellGap)
			cells += fmt.Sprintf(`<text x="%d" y="%d" class="month-label">%s</text>`, x, padTop-8, monthNames[month])
			lastMonth = month
		}
	}

	// Cells
	for w := 0; w < weeks; w++ {
		for d := 0; d < 7; d++ {
			date := startDate.AddDate(0, 0, w*7+d)
			if date.After(endDate) {
				continue
			}
			dateStr := date.Format("2006-01-02")
			count := tokenMap[dateStr]
			x := padLeft + w*(cellSize+cellGap)
			y := padTop + d*(cellSize+cellGap)
			color := getColor(count)
			tooltip := fmt.Sprintf("%s: %s tokens", dateStr, formatNumber(count))
			cells += fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" rx="2" fill="%s"><title>%s</title></rect>`,
				x, y, cellSize, cellSize, color, tooltip)
		}
	}

	// Intensity legend
	legendY := padTop + 7*(cellSize+cellGap) + 12
	legendLabels := []string{"Less", "", "", "", "More"}
	legendColors := []string{"#161b22", purpleScale[0], purpleScale[1], purpleScale[2], purpleScale[3]}
	dx := 25
	for i, color := range legendColors {
		cells += fmt.Sprintf(`<rect x="%d" y="%d" width="10" height="10" rx="2" fill="%s"/>`, dx, legendY, color)
		if legendLabels[i] != "" {
			cells += fmt.Sprintf(`<text x="%d" y="%d" class="legend-label">%s</text>`, dx+14, legendY+9, legendLabels[i])
			dx += 14 + len(legendLabels[i])*6 + 8
		} else {
			dx += 14
		}
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
<style>
	.title { font: 600 14px 'Segoe UI', Ubuntu, Sans-Serif; fill: #fff; }
	.day-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
	.month-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
	.legend-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
</style>
<rect width="%d" height="%d" rx="4.5" fill="#151515" stroke="#e4e2e2" stroke-opacity="0.2"/>
<text x="25" y="25" class="title">Claude Code Tokens (by day)</text>
%s
</svg>`, width, height, width, height, width, height, cells)

	return svg, nil
}

func GenerateTokensHeatmap(tokens []TokenUsage, outputPath string) error {
	svg, err := renderTokensHeatmap(tokens)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, []byte(svg), 0644)
}

func renderLanguagesBarChart(languages map[string]map[PlatformName]int64) (string, error) {
	// Calculate totals and sort
	type langEntry struct {
		Name      string
		Total     int64
		Platforms map[PlatformName]int64
	}
	var entries []langEntry
	for name, platforms := range languages {
		var total int64
		for _, bytes := range platforms {
			total += bytes
		}
		entries = append(entries, langEntry{name, total, platforms})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Total > entries[j].Total })
	if len(entries) > 5 {
		entries = entries[:5]
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("no language data")
	}

	var grandTotal int64
	for _, e := range entries {
		grandTotal += e.Total
	}
	if grandTotal == 0 {
		return "", fmt.Errorf("all language byte counts are zero")
	}

	maxBytes := entries[0].Total

	const barAreaX = 210
	const barAreaW = 150
	const valueX = 445

	var body string
	for i, e := range entries {
		pct := float64(e.Total) / float64(grandTotal) * 100
		totalBarW := int(float64(e.Total) / float64(maxBytes) * float64(barAreaW))
		if totalBarW < 2 {
			totalBarW = 2
		}

		delay := 450 + i*150
		body += fmt.Sprintf(`<g class="stagger" style="animation-delay: %dms" transform="translate(25, %d)">`, delay, 45+i*28)
		langColor := languageColor(e.Name)
		body += fmt.Sprintf(`<circle cx="7" cy="8" r="6" fill="%s"/>`, langColor)
		body += fmt.Sprintf(`<text class="stat" x="20" y="12.5">%s</text>`, e.Name)

		// Stacked bar segments by platform
		bx := barAreaX
		remaining := totalBarW
		nonZero := 0
		for _, p := range platformOrder {
			if e.Platforms[p] > 0 {
				nonZero++
			}
		}
		for _, p := range platformOrder {
			v := e.Platforms[p]
			if v == 0 || remaining <= 0 || nonZero <= 0 {
				continue
			}
			var segW int
			if nonZero == 1 {
				segW = remaining
			} else {
				segW = int(float64(v) / float64(e.Total) * float64(totalBarW))
				if segW < 2 {
					segW = 2
				}
				if segW > remaining {
					segW = remaining
				}
			}
			platformPct := float64(v) / float64(e.Total) * 100.0
			body += fmt.Sprintf(`<rect x="%d" y="0" width="%d" height="16" rx="2" fill="%s"><title>%s: %.1f%%</title></rect>`, bx, segW, p.Color(), string(p), platformPct)
			bx += segW
			remaining -= segW
			nonZero--
		}

		body += fmt.Sprintf(`<text class="stat bold" x="%d" y="12.5" text-anchor="end">%.1f%%</text>`, valueX, pct)
		body += `</g>`
	}

	legend := renderPlatformLegend(25, 230)

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="495" height="260" viewBox="0 0 495 260" fill="none" role="img">
<title>Top Languages</title>
<style>
	.header { font: 600 14px 'Segoe UI', Ubuntu, Sans-Serif; fill: #fff; }
	.stat { font: 400 12px 'Segoe UI', Ubuntu, Sans-Serif; fill: #c9d1d9; }
	.bold { font-weight: 700; font-size: 12px; fill: #9f9f9f; }
	.icon { fill: #79ff97; display: block; }
	.legend-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
	.stagger { opacity: 0; animation: fadeInAnimation 0.3s ease-in-out forwards; }
	@keyframes fadeInAnimation { from { opacity: 0; } to { opacity: 1; } }
</style>
<rect data-testid="card-bg" x="0.5" y="0.5" rx="4.5" height="99%%" width="494" fill="#151515" stroke="#e4e2e2" stroke-opacity="0.2"/>
<g data-testid="card-title" transform="translate(25, 25)">
	<text x="0" y="0" class="header" data-testid="header">Top Languages (across all platforms)</text>
</g>
%s
%s
</svg>`, body, legend)

	return svg, nil
}

func GenerateLanguagesBarChart(languages map[string]map[PlatformName]int64, outputPath string) error {
	svg, err := renderLanguagesBarChart(languages)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, []byte(svg), 0644)
}

func renderContributionHeatmap(contributions map[string]map[PlatformName]int, startDate, endDate time.Time) string {
	cellSize := 13
	cellGap := 3
	padLeft := 60
	padTop := 55
	padBottom := 20
	legendHeight := 20

	// Always rewind to the previous Sunday so the grid rows match
	// the Mon/Wed/Fri day labels. Pre-January cells render as empty.
	for startDate.Weekday() != time.Sunday {
		startDate = startDate.AddDate(0, 0, -1)
	}

	// Find max total count across all days
	maxCount := 1
	for _, platforms := range contributions {
		total := 0
		for _, c := range platforms {
			total += c
		}
		if total > maxCount {
			maxCount = total
		}
	}

	// Determine platform combo and color for a day
	activeCombos := make(map[PlatformCombo]bool)
	getColor := func(platforms map[PlatformName]int) (string, PlatformCombo) {
		total := 0
		var combo PlatformCombo
		for p, c := range platforms {
			if c > 0 {
				total += c
				combo |= platformToCombo(p)
			}
		}
		if total == 0 {
			return "#161b22", 0
		}
		activeCombos[combo] = true

		scale := comboColorScale(combo)
		ratio := float64(total) / float64(maxCount)
		switch {
		case ratio <= 0.25:
			return scale[0], combo
		case ratio <= 0.50:
			return scale[1], combo
		case ratio <= 0.75:
			return scale[2], combo
		default:
			return scale[3], combo
		}
	}

	totalDays := int(endDate.Sub(startDate).Hours()/24) + 1
	weeks := (totalDays + 6) / 7
	if weeks < 1 {
		weeks = 1
	}
	// Standardize to 53 weeks width (full year) for consistent sizing with tokens heatmap
	displayWeeks := 53
	if weeks > displayWeeks {
		displayWeeks = weeks
	}
	width := padLeft + displayWeeks*(cellSize+cellGap) + 25
	height := padTop + 7*(cellSize+cellGap) + padBottom + legendHeight

	var cells string
	dayLabels := []string{"", "Mon", "", "Wed", "", "Fri", ""}
	for i, label := range dayLabels {
		if label != "" {
			y := padTop + i*(cellSize+cellGap) + cellSize - 2
			cells += fmt.Sprintf(`<text x="25" y="%d" class="day-label">%s</text>`, y, label)
		}
	}

	currentDate := startDate
	lastMonth := -1
	monthNames := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	for w := 0; w < weeks; w++ {
		d := currentDate.AddDate(0, 0, w*7)
		month := int(d.Month()) - 1
		if month != lastMonth {
			x := padLeft + w*(cellSize+cellGap)
			cells += fmt.Sprintf(`<text x="%d" y="%d" class="month-label">%s</text>`, x, padTop-8, monthNames[month])
			lastMonth = month
		}
	}

	for w := 0; w < weeks; w++ {
		for d := 0; d < 7; d++ {
			date := startDate.AddDate(0, 0, w*7+d)
			if date.After(endDate) {
				continue
			}
			dateStr := date.Format("2006-01-02")
			platforms := contributions[dateStr]
			x := padLeft + w*(cellSize+cellGap)
			y := padTop + d*(cellSize+cellGap)
			color, _ := getColor(platforms)

			// Build tooltip with per-platform breakdown
			total := 0
			for _, c := range platforms {
				total += c
			}
			tooltip := fmt.Sprintf("%s: %d contributions", dateStr, total)
			if total > 0 {
				var parts []string
				for _, p := range platformOrder {
					if platforms[p] > 0 {
						parts = append(parts, fmt.Sprintf("%s: %d", string(p), platforms[p]))
					}
				}
				tooltip = fmt.Sprintf("%s: %d (%s)", dateStr, total, strings.Join(parts, ", "))
			}

			cells += fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" rx="2" fill="%s"><title>%s</title></rect>`,
				x, y, cellSize, cellSize, color, tooltip)
		}
	}

	// Platform combo legend (show only combos that appear in the data)
	legendY := padTop + 7*(cellSize+cellGap) + 12
	comboOrder := []PlatformCombo{
		comboGitHub, comboGitLab, comboAzureDevOps,
		comboGitHub | comboGitLab, comboGitHub | comboAzureDevOps, comboGitLab | comboAzureDevOps,
		comboGitHub | comboGitLab | comboAzureDevOps,
	}
	dx := 25
	row := 0
	maxDx := width - 25
	// Always show all 3 single-platform labels, plus any active combo labels
	for _, combo := range comboOrder {
		isSinglePlatform := combo == comboGitHub || combo == comboGitLab || combo == comboAzureDevOps
		if !isSinglePlatform && !activeCombos[combo] {
			continue
		}
		label := comboLabels[combo]
		scale := comboColorScale(combo)
		entryWidth := 14 + len(label)*6 + 12
		if dx+entryWidth > maxDx && dx > 25 {
			row++
			dx = 25
		}
		y := legendY + row*16
		cells += fmt.Sprintf(`<rect x="%d" y="%d" width="10" height="10" rx="2" fill="%s"/>`, dx, y, scale[3])
		cells += fmt.Sprintf(`<text x="%d" y="%d" class="legend-label">%s</text>`, dx+14, y+9, label)
		dx += entryWidth
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
<style>
	.title { font: 600 14px 'Segoe UI', Ubuntu, Sans-Serif; fill: #fff; }
	.day-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
	.month-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
	.legend-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
</style>
<rect width="%d" height="%d" rx="4.5" fill="#151515" stroke="#e4e2e2" stroke-opacity="0.2"/>
<text x="25" y="25" class="title">Contributions (across all platforms)</text>
%s
</svg>`, width, height, width, height, width, height, cells)
}

func GenerateContributionHeatmap(contributions map[string]map[PlatformName]int, startDate, endDate time.Time, outputPath string) error {
	svg := renderContributionHeatmap(contributions, startDate, endDate)
	return os.WriteFile(outputPath, []byte(svg), 0644)
}

// --- Helpers ---

func formatNumber(n int) string {
	if n >= 1000000000 {
		return fmt.Sprintf("%.1fB", float64(n)/1000000000)
	}
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func clipDailyContributionsToRange(stats *PlatformStats, from, to time.Time) {
	for date := range stats.DailyContributions {
		d, err := time.Parse("2006-01-02", date)
		if err != nil || d.Before(from) || d.After(to) {
			delete(stats.DailyContributions, date)
		}
	}
}

// topNLanguagesForPlatform returns the top N languages by value, with values
// normalized to a percentage scale (value * 10000 / total) so that platforms
// using different units (bytes, percentages, file counts) contribute equally.
func topNLanguagesForPlatform(langTotals map[string]int64, n int) map[string]int64 {
	if len(langTotals) == 0 {
		return make(map[string]int64)
	}

	type entry struct {
		name  string
		value int64
	}
	var entries []entry
	for name, val := range langTotals {
		if val > 0 {
			entries = append(entries, entry{name, val})
		}
	}
	if len(entries) == 0 {
		return make(map[string]int64)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].value > entries[j].value })
	if len(entries) > n {
		entries = entries[:n]
	}

	var total int64
	for _, e := range entries {
		total += e.value
	}

	result := make(map[string]int64, len(entries))
	for _, e := range entries {
		result[e.name] = e.value * 10000 / total
	}
	return result
}

func aggregateLanguagesByPlatform(named []NamedPlatformStats) map[string]map[PlatformName]int64 {
	// Phase 1: Collect raw totals per platform
	platformLangs := make(map[PlatformName]map[string]int64)
	for _, ns := range named {
		if platformLangs[ns.Platform] == nil {
			platformLangs[ns.Platform] = make(map[string]int64)
		}
		for lang, val := range ns.Stats.Languages {
			platformLangs[ns.Platform][lang] += val
		}
	}

	// Phase 2: For each platform, keep top 5 and normalize to common scale
	normalizedPlatformLangs := make(map[PlatformName]map[string]int64)
	for platform, langs := range platformLangs {
		normalizedPlatformLangs[platform] = topNLanguagesForPlatform(langs, 5)
		for lang, val := range normalizedPlatformLangs[platform] {
			logger.WithFields(logger.Fields{
				"platform":   string(platform),
				"language":   lang,
				"normalized": val,
			}).Debug("normalized language (top 5, basis points)")
		}
	}

	// Phase 3: Combine into language -> platform -> normalized value
	result := make(map[string]map[PlatformName]int64)
	for platform, langs := range normalizedPlatformLangs {
		for lang, val := range langs {
			if result[lang] == nil {
				result[lang] = make(map[PlatformName]int64)
			}
			result[lang][platform] = val
		}
	}

	// Log final combined result
	for lang, platforms := range result {
		fields := logger.Fields{"language": lang}
		for p, v := range platforms {
			fields[string(p)] = v
		}
		logger.WithFields(fields).Debug("combined language entry for chart")
	}

	return result
}

func aggregateContributionsByPlatform(named []NamedPlatformStats) map[string]map[PlatformName]int {
	result := make(map[string]map[PlatformName]int)
	for _, ns := range named {
		for date, count := range ns.Stats.DailyContributions {
			if result[date] == nil {
				result[date] = make(map[PlatformName]int)
			}
			result[date][ns.Platform] += count
		}
	}
	return result
}

var githubLanguageColors = map[string]string{
	"Go":            "#00ADD8",
	"Python":        "#3572A5",
	"JavaScript":    "#f1e05a",
	"TypeScript":    "#3178c6",
	"Java":          "#b07219",
	"C#":            "#178600",
	"C++":           "#f34b7d",
	"C":             "#555555",
	"Ruby":          "#701516",
	"PHP":           "#4F5D95",
	"Rust":          "#dea584",
	"Swift":         "#F05138",
	"Kotlin":        "#A97BFF",
	"Dart":          "#00B4AB",
	"Shell":         "#89e051",
	"HTML":          "#e34c26",
	"CSS":           "#563d7c",
	"Scala":         "#c22d40",
	"Lua":           "#000080",
	"Perl":          "#0298c3",
	"R":             "#198CE7",
	"Haskell":       "#5e5086",
	"Elixir":        "#6e4a7e",
	"Clojure":       "#db5855",
	"Erlang":        "#B83998",
	"Zig":           "#ec915c",
	"Nix":           "#7e7eff",
	"HCL":           "#844FBA",
	"Terraform":     "#7B42BC",
	"Makefile":      "#427819",
	"Dockerfile":    "#384d54",
	"Pascal":        "#E3F171",
	"TeX":           "#3D6117",
	"Go Template":   "#00ADD8",
	"Vue":           "#41b883",
	"Svelte":        "#ff3e00",
	"SCSS":          "#c6538c",
	"YAML":          "#cb171e",
	"JSON":          "#292929",
	"Markdown":      "#083fa1",
	"Groovy":        "#4298b8",
	"PowerShell":    "#012456",
}

func languageColor(name string) string {
	if color, ok := githubLanguageColors[name]; ok {
		return color
	}
	return "#8b949e"
}

func renderPlatformLegend(x, y int) string {
	var legend string
	dx := x
	for _, p := range platformOrder {
		legend += fmt.Sprintf(`<rect x="%d" y="%d" width="10" height="10" rx="2" fill="%s"/>`, dx, y, p.Color())
		legend += fmt.Sprintf(`<text x="%d" y="%d" class="legend-label">%s</text>`, dx+14, y+9, string(p))
		dx += 14 + len(string(p))*7 + 12
	}
	return legend
}

func loadTokenUsage(path string) ([]TokenUsage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tokens []TokenUsage
	if err = json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}
	// Sort by date
	sort.Slice(tokens, func(i, j int) bool { return tokens[i].Date < tokens[j].Date })
	return tokens, nil
}

// --- Main ---

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func main() {
	logger.SetFormatter(&logger.TextFormatter{ForceColors: true, FullTimestamp: true})
	logger.SetOutput(os.Stdout)

	logLevelStr := strings.ToLower(getEnvOrDefault("LOG_LEVEL", "info"))
	if level, err := logger.ParseLevel(logLevelStr); err == nil {
		logger.SetLevel(level)
	} else {
		// Fall back to info level if LOG_LEVEL is invalid
		logger.SetLevel(logger.InfoLevel)
		logger.WithField("LOG_LEVEL", logLevelStr).Warn("Invalid LOG_LEVEL, defaulting to info")
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	currentYear := now.Year()

	mode := getEnvOrDefault("RUN_MODE", "daily")
	var targetYear int

	var from, to time.Time
	todayDate := now.UTC().Truncate(24 * time.Hour)
	nowUTC := now.UTC()

	if mode == "bootstrap" {
		from = time.Date(todayDate.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		to = nowUTC
	} else if mode == "recalculate" {
		targetYearStr := os.Getenv("TARGET_YEAR")
		if targetYearStr == "" {
			logger.Fatal("TARGET_YEAR is required for recalculate mode")
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(targetYearStr))
		targetYear = parsed
		if err != nil || targetYear < 2000 || targetYear > todayDate.Year() {
			logger.WithFields(logger.Fields{
				"min_year":    2000,
				"max_year":    todayDate.Year(),
				"target_year": targetYearStr,
			}).Fatal("TARGET_YEAR is out of valid range")
		}
		from = time.Date(targetYear, 1, 1, 0, 0, 0, 0, time.UTC)
		if targetYear == todayDate.Year() {
			to = nowUTC
		} else {
			to = time.Date(targetYear, 12, 31, 23, 59, 59, 0, time.UTC)
		}
	} else {
		from = todayDate.AddDate(0, 0, -1) // start from yesterday to capture a full day of contributions
		to = nowUTC
	}

	logger.WithFields(logger.Fields{
		"mode": mode,
		"from": from.Format(time.RFC3339),
		"to":   to.Format(time.RFC3339),
	}).Info("starting stats generation")

	historyPath := getEnvOrDefault("STATS_HISTORY_PATH", "stats_history.json")
	outputDir := getEnvOrDefault("SVG_OUTPUT_DIR", ".")

	// 1. Load existing history
	logger.WithFields(logger.Fields{
		"path": historyPath,
	}).Info("loading stats history")
	history, err := loadStatsHistory(historyPath)
	if err != nil {
		logger.WithFields(logger.Fields{
			"path":  historyPath,
			"error": err.Error(),
		}).Warn("could not load history, creating new history file")
		historyPath = historyPath + ".new"
		history = &StatsHistory{Version: 1}
	}

	// 2. Fetch fresh platform data (in parallel)
	type fetchResult struct {
		Platform PlatformName
		Stats    *PlatformStats
		Err      error
	}

	resultCh := make(chan fetchResult, 3)
	fetchers := 0
	fetchStart := time.Now()

	ghUsername := os.Getenv("GITHUB_USERNAME")
	ghToken := os.Getenv("GH_TOKEN")
	if ghUsername != "" && ghToken != "" {
		fetchers++
		go func() {
			logger.WithFields(logger.Fields{
				"platform": "GitHub",
				"from":     from.Format(time.RFC3339),
				"to":       to.Format(time.RFC3339),
			}).Info("fetching platform stats")
			stats, err := FetchGitHubStats(ghUsername, ghToken, from, to, mode == "daily")
			resultCh <- fetchResult{PlatformGitHub, stats, err}
		}()
	}

	glUsername := os.Getenv("GITLAB_USERNAME")
	glToken := os.Getenv("GITLAB_ACCESS_TOKEN")
	if glUsername != "" && glToken != "" {
		fetchers++
		go func() {
			logger.WithFields(logger.Fields{
				"platform": "GitLab",
				"from":     from.Format(time.RFC3339),
				"to":       to.Format(time.RFC3339),
			}).Info("fetching platform stats")
			stats, err := FetchGitLabStats(glUsername, glToken, from, to, mode == "daily")
			resultCh <- fetchResult{PlatformGitLab, stats, err}
		}()
	}

	adoOrg := os.Getenv("AZURE_DEVOPS_ORG")
	adoToken := os.Getenv("AZURE_DEVOPS_ACCESS_TOKEN")
	if adoOrg != "" && adoToken != "" {
		fetchers++
		go func() {
			logger.WithFields(logger.Fields{
				"platform": "Azure DevOps",
				"from":     from.Format(time.RFC3339),
				"to":       to.Format(time.RFC3339),
			}).Info("fetching platform stats")
			stats, err := FetchAzureDevOpsStats(adoOrg, adoToken, from, to, mode == "daily")
			resultCh <- fetchResult{PlatformAzureDevOps, stats, err}
		}()
	}

	var namedStats []NamedPlatformStats
	for i := 0; i < fetchers; i++ {
		r := <-resultCh
		if r.Err != nil {
			logger.WithFields(logger.Fields{
				"platform": string(r.Platform),
				"error":    r.Err.Error(),
			}).Warn("skipping platform due to fetch error")
			continue
		}
		logger.WithFields(logger.Fields{
			"platform":      string(r.Platform),
			"commits":       r.Stats.TotalCommits,
			"prs_or_mrs":    r.Stats.TotalPRsOrMRs,
			"issues_or_wis": r.Stats.TotalIssuesOrWIs,
			"repos":         r.Stats.TotalRepos,
			"languages":     len(r.Stats.Languages),
		}).Info("platform stats fetched")
		// Log top 5 raw languages for debugging
		type langEntry struct {
			name  string
			bytes int64
		}
		var topLangs []langEntry
		for lang, bytes := range r.Stats.Languages {
			topLangs = append(topLangs, langEntry{lang, bytes})
		}
		sort.Slice(topLangs, func(i, j int) bool { return topLangs[i].bytes > topLangs[j].bytes })
		if len(topLangs) > 5 {
			topLangs = topLangs[:5]
		}
		for _, l := range topLangs {
			logger.WithFields(logger.Fields{
				"platform": string(r.Platform),
				"language": l.name,
				"bytes":    l.bytes,
			}).Debug("raw language data")
		}
		namedStats = append(namedStats, NamedPlatformStats{r.Platform, r.Stats})
	}

	logger.WithFields(logger.Fields{
		"elapsed":   time.Since(fetchStart).String(),
		"platforms": len(namedStats),
	}).Info("all platform fetches completed")

	if len(namedStats) == 0 {
		logger.Fatal("no platform credentials configured; set GITHUB_USERNAME/GH_TOKEN, GITLAB_USERNAME/GITLAB_ACCESS_TOKEN, or AZURE_DEVOPS_ORG/AZURE_DEVOPS_ACCESS_TOKEN")
	}

	// In daily mode, reuse languages from the latest existing snapshot
	if mode == "daily" && len(history.Snapshots) > 0 {
		latest := history.Snapshots[len(history.Snapshots)-1]
		for i, ns := range namedStats {
			if ps, ok := latest.Platforms[ns.Platform]; ok && len(ns.Stats.Languages) == 0 {
				namedStats[i].Stats.Languages = make(map[string]int64)
				for lang, bytes := range ps.Languages {
					namedStats[i].Stats.Languages[lang] = bytes
				}
			}
		}
	}

	// Clip daily contributions to [from, to] as a safety net against API leakage
	for _, ns := range namedStats {
		clipDailyContributionsToRange(ns.Stats, from, to)
	}

	// 3. Save snapshot to history
	snapshotDate := today
	if mode == "bootstrap" {
		removeSnapshotsForYear(history, currentYear)
	} else if mode == "recalculate" {
		removeSnapshotsForYear(history, targetYear)
		snapshotDate = to.Format("2006-01-02")
	}
	logger.WithFields(logger.Fields{
		"date": snapshotDate,
		"mode": mode,
	}).Info("saving snapshot to history")
	addSnapshot(history, snapshotDate, namedStats)
	if err := saveStatsHistory(history, historyPath); err != nil {
		logger.WithFields(logger.Fields{
			"path":  historyPath,
			"error": err.Error(),
		}).Fatal("failed to save stats history")
	}
	logger.WithFields(logger.Fields{
		"date": snapshotDate,
		"path": historyPath,
	}).Info("saved snapshot to history")

	// 4. Build per-year accumulated data
	logger.Info("accumulating per-year stats from history")
	yearlyStats := accumulateByYear(history)
	var years []int
	for y := range yearlyStats {
		years = append(years, y)
	}
	sort.Ints(years)

	// 5. Generate per-year SVGs
	svgStart := time.Now()
	hadErrors := false
	for _, year := range years {
		stats := yearlyStats[year]
		suffix := fmt.Sprintf("_%d.svg", year)

		// Log per-platform stats entering SVG generation
		for _, ns := range stats {
			logger.WithFields(logger.Fields{
				"year":      year,
				"platform":  string(ns.Platform),
				"commits":   ns.Stats.TotalCommits,
				"prs":       ns.Stats.TotalPRsOrMRs,
				"issues":    ns.Stats.TotalIssuesOrWIs,
				"repos":     ns.Stats.TotalRepos,
				"languages": len(ns.Stats.Languages),
				"contribs":  len(ns.Stats.DailyContributions),
			}).Info("generating SVGs for year/platform")
		}

		// Combined stats
		if err := GenerateCombinedStatsSVG(stats, filepath.Join(outputDir, "combined_stats"+suffix)); err != nil {
			logger.WithFields(logger.Fields{
				"year":  year,
				"chart": "combined_stats",
				"error": err.Error(),
			}).Error("failed to generate SVG")
			hadErrors = true
		}

		// Languages: delta-based from accumulated history snapshots
		langsByPlatform := aggregateLanguagesByPlatform(stats)
		if err := GenerateLanguagesBarChart(langsByPlatform, filepath.Join(outputDir, "top_languages"+suffix)); err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "no language data") || strings.Contains(errMsg, "all language byte counts are zero") {
				// Expected "no data" condition: warn and continue without marking a hard error.
				logger.WithFields(logger.Fields{
					"year":  year,
					"chart": "top_languages",
					"error": err.Error(),
				}).Warn("skipping chart due to missing data")
			} else {
				// Unexpected failure (e.g., file I/O, template, logic): treat as an error.
				logger.WithFields(logger.Fields{
					"year":  year,
					"chart": "top_languages",
					"error": err.Error(),
				}).Error("failed to generate SVG")
				hadErrors = true
			}
		}

		// Contribution heatmap
		contribsByPlatform := aggregateContributionsByPlatform(stats)
		var startDate, endDate time.Time
		startDate = time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate = time.Date(year, 12, 31, 23, 59, 59, 0, time.UTC)
		if err := GenerateContributionHeatmap(contribsByPlatform, startDate, endDate, filepath.Join(outputDir, "contributions"+suffix)); err != nil {
			logger.WithFields(logger.Fields{
				"year":  year,
				"chart": "contributions",
				"error": err.Error(),
			}).Error("failed to generate SVG")
			hadErrors = true
		}
	}

	// 6. Copy current year SVGs to _final.svg for backward compatibility
	logger.WithFields(logger.Fields{
		"year": currentYear,
	}).Info("copying current year SVGs to backward-compatibility _final files")
	for _, base := range []string{"combined_stats", "top_languages", "contributions"} {
		src := filepath.Join(outputDir, fmt.Sprintf("%s_%d.svg", base, currentYear))
		dst := filepath.Join(outputDir, base+"_final.svg")

		data, err := os.ReadFile(src)
		if err != nil {
			logger.WithFields(logger.Fields{
				"source": src,
				"error":  err.Error(),
			}).Warn("could not read SVG for backward-compatibility copy")
			continue
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			logger.WithFields(logger.Fields{
				"destination": dst,
				"error":       err.Error(),
			}).Error("failed to write backward-compatibility file")
			hadErrors = true
		}
	}

	logger.WithFields(logger.Fields{
		"elapsed": time.Since(svgStart).String(),
		"years":   len(years),
	}).Info("SVG generation completed")

	// 7. Generate Claude Code tokens heatmap (not year-based)
	tokenData, err := loadTokenUsage("claude_tokens.json")
	if err != nil {
		logger.WithFields(logger.Fields{
			"error": err.Error(),
		}).Warn("could not load Claude Code token data, generating empty graph")
		tokenData = []TokenUsage{}
	}
	logger.Info("generating Claude Code tokens graph")
	if len(tokenData) == 0 {
		// Generate empty placeholder SVG
		emptySvg := `<svg xmlns="http://www.w3.org/2000/svg" width="893" height="207" viewBox="0 0 893 207">
<style>.title { font: 600 14px 'Segoe UI', Ubuntu, Sans-Serif; fill: #fff; } .empty { font: 400 12px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }</style>
<rect width="893" height="207" rx="4.5" fill="#151515" stroke="#e4e2e2" stroke-opacity="0.2"/>
<text x="25" y="25" class="title">Claude Code Tokens (by day)</text>
<text x="446" y="120" text-anchor="middle" class="empty">No token data available</text>
</svg>`
		os.WriteFile(filepath.Join(outputDir, "claude_tokens_final.svg"), []byte(emptySvg), 0644)
	} else {
		if err = GenerateTokensHeatmap(tokenData, filepath.Join(outputDir, "claude_tokens_final.svg")); err != nil {
			logger.WithFields(logger.Fields{
				"error": err.Error(),
			}).Error("failed to generate tokens graph")
		}
	}

	if hadErrors {
		logger.Fatal("SVG generation completed with errors")
	}
	logger.Info("all SVGs generated successfully")
}
