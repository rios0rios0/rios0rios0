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
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
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
		return [4]string{"#0e4429", "#006d32", "#26a641", "#39d353"}
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
	comboGitHub:                        {"#0e4429", "#006d32", "#26a641", "#39d353"},
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

func accumulateByYear(history *StatsHistory) map[int][]NamedPlatformStats {
	type accumEntry struct {
		maxCommits    int
		maxPRs        int
		maxIssues     int
		maxRepos      int
		contributions map[string]int    // date -> max count
		earliestDate  string            // earliest snapshot date in this year
		earliestLangs map[string]int64  // language bytes from earliest snapshot
		latestDate    string            // latest snapshot date in this year
		latestLangs   map[string]int64  // language bytes from latest snapshot
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
						earliestLangs: make(map[string]int64),
						latestLangs:   make(map[string]int64),
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
				// Track earliest and latest language snapshots for delta calculation
				if entry.earliestDate == "" || snap.Date < entry.earliestDate {
					entry.earliestDate = snap.Date
					entry.earliestLangs = make(map[string]int64)
					for lang, bytes := range ps.Languages {
						entry.earliestLangs[lang] = bytes
					}
				}
				if snap.Date > entry.latestDate {
					entry.latestDate = snap.Date
					entry.latestLangs = make(map[string]int64)
					for lang, bytes := range ps.Languages {
						entry.latestLangs[lang] = bytes
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
						earliestLangs: make(map[string]int64),
						latestLangs:   make(map[string]int64),
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

			// Compute language delta: latest - earliest for each language
			langs := make(map[string]int64)
			if entry.earliestDate == entry.latestDate {
				// Single snapshot: use absolute values (first run)
				for lang, bytes := range entry.latestLangs {
					if bytes > 0 {
						langs[lang] = bytes
					}
				}
			} else {
				// Multiple snapshots: compute delta
				for lang, latestBytes := range entry.latestLangs {
					delta := latestBytes - entry.earliestLangs[lang]
					if delta > 0 {
						langs[lang] = delta
					}
				}
				// Include new languages that only appear in latest
				for lang, latestBytes := range entry.latestLangs {
					if _, exists := entry.earliestLangs[lang]; !exists && latestBytes > 0 {
						langs[lang] = latestBytes
					}
				}
			}

			result[year] = append(result[year], NamedPlatformStats{
				Platform: p,
				Stats: &PlatformStats{
					TotalCommits:       entry.maxCommits,
					TotalPRsOrMRs:      entry.maxPRs,
					TotalIssuesOrWIs:   entry.maxIssues,
					TotalRepos:         entry.maxRepos,
					Languages:          langs,
					DailyContributions: entry.contributions,
				},
			})
		}
	}
	return result
}

// --- GitHub ---

func FetchGitHubStats(username, token string) (*PlatformStats, error) {
	stats := &PlatformStats{
		Languages:          make(map[string]int64),
		DailyContributions: make(map[string]int),
	}

	// Use GraphQL API for contributions + repos committed to
	query := fmt.Sprintf(`{
		"query": "query { user(login: \"%s\") { contributionsCollection { totalCommitContributions totalPullRequestContributions totalIssueContributions contributionCalendar { weeks { contributionDays { date contributionCount } } } commitContributionsByRepository(maxRepositories: 100) { contributions { totalCount } repository { name owner { login } isPrivate } } } repositories(first: 100, ownerAffiliations: OWNER, isFork: false, privacy: PUBLIC) { totalCount } } }"
	}`, username)

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

	for _, week := range cc.ContributionCalendar.Weeks {
		for _, day := range week.ContributionDays {
			if day.ContributionCount > 0 {
				stats.DailyContributions[day.Date] = day.ContributionCount
			}
		}
	}

	// Fetch languages weighted by commit activity in the contribution period.
	// commitContributionsByRepository gives repos the user actually committed to,
	// so we weight each repo's language bytes by its share of total commits.
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
		fmt.Printf("Warning: could not fetch GitHub languages: %v\n", err)
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

func FetchGitLabStats(username, accessToken string) (*PlatformStats, error) {
	stats := &PlatformStats{
		Languages:          make(map[string]int64),
		DailyContributions: make(map[string]int),
	}

	client := &http.Client{}

	// Fetch user ID
	userURL := fmt.Sprintf("https://gitlab.com/api/v4/users?username=%s", username)
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

	// Fetch events for the last year
	now := time.Now()
	oneYearAgo := now.AddDate(-1, 0, 0)
	page := 1

	for {
		eventsURL := fmt.Sprintf("https://gitlab.com/api/v4/users/%d/events?after=%s&before=%s&page=%d&per_page=100",
			userID, oneYearAgo.Format("2006-01-02"), now.Format("2006-01-02"), page)

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

	// Fetch languages only from projects with recent activity
	if err = fetchGitLabLanguages(client, userID, accessToken, oneYearAgo, stats); err != nil {
		fmt.Printf("Warning: could not fetch GitLab languages: %v\n", err)
	}

	return stats, nil
}

func fetchGitLabLanguages(client *http.Client, userID int, accessToken string, since time.Time, stats *PlatformStats) error {
	page := 1
	for {
		projectsURL := fmt.Sprintf("https://gitlab.com/api/v4/users/%d/projects?per_page=100&page=%d&owned=true&order_by=last_activity_at&sort=desc", userID, page)
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

			// GitLab returns percentages, convert to pseudo-bytes (multiply by 100 to keep precision)
			var langs map[string]float64
			if err = json.Unmarshal(langBody, &langs); err != nil {
				continue
			}
			for lang, pct := range langs {
				stats.Languages[lang] += int64(pct * 100)
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

func FetchAzureDevOpsStats(organization, accessToken string) (*PlatformStats, error) {
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

	now := time.Now()
	oneYearAgo := now.AddDate(-1, 0, 0)
	fromDate := oneYearAgo.Format("2006-01-02T15:04:05Z")

	// Get all projects
	var projects []adoProject

	continuationToken := ""
	for {
		projectsURL := fmt.Sprintf("https://dev.azure.com/%s/_apis/projects?api-version=7.0&$top=100", url.PathEscape(organization))
		if continuationToken != "" {
			projectsURL += "&continuationToken=" + url.QueryEscape(continuationToken)
		}

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

		stats.TotalRepos += len(reposResult.Value)

		// Count commits with dates
		for _, repo := range reposResult.Value {
			repoHadCommits := false
			skip := 0
			for {
				commitsURL := fmt.Sprintf(
					"https://dev.azure.com/%s/%s/_apis/git/repositories/%s/commits?searchCriteria.author=%s&searchCriteria.fromDate=%s&$top=100&$skip=%d&api-version=7.0",
					url.PathEscape(organization), url.PathEscape(proj.ID), url.PathEscape(repo.ID),
					url.QueryEscape(displayName), url.QueryEscape(fromDate), skip,
				)
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
				activeRepos = append(activeRepos, adoRepoRef{ProjectID: proj.ID, RepoID: repo.ID})
			}
		}

		// Count PRs
		skip := 0
		for {
			prsURL := fmt.Sprintf(
				"https://dev.azure.com/%s/%s/_apis/git/pullrequests?searchCriteria.creatorId=%s&searchCriteria.status=all&$top=100&$skip=%d&api-version=7.0",
				url.PathEscape(organization), url.PathEscape(proj.ID), url.PathEscape(userID), skip,
			)
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
				if prDate.After(oneYearAgo) {
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

	// Fetch languages only from repos the user committed to
	fetchAzureDevOpsLanguages(newRequest, doRequest, organization, activeRepos, stats)

	// Count work items
	wiqlURL := fmt.Sprintf("https://dev.azure.com/%s/_apis/wit/wiql?$top=20000&api-version=7.0", url.PathEscape(organization))
	wiqlQuery := fmt.Sprintf(
		`{"query": "SELECT [System.Id] FROM WorkItems WHERE [System.AssignedTo] = @Me AND [System.CreatedDate] >= '%s'"}`,
		oneYearAgo.Format("2006-01-02"),
	)
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

type adoRepoRef struct {
	ProjectID string
	RepoID    string
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

		// Fetch the file tree from the default branch with pagination
		baseItemsURL := fmt.Sprintf(
			"https://dev.azure.com/%s/%s/_apis/git/repositories/%s/items?recursionLevel=Full&versionDescriptor.version=%s&versionDescriptor.versionType=branch&api-version=7.0",
			url.PathEscape(organization), url.PathEscape(ref.ProjectID), url.PathEscape(ref.RepoID),
			url.QueryEscape(branch),
		)

			continuationToken := ""
			for {
				itemsURL := baseItemsURL
				if continuationToken != "" {
					itemsURL += "&continuationToken=" + url.QueryEscape(continuationToken)
				}

				req, err := newRequest("GET", itemsURL, nil)
				if err != nil {
					break
				}

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					break
				}

				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil || resp.StatusCode != http.StatusOK {
					break
				}

				var itemsResult struct {
					Value []struct {
						Path     string `json:"path"`
						IsFolder bool   `json:"isFolder"`
					} `json:"value"`
				}
				if err = json.Unmarshal(body, &itemsResult); err != nil {
					break
				}

				for _, item := range itemsResult.Value {
					if item.IsFolder {
						continue
					}
					ext := strings.ToLower(filepath.Ext(item.Path))
					if lang, ok := extensionToLanguage[ext]; ok {
						stats.Languages[lang]++
					}
				}

				continuationToken = resp.Header.Get("x-ms-continuationtoken")
				if continuationToken == "" {
					break
				}
			}
	}
}

// --- SVG Generators ---

func computeStreak(contributions map[string]int) int {
	var dates []string
	for d, c := range contributions {
		if c > 0 {
			dates = append(dates, d)
		}
	}
	sort.Strings(dates)

	maxStreak := 0
	currentStreak := 0
	var prevDate time.Time

	for _, d := range dates {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			continue
		}
		if !prevDate.IsZero() && t.Sub(prevDate) == 24*time.Hour {
			currentStreak++
		} else {
			currentStreak = 1
		}
		if currentStreak > maxStreak {
			maxStreak = currentStreak
		}
		prevDate = t
	}
	return maxStreak
}

func renderCombinedStatsSVG(platformStats []NamedPlatformStats) string {
	printer := message.NewPrinter(language.English)

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

	var totalBytes int64
	mergedContribs := make(map[string]int)

	for _, ns := range platformStats {
		totalCommits += ns.Stats.TotalCommits
		totalPRs += ns.Stats.TotalPRsOrMRs
		totalIssues += ns.Stats.TotalIssuesOrWIs
		totalRepos += ns.Stats.TotalRepos
		commitVals[ns.Platform] += int64(ns.Stats.TotalCommits)
		prVals[ns.Platform] += int64(ns.Stats.TotalPRsOrMRs)
		issueVals[ns.Platform] += int64(ns.Stats.TotalIssuesOrWIs)
		repoVals[ns.Platform] += int64(ns.Stats.TotalRepos)
		for _, bytes := range ns.Stats.Languages {
			totalBytes += bytes
		}
		for date, count := range ns.Stats.DailyContributions {
			mergedContribs[date] += count
		}
	}
	// Estimate LoC only from platforms with real byte counts (GitHub only).
	// GitLab stores language percentages scaled by 100, not actual bytes.
	// Azure DevOps stores file counts, not bytes.
	const bytesPerLine = 40
	var realBytes int64
	locVals := make(map[PlatformName]int64)
	for _, ns := range platformStats {
		if ns.Platform == PlatformGitLab || ns.Platform == PlatformAzureDevOps {
			continue
		}
		var platBytes int64
		for _, bytes := range ns.Stats.Languages {
			platBytes += bytes
		}
		realBytes += platBytes
		locVals[ns.Platform] = platBytes / bytesPerLine
	}
	linesOfCode := int(realBytes / bytesPerLine)

	streak := computeStreak(mergedContribs)
	streakVals := make(map[PlatformName]int64)
	for _, ns := range platformStats {
		platContribs := make(map[string]int)
		for d, c := range ns.Stats.DailyContributions {
			if c > 0 {
				platContribs[d] = c
			}
		}
		platStreak := computeStreak(platContribs)
		if platStreak > 0 {
			streakVals[ns.Platform] = int64(platStreak)
		}
	}

	iconCommits := `<path fill-rule="evenodd" d="M1.643 3.143L.427 1.927A.25.25 0 000 2.104V5.75c0 .138.112.25.25.25h3.646a.25.25 0 00.177-.427L2.715 4.215a6.5 6.5 0 11-1.18 4.458.75.75 0 10-1.493.154 8.001 8.001 0 101.6-5.684zM7.75 4a.75.75 0 01.75.75v2.992l2.028.812a.75.75 0 01-.557 1.392l-2.5-1A.75.75 0 017 8.25v-3.5A.75.75 0 017.75 4z"/>`
	iconPRs := `<path fill-rule="evenodd" d="M7.177 3.073L9.573.677A.25.25 0 0110 .854v4.792a.25.25 0 01-.427.177L7.177 3.427a.25.25 0 010-.354zM3.75 2.5a.75.75 0 100 1.5.75.75 0 000-1.5zm-2.25.75a2.25 2.25 0 113 2.122v5.256a2.251 2.251 0 11-1.5 0V5.372A2.25 2.25 0 011.5 3.25zM11 2.5h-1V4h1a1 1 0 011 1v5.628a2.251 2.251 0 101.5 0V5A2.5 2.5 0 0011 2.5zm1 10.25a.75.75 0 111.5 0 .75.75 0 01-1.5 0zM3.75 12a.75.75 0 100 1.5.75.75 0 000-1.5z"/>`
	iconIssues := `<path fill-rule="evenodd" d="M8 1.5a6.5 6.5 0 100 13 6.5 6.5 0 000-13zM0 8a8 8 0 1116 0A8 8 0 010 8zm9 3a1 1 0 11-2 0 1 1 0 012 0zm-.25-6.25a.75.75 0 00-1.5 0v3.5a.75.75 0 001.5 0v-3.5z"/>`
	iconRepos := `<path fill-rule="evenodd" d="M2 2.5A2.5 2.5 0 014.5 0h8.75a.75.75 0 01.75.75v12.5a.75.75 0 01-.75.75h-2.5a.75.75 0 110-1.5h1.75v-2h-8a1 1 0 00-1 1v.17a2.5 2.5 0 01-.286-.958A2.495 2.495 0 012 11.5v-9zm10.5-1h-8a1 1 0 00-1 1v6.708A2.486 2.486 0 014.5 9h8V1.5z"/>`
	iconCode := `<path fill-rule="evenodd" d="M4.72 3.22a.75.75 0 011.06 1.06L2.06 8l3.72 3.72a.75.75 0 11-1.06 1.06L.47 8.53a.75.75 0 010-1.06l4.25-4.25zm6.56 0a.75.75 0 10-1.06 1.06L13.94 8l-3.72 3.72a.75.75 0 101.06 1.06l4.25-4.25a.75.75 0 000-1.06l-4.25-4.25z"/>`
	iconStreak := `<path fill-rule="evenodd" d="M7.998.002C5.026.002 2.975 2.1 2.31 3.548c-.333.723-.522 1.477-.522 2.087 0 1.236.755 2.26 1.756 2.943.39.267.833.49 1.272.658-.122.1-.242.21-.355.33A3.51 3.51 0 003.5 11.5a3.5 3.5 0 007 0c0-.96-.39-1.83-1.02-2.46a5.844 5.844 0 00-.397-.37c.466-.182.937-.425 1.346-.71C11.4 7.233 12.13 6.201 12.13 4.97c0-.61-.19-1.364-.522-2.087C10.942 1.434 8.888-.664 7.998.002zM7.5 12a2 2 0 01-2-2c0-.537.12-.976.373-1.393.247-.408.622-.786 1.127-1.107.505.32.88.699 1.127 1.107.254.417.373.856.373 1.393a2 2 0 01-2 2z"/>`

	rows := []statRow{
		{"Total Commits", iconCommits, commitVals, totalCommits},
		{"Total PRs / MRs", iconPRs, prVals, totalPRs},
		{"Total Issues / Work Items", iconIssues, issueVals, totalIssues},
		{"Total Repositories", iconRepos, repoVals, totalRepos},
		{"Lines of Code", iconCode, locVals, linesOfCode},
		{"Longest Streak (days)", iconStreak, streakVals, streak},
	}

	barAreaX := 260
	barAreaW := 140
	valueX := 455

	var body string
	for i, row := range rows {
		yOffset := i * 30
		delay := 450 + i*150

		// Icon
		body += fmt.Sprintf(`<g class="stagger" style="animation-delay: %dms" transform="translate(25, %d)">`, delay, yOffset)
		body += fmt.Sprintf(`<svg data-testid="icon" class="icon" viewBox="0 0 16 16" version="1.1" width="16" height="16">%s</svg>`, row.Icon)
		body += fmt.Sprintf(`<text class="stat bold" x="25" y="12.5">%s</text>`, row.Label)

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
		body += fmt.Sprintf(`<text class="stat bold" x="%d" y="12.5" text-anchor="end" data-testid="value">%s</text>`, valueX, printer.Sprintf("%d", row.Total))
		body += `</g>`
	}

	legend := renderPlatformLegend(25, 0)

	svgHeight := 350
	titleY := 35
	bodyY := 55
	legendY := 280

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="495" height="%d" viewBox="0 0 495 %d" fill="none" role="img">
<title>Combined Stats</title>
<style>
	.header { font: 600 14px 'Segoe UI', Ubuntu, Sans-Serif; fill: #fff; animation: fadeInAnimation 0.8s ease-in-out forwards; }
	.stat { font: 600 14px 'Segoe UI', Ubuntu, Sans-Serif; fill: #9f9f9f; }
	.bold { font-weight: 700 }
	.icon { fill: #79ff97; display: block; }
	.legend-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
	.stagger { opacity: 0; animation: fadeInAnimation 0.3s ease-in-out forwards; }
	@keyframes fadeInAnimation { from { opacity: 0; } to { opacity: 1; } }
</style>
<rect data-testid="card-bg" x="0.5" y="0.5" rx="4.5" height="99%%" width="494" fill="#151515" stroke="#e4e2e2" stroke-opacity="0.2"/>
<g data-testid="card-title" transform="translate(25, %d)">
	<text x="0" y="0" class="header" data-testid="header">Stats (across all platforms)</text>
</g>
<g data-testid="main-card-body" transform="translate(0, %d)">
	<svg x="0" y="0">%s</svg>
</g>
<g transform="translate(0, %d)">%s</g>
</svg>`, svgHeight, svgHeight, titleY, bodyY, body, legendY, legend)
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
	padLeft := 35
	padTop := 50
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
	width := padLeft + displayWeeks*(cellSize+cellGap) + 10
	height := padTop + 7*(cellSize+cellGap) + padBottom + legendHeight

	var cells string
	dayLabels := []string{"", "Mon", "", "Wed", "", "Fri", ""}
	for i, label := range dayLabels {
		if label != "" {
			y := padTop + i*(cellSize+cellGap) + cellSize - 2
			cells += fmt.Sprintf(`<text x="8" y="%d" class="day-label">%s</text>`, y, label)
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
	dx := padLeft
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
<text x="20" y="20" class="title">Claude Code Tokens (by day)</text>
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
	if len(entries) > 10 {
		entries = entries[:10]
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

	width := 495
	barHeight := 22
	barGap := 6
	padLeft := 110
	padRight := 70
	padTop := 35
	graphW := width - padLeft - padRight
	legendHeight := 25
	height := padTop + len(entries)*(barHeight+barGap) + legendHeight + 10
	if height < 350 {
		height = 350
	}

	maxBytes := entries[0].Total

	var bars string
	for i, e := range entries {
		y := padTop + i*(barHeight+barGap)
		totalBarW := int(float64(e.Total) / float64(maxBytes) * float64(graphW))
		if totalBarW < 2 {
			totalBarW = 2
		}
		pct := float64(e.Total) / float64(grandTotal) * 100

		bars += fmt.Sprintf(`<text x="%d" y="%d" class="lang-label" text-anchor="end">%s</text>`, padLeft-8, y+15, e.Name)

		// Stacked bar segments by platform
		bx := padLeft
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
			bars += fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" rx="2" fill="%s" class="bar"><title>%s: %d bytes</title></rect>`, bx, y, segW, barHeight, p.Color(), string(p), v)
			bx += segW
			remaining -= segW
			nonZero--
		}

		bars += fmt.Sprintf(`<text x="%d" y="%d" class="pct-label">%.1f%%</text>`, padLeft+totalBarW+6, y+15, pct)
	}

	// Platform legend
	legendY := padTop + len(entries)*(barHeight+barGap) + 5
	var legend string
	dx := padLeft
	for _, p := range platformOrder {
		legend += fmt.Sprintf(`<rect x="%d" y="%d" width="10" height="10" rx="2" fill="%s"/>`, dx, legendY, p.Color())
		legend += fmt.Sprintf(`<text x="%d" y="%d" class="legend-label">%s</text>`, dx+14, legendY+9, string(p))
		dx += 14 + len(string(p))*7 + 12
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
<style>
	.title { font: 600 14px 'Segoe UI', Ubuntu, Sans-Serif; fill: #fff; }
	.lang-label { font: 400 12px 'Segoe UI', Ubuntu, Sans-Serif; fill: #c9d1d9; }
	.pct-label { font: 400 11px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
	.legend-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
	.bar { opacity: 0; animation: barGrow 0.5s ease-out forwards; }
	@keyframes barGrow { from { opacity: 0; width: 0; } to { opacity: 1; } }
</style>
<rect width="%d" height="%d" rx="4.5" fill="#151515" stroke="#e4e2e2" stroke-opacity="0.2"/>
<text x="20" y="22" class="title">Top Languages (across all platforms)</text>
%s
%s
</svg>`, width, height, width, height, width, height, bars, legend)

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
	padLeft := 35
	padTop := 50
	padBottom := 20
	legendHeight := 20

	// For a full calendar-year view (Jan 1 start), do not rewind to the previous
	// Sunday as that would include days from the previous year.
	isCalendarYear := startDate.Day() == 1 && startDate.Month() == time.January
	if !isCalendarYear {
		for startDate.Weekday() != time.Sunday {
			startDate = startDate.AddDate(0, 0, -1)
		}
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
	width := padLeft + displayWeeks*(cellSize+cellGap) + 10
	height := padTop + 7*(cellSize+cellGap) + padBottom + legendHeight

	var cells string
	dayLabels := []string{"", "Mon", "", "Wed", "", "Fri", ""}
	for i, label := range dayLabels {
		if label != "" {
			y := padTop + i*(cellSize+cellGap) + cellSize - 2
			cells += fmt.Sprintf(`<text x="8" y="%d" class="day-label">%s</text>`, y, label)
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
	dx := padLeft
	row := 0
	maxDx := width - 20
	for _, combo := range comboOrder {
		if !activeCombos[combo] {
			continue
		}
		label := comboLabels[combo]
		scale := comboColorScale(combo)
		entryWidth := 14 + len(label)*6 + 12
		if dx+entryWidth > maxDx && dx > padLeft {
			row++
			dx = padLeft
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
<text x="20" y="20" class="title">Contributions (across all platforms)</text>
%s
</svg>`, width, height, width, height, width, height, cells)
}

func GenerateContributionHeatmap(contributions map[string]map[PlatformName]int, startDate, endDate time.Time, outputPath string) error {
	svg := renderContributionHeatmap(contributions, startDate, endDate)
	return os.WriteFile(outputPath, []byte(svg), 0644)
}

// --- Helpers ---

func formatNumber(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func aggregateLanguagesByPlatform(named []NamedPlatformStats) map[string]map[PlatformName]int64 {
	result := make(map[string]map[PlatformName]int64)
	for _, ns := range named {
		for lang, bytes := range ns.Stats.Languages {
			if result[lang] == nil {
				result[lang] = make(map[PlatformName]int64)
			}
			result[lang][ns.Platform] += bytes
		}
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
	now := time.Now()
	today := now.Format("2006-01-02")
	currentYear := now.Year()

	historyPath := getEnvOrDefault("STATS_HISTORY_PATH", "stats_history.json")
	outputDir := getEnvOrDefault("SVG_OUTPUT_DIR", ".")

	// 1. Load existing history
	history, err := loadStatsHistory(historyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load history from %s: %v\n", historyPath, err)
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

	ghUsername := os.Getenv("GITHUB_USERNAME")
	ghToken := os.Getenv("GH_TOKEN")
	if ghUsername != "" && ghToken != "" {
		fetchers++
		go func() {
			fmt.Println("Fetching GitHub stats...")
			stats, err := FetchGitHubStats(ghUsername, ghToken)
			resultCh <- fetchResult{PlatformGitHub, stats, err}
		}()
	}

	glUsername := os.Getenv("GITLAB_USERNAME")
	glToken := os.Getenv("GITLAB_ACCESS_TOKEN")
	if glUsername != "" && glToken != "" {
		fetchers++
		go func() {
			fmt.Println("Fetching GitLab stats...")
			stats, err := FetchGitLabStats(glUsername, glToken)
			resultCh <- fetchResult{PlatformGitLab, stats, err}
		}()
	}

	adoOrg := os.Getenv("AZURE_DEVOPS_ORG")
	adoToken := os.Getenv("AZURE_DEVOPS_ACCESS_TOKEN")
	if adoOrg != "" && adoToken != "" {
		fetchers++
		go func() {
			fmt.Println("Fetching Azure DevOps stats...")
			stats, err := FetchAzureDevOpsStats(adoOrg, adoToken)
			resultCh <- fetchResult{PlatformAzureDevOps, stats, err}
		}()
	}

	var namedStats []NamedPlatformStats
	for i := 0; i < fetchers; i++ {
		r := <-resultCh
		if r.Err != nil {
			fmt.Printf("Warning: skipping %s — %v\n", r.Platform, r.Err)
			continue
		}
		switch r.Platform {
		case PlatformGitHub:
			fmt.Printf("GitHub: %d commits, %d PRs, %d issues\n", r.Stats.TotalCommits, r.Stats.TotalPRsOrMRs, r.Stats.TotalIssuesOrWIs)
		case PlatformGitLab:
			fmt.Printf("GitLab: %d commits, %d MRs, %d issues\n", r.Stats.TotalCommits, r.Stats.TotalPRsOrMRs, r.Stats.TotalIssuesOrWIs)
		case PlatformAzureDevOps:
			fmt.Printf("Azure DevOps: %d commits, %d PRs, %d work items\n", r.Stats.TotalCommits, r.Stats.TotalPRsOrMRs, r.Stats.TotalIssuesOrWIs)
		}
		namedStats = append(namedStats, NamedPlatformStats{r.Platform, r.Stats})
	}

	if len(namedStats) == 0 {
		fmt.Println("No platform credentials configured.")
		fmt.Println("Set GITHUB_USERNAME/GH_TOKEN, GITLAB_USERNAME/GITLAB_ACCESS_TOKEN, or AZURE_DEVOPS_ORG/AZURE_DEVOPS_ACCESS_TOKEN")
		os.Exit(1)
	}

	// 3. Save today's snapshot to history
	addSnapshot(history, today, namedStats)
	if err := saveStatsHistory(history, historyPath); err != nil {
		fmt.Printf("Error saving history: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Saved snapshot for %s to %s\n", today, historyPath)

	// 4. Build per-year accumulated data
	yearlyStats := accumulateByYear(history)
	var years []int
	for y := range yearlyStats {
		years = append(years, y)
	}
	sort.Ints(years)

	// 5. Generate per-year SVGs
	hadErrors := false
	for _, year := range years {
		stats := yearlyStats[year]
		suffix := fmt.Sprintf("_%d.svg", year)

		fmt.Printf("Generating SVGs for %d...\n", year)

		// Combined stats
		if err := GenerateCombinedStatsSVG(stats, filepath.Join(outputDir, "combined_stats"+suffix)); err != nil {
			fmt.Printf("Error generating combined stats SVG for %d: %v\n", year, err)
			hadErrors = true
		}

		// Languages: delta-based from accumulated history snapshots
		langsByPlatform := aggregateLanguagesByPlatform(stats)
		if err := GenerateLanguagesBarChart(langsByPlatform, filepath.Join(outputDir, "top_languages"+suffix)); err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "no language data") || strings.Contains(errMsg, "all language byte counts are zero") {
				// Expected "no data" condition: warn and continue without marking a hard error.
				fmt.Printf("Warning: skipping languages chart for %d: %v\n", year, err)
			} else {
				// Unexpected failure (e.g., file I/O, template, logic): treat as an error.
				fmt.Printf("Error generating languages chart for %d: %v\n", year, err)
				hadErrors = true
			}
		}

		// Contribution heatmap
		contribsByPlatform := aggregateContributionsByPlatform(stats)
		var startDate, endDate time.Time
		startDate = time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate = time.Date(year, 12, 31, 23, 59, 59, 0, time.UTC)
		if err := GenerateContributionHeatmap(contribsByPlatform, startDate, endDate, filepath.Join(outputDir, "contributions"+suffix)); err != nil {
			fmt.Printf("Error generating contribution heatmap for %d: %v\n", year, err)
			hadErrors = true
		}
	}

	// 6. Copy current year SVGs to _final.svg for backward compatibility
	for _, base := range []string{"combined_stats", "top_languages", "contributions"} {
		src := filepath.Join(outputDir, fmt.Sprintf("%s_%d.svg", base, currentYear))
		dst := filepath.Join(outputDir, base+"_final.svg")

		data, err := os.ReadFile(src)
		if err != nil {
			fmt.Printf("Warning: could not read %s for backward-compatibility copy: %v\n", src, err)
			continue
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			fmt.Printf("Error writing backward-compatibility file %s: %v\n", dst, err)
			hadErrors = true
		}
	}

	// 7. Generate Claude Code tokens line graph (not year-based)
	tokenData, err := loadTokenUsage("claude_tokens.json")
	if err != nil {
		fmt.Printf("Warning: could not load Claude Code token data: %v (skipping tokens graph)\n", err)
	} else {
		fmt.Println("Generating Claude Code tokens graph...")
		if err = GenerateTokensHeatmap(tokenData, filepath.Join(outputDir, "claude_tokens_final.svg")); err != nil {
			fmt.Printf("Error generating tokens graph: %v\n", err)
		}
	}

	if hadErrors {
		fmt.Println("SVG generation completed with errors.")
		os.Exit(1)
	}
	fmt.Println("All SVGs generated successfully!")
}
