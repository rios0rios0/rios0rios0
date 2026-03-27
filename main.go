package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// PlatformStats holds stats from a single platform
type PlatformStats struct {
	TotalCommits      int
	TotalPRsOrMRs     int
	TotalIssuesOrWIs  int
	Languages         map[string]int64         // language -> bytes
	DailyContributions map[string]int           // "2025-03-20" -> count
}

// TokenUsage represents daily Claude Code token usage
type TokenUsage struct {
	Date   string `json:"date"`
	Tokens int    `json:"tokens"`
}

// --- GitHub ---

func FetchGitHubStats(username, token string) (*PlatformStats, error) {
	stats := &PlatformStats{
		Languages:          make(map[string]int64),
		DailyContributions: make(map[string]int),
	}

	// Use GraphQL API for contributions
	query := fmt.Sprintf(`{
		"query": "query { user(login: \"%s\") { contributionsCollection { totalCommitContributions totalPullRequestContributions totalIssueContributions contributionCalendar { weeks { contributionDays { date contributionCount } } } } } }"
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
				} `json:"contributionsCollection"`
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

	for _, week := range cc.ContributionCalendar.Weeks {
		for _, day := range week.ContributionDays {
			if day.ContributionCount > 0 {
				stats.DailyContributions[day.Date] = day.ContributionCount
			}
		}
	}

	// Fetch languages from repos
	if err = fetchGitHubLanguages(client, username, token, stats); err != nil {
		fmt.Printf("Warning: could not fetch GitHub languages: %v\n", err)
	}

	return stats, nil
}

func fetchGitHubLanguages(client *http.Client, username, token string, stats *PlatformStats) error {
	page := 1
	for {
		reposURL := fmt.Sprintf("https://api.github.com/users/%s/repos?per_page=100&page=%d&type=owner", username, page)
		req, err := http.NewRequest("GET", reposURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			return err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		var repos []struct {
			Name     string `json:"name"`
			Fork     bool   `json:"fork"`
			Language string `json:"language"`
		}
		if err = json.Unmarshal(body, &repos); err != nil {
			return err
		}

		if len(repos) == 0 {
			break
		}

		for _, repo := range repos {
			if repo.Fork {
				continue
			}
			langURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/languages", username, repo.Name)
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

			var langs map[string]int64
			if err = json.Unmarshal(langBody, &langs); err != nil {
				continue
			}
			for lang, byteCount := range langs {
				stats.Languages[lang] += byteCount
			}
		}

		if len(repos) < 100 {
			break
		}
		page++
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

	// Fetch languages from user's projects
	if err = fetchGitLabLanguages(client, userID, accessToken, stats); err != nil {
		fmt.Printf("Warning: could not fetch GitLab languages: %v\n", err)
	}

	return stats, nil
}

func fetchGitLabLanguages(client *http.Client, userID int, accessToken string, stats *PlatformStats) error {
	page := 1
	for {
		projectsURL := fmt.Sprintf("https://gitlab.com/api/v4/users/%d/projects?per_page=100&page=%d&owned=true", userID, page)
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
			ID int `json:"id"`
		}
		if err = json.Unmarshal(body, &projects); err != nil {
			return err
		}
		if len(projects) == 0 {
			break
		}

		for _, proj := range projects {
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

		if len(projects) < 100 {
			break
		}
		page++
	}
	return nil
}

// --- Azure DevOps ---

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
	type project struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var projects []project

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
			Count int       `json:"count"`
			Value []project `json:"value"`
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

// --- SVG Generators ---

func renderCombinedStatsSVG(templateContent string, totalCommits, totalPRs, totalIssues, totalContributions int) string {
	printer := message.NewPrinter(language.English)
	return printer.Sprintf(templateContent,
		totalCommits,
		totalPRs,
		totalIssues,
		totalContributions,
	)
}

func GenerateCombinedStatsSVG(totalCommits, totalPRs, totalIssues, totalContributions int, outputPath string) error {
	templateContent, err := os.ReadFile("combined_stats.svg")
	if err != nil {
		return err
	}
	svgContent := renderCombinedStatsSVG(string(templateContent), totalCommits, totalPRs, totalIssues, totalContributions)
	return os.WriteFile(outputPath, []byte(svgContent), 0644)
}

func renderTokensLineGraph(tokens []TokenUsage) (string, error) {
	if len(tokens) == 0 {
		return "", fmt.Errorf("no token data")
	}

	width := 800
	height := 200
	padLeft := 70
	padRight := 20
	padTop := 35
	padBottom := 40
	graphW := width - padLeft - padRight
	graphH := height - padTop - padBottom

	// Find max tokens for scaling
	maxTokens := 0
	for _, t := range tokens {
		if t.Tokens > maxTokens {
			maxTokens = t.Tokens
		}
	}
	if maxTokens == 0 {
		maxTokens = 1
	}

	// Build path
	var pathParts []string
	var areaParts []string
	n := len(tokens)

	for i, t := range tokens {
		var x int
		if n < 2 {
			x = padLeft
		} else {
			x = padLeft + int(float64(i)/float64(n-1)*float64(graphW))
		}
		y := padTop + graphH - int(float64(t.Tokens)/float64(maxTokens)*float64(graphH))
		if i == 0 {
			pathParts = append(pathParts, fmt.Sprintf("M%d,%d", x, y))
			areaParts = append(areaParts, fmt.Sprintf("M%d,%d", x, padTop+graphH))
			areaParts = append(areaParts, fmt.Sprintf("L%d,%d", x, y))
		} else {
			pathParts = append(pathParts, fmt.Sprintf("L%d,%d", x, y))
			areaParts = append(areaParts, fmt.Sprintf("L%d,%d", x, y))
		}
	}

	// Close area path
	var lastX int
	if n < 2 {
		lastX = padLeft
	} else {
		lastX = padLeft + int(float64(n-1)/float64(n-1)*float64(graphW))
	}
	areaParts = append(areaParts, fmt.Sprintf("L%d,%d", lastX, padTop+graphH))
	areaParts = append(areaParts, "Z")

	linePath := strings.Join(pathParts, " ")
	areaPath := strings.Join(areaParts, " ")

	// Y-axis labels (5 ticks)
	var yLabels string
	for i := 0; i <= 4; i++ {
		val := maxTokens * i / 4
		y := padTop + graphH - int(float64(i)/4.0*float64(graphH))
		label := formatNumber(val)
		yLabels += fmt.Sprintf(`<text x="%d" y="%d" class="axis-label" text-anchor="end">%s</text>`, padLeft-8, y+4, label)
		yLabels += fmt.Sprintf(`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#333" stroke-width="0.5"/>`, padLeft, y, padLeft+graphW, y)
	}

	// X-axis labels (show ~6 dates evenly spaced)
	var xLabels string
	labelCount := 6
	if n < labelCount {
		labelCount = n
	}
	for i := 0; i < labelCount; i++ {
		var idx int
		if labelCount < 2 {
			idx = 0
		} else {
			idx = i * (n - 1) / (labelCount - 1)
		}
		var x int
		if n < 2 {
			x = padLeft
		} else {
			x = padLeft + int(float64(idx)/float64(n-1)*float64(graphW))
		}
		date := tokens[idx].Date
		if len(date) >= 10 {
			date = date[5:10] // MM-DD
		}
		xLabels += fmt.Sprintf(`<text x="%d" y="%d" class="axis-label" text-anchor="middle">%s</text>`, x, padTop+graphH+20, date)
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
<style>
	.title { font: 600 14px 'Segoe UI', Ubuntu, Sans-Serif; fill: #fff; }
	.axis-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
</style>
<rect width="%d" height="%d" rx="4.5" fill="#151515" stroke="#e4e2e2" stroke-opacity="0.2"/>
<text x="%d" y="22" class="title">Claude Code Tokens (by day)</text>
%s
%s
<path d="%s" fill="rgba(136,132,216,0.15)" stroke="none"/>
<path d="%s" fill="none" stroke="#8884d8" stroke-width="2"/>
</svg>`, width, height, width, height, width, height, padLeft, yLabels, xLabels, areaPath, linePath)

	return svg, nil
}

func GenerateTokensLineGraph(tokens []TokenUsage, outputPath string) error {
	svg, err := renderTokensLineGraph(tokens)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, []byte(svg), 0644)
}

func renderLanguagesBarChart(languages map[string]int64) (string, error) {
	// Sort and take top 10
	type langEntry struct {
		Name  string
		Bytes int64
	}
	var entries []langEntry
	for name, bytes := range languages {
		entries = append(entries, langEntry{name, bytes})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Bytes > entries[j].Bytes })
	if len(entries) > 10 {
		entries = entries[:10]
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("no language data")
	}

	// Calculate total for percentages
	var total int64
	for _, e := range entries {
		total += e.Bytes
	}
	if total == 0 {
		return "", fmt.Errorf("all language byte counts are zero")
	}

	width := 400
	barHeight := 22
	barGap := 6
	padLeft := 110
	padRight := 60
	padTop := 35
	graphW := width - padLeft - padRight
	height := padTop + len(entries)*(barHeight+barGap) + 10

	colors := []string{"#f1e05a", "#3572A5", "#e34c26", "#00ADD8", "#b07219", "#89e051", "#563d7c", "#178600", "#438eff", "#DA5B0B"}

	var bars string
	maxBytes := entries[0].Bytes
	for i, e := range entries {
		y := padTop + i*(barHeight+barGap)
		barW := int(float64(e.Bytes) / float64(maxBytes) * float64(graphW))
		if barW < 2 {
			barW = 2
		}
		pct := float64(e.Bytes) / float64(total) * 100
		color := colors[i%len(colors)]

		bars += fmt.Sprintf(`<text x="%d" y="%d" class="lang-label" text-anchor="end">%s</text>`, padLeft-8, y+15, e.Name)
		bars += fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" rx="3" fill="%s" class="bar"/>`, padLeft, y, barW, barHeight, color)
		bars += fmt.Sprintf(`<text x="%d" y="%d" class="pct-label">%.1f%%</text>`, padLeft+barW+6, y+15, pct)
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
<style>
	.title { font: 600 14px 'Segoe UI', Ubuntu, Sans-Serif; fill: #fff; }
	.lang-label { font: 400 12px 'Segoe UI', Ubuntu, Sans-Serif; fill: #c9d1d9; }
	.pct-label { font: 400 11px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
	.bar { opacity: 0; animation: barGrow 0.5s ease-out forwards; }
	@keyframes barGrow { from { opacity: 0; width: 0; } to { opacity: 1; } }
</style>
<rect width="%d" height="%d" rx="4.5" fill="#151515" stroke="#e4e2e2" stroke-opacity="0.2"/>
<text x="20" y="22" class="title">Top Languages (across all platforms)</text>
%s
</svg>`, width, height, width, height, width, height, bars)

	return svg, nil
}

func GenerateLanguagesBarChart(languages map[string]int64, outputPath string) error {
	svg, err := renderLanguagesBarChart(languages)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, []byte(svg), 0644)
}

func renderContributionHeatmap(contributions map[string]int, now time.Time) string {
	cellSize := 13
	cellGap := 3
	padLeft := 35
	padTop := 35
	padBottom := 20

	// Find the start: go back to the nearest Sunday, 52 weeks ago
	startDate := now.AddDate(0, 0, -364)
	for startDate.Weekday() != time.Sunday {
		startDate = startDate.AddDate(0, 0, -1)
	}

	// Collect all dates and find max
	maxCount := 1
	for _, count := range contributions {
		if count > maxCount {
			maxCount = count
		}
	}

	weeks := 53
	width := padLeft + weeks*(cellSize+cellGap) + 10
	height := padTop + 7*(cellSize+cellGap) + padBottom

	// Color scale (GitHub-like green tones)
	getColor := func(count int) string {
		if count == 0 {
			return "#161b22"
		}
		ratio := float64(count) / float64(maxCount)
		switch {
		case ratio <= 0.25:
			return "#0e4429"
		case ratio <= 0.50:
			return "#006d32"
		case ratio <= 0.75:
			return "#26a641"
		default:
			return "#39d353"
		}
	}

	var cells string
	// Day labels
	dayLabels := []string{"", "Mon", "", "Wed", "", "Fri", ""}
	for i, label := range dayLabels {
		if label != "" {
			y := padTop + i*(cellSize+cellGap) + cellSize - 2
			cells += fmt.Sprintf(`<text x="2" y="%d" class="day-label">%s</text>`, y, label)
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
			if date.After(now) {
				continue
			}
			dateStr := date.Format("2006-01-02")
			count := contributions[dateStr]
			x := padLeft + w*(cellSize+cellGap)
			y := padTop + d*(cellSize+cellGap)
			color := getColor(count)
			cells += fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" rx="2" fill="%s"><title>%s: %d contributions</title></rect>`,
				x, y, cellSize, cellSize, color, dateStr, count)
		}
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
<style>
	.title { font: 600 14px 'Segoe UI', Ubuntu, Sans-Serif; fill: #fff; }
	.day-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
	.month-label { font: 400 10px 'Segoe UI', Ubuntu, Sans-Serif; fill: #8b949e; }
</style>
<rect width="%d" height="%d" rx="4.5" fill="#151515" stroke="#e4e2e2" stroke-opacity="0.2"/>
<text x="20" y="18" class="title">Contributions (across all platforms)</text>
%s
</svg>`, width, height, width, height, width, height, cells)
}

func GenerateContributionHeatmap(contributions map[string]int, outputPath string) error {
	svg := renderContributionHeatmap(contributions, time.Now())
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

func mergeContributions(maps ...map[string]int) map[string]int {
	merged := make(map[string]int)
	for _, m := range maps {
		for k, v := range m {
			merged[k] += v
		}
	}
	return merged
}

func mergeLanguages(maps ...map[string]int64) map[string]int64 {
	merged := make(map[string]int64)
	for _, m := range maps {
		for k, v := range m {
			merged[k] += v
		}
	}
	return merged
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

func main() {
	var allStats []*PlatformStats

	// GitHub
	ghUsername := os.Getenv("GITHUB_USERNAME")
	ghToken := os.Getenv("GH_TOKEN")
	if ghUsername != "" && ghToken != "" {
		fmt.Println("Fetching GitHub stats...")
		stats, err := FetchGitHubStats(ghUsername, ghToken)
		if err != nil {
			fmt.Printf("Error fetching GitHub stats: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("GitHub: %d commits, %d PRs, %d issues\n", stats.TotalCommits, stats.TotalPRsOrMRs, stats.TotalIssuesOrWIs)
		allStats = append(allStats, stats)
	}

	// GitLab
	glUsername := os.Getenv("GITLAB_USERNAME")
	glToken := os.Getenv("GITLAB_ACCESS_TOKEN")
	if glUsername != "" && glToken != "" {
		fmt.Println("Fetching GitLab stats...")
		stats, err := FetchGitLabStats(glUsername, glToken)
		if err != nil {
			fmt.Printf("Error fetching GitLab stats: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("GitLab: %d commits, %d MRs, %d issues\n", stats.TotalCommits, stats.TotalPRsOrMRs, stats.TotalIssuesOrWIs)
		allStats = append(allStats, stats)
	}

	// Azure DevOps
	adoOrg := os.Getenv("AZURE_DEVOPS_ORG")
	adoToken := os.Getenv("AZURE_DEVOPS_ACCESS_TOKEN")
	if adoOrg != "" && adoToken != "" {
		fmt.Println("Fetching Azure DevOps stats...")
		stats, err := FetchAzureDevOpsStats(adoOrg, adoToken)
		if err != nil {
			fmt.Printf("Error fetching Azure DevOps stats: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Azure DevOps: %d commits, %d PRs, %d work items\n", stats.TotalCommits, stats.TotalPRsOrMRs, stats.TotalIssuesOrWIs)
		allStats = append(allStats, stats)
	}

	if len(allStats) == 0 {
		fmt.Println("No platform credentials configured.")
		fmt.Println("Set GITHUB_USERNAME/GH_TOKEN, GITLAB_USERNAME/GITLAB_ACCESS_TOKEN, or AZURE_DEVOPS_ORG/AZURE_DEVOPS_ACCESS_TOKEN")
		os.Exit(1)
	}

	// Merge all stats
	totalCommits := 0
	totalPRs := 0
	totalIssues := 0
	var contribMaps []map[string]int
	var langMaps []map[string]int64

	for _, s := range allStats {
		totalCommits += s.TotalCommits
		totalPRs += s.TotalPRsOrMRs
		totalIssues += s.TotalIssuesOrWIs
		contribMaps = append(contribMaps, s.DailyContributions)
		langMaps = append(langMaps, s.Languages)
	}

	totalContributions := totalCommits + totalPRs + totalIssues
	mergedContribs := mergeContributions(contribMaps...)
	mergedLangs := mergeLanguages(langMaps...)

	// 1. Generate combined stats SVG
	fmt.Println("Generating combined stats SVG...")
	if err := GenerateCombinedStatsSVG(totalCommits, totalPRs, totalIssues, totalContributions, "combined_stats_final.svg"); err != nil {
		fmt.Printf("Error generating combined stats SVG: %v\n", err)
		os.Exit(1)
	}

	// 2. Generate Claude Code tokens line graph
	tokenData, err := loadTokenUsage("claude_tokens.json")
	if err != nil {
		fmt.Printf("Warning: could not load Claude Code token data: %v (skipping tokens graph)\n", err)
	} else {
		fmt.Println("Generating Claude Code tokens graph...")
		if err = GenerateTokensLineGraph(tokenData, "claude_tokens_final.svg"); err != nil {
			fmt.Printf("Error generating tokens graph: %v\n", err)
		}
	}

	// 3. Generate top languages bar chart
	fmt.Println("Generating languages bar chart...")
	if err := GenerateLanguagesBarChart(mergedLangs, "top_languages_final.svg"); err != nil {
		fmt.Printf("Error generating languages chart: %v\n", err)
		os.Exit(1)
	}

	// 4. Generate contribution heatmap
	fmt.Println("Generating contribution heatmap...")
	if err := GenerateContributionHeatmap(mergedContribs, "contributions_final.svg"); err != nil {
		fmt.Printf("Error generating contribution heatmap: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("All SVGs generated successfully!")
}
