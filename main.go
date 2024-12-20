package main

import (
	"encoding/json"
	"fmt"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// GitLabUserStats represents the GitLab statistics
type GitLabUserStats struct {
	Name               string `json:"name"`
	Username           string `json:"username"`
	TotalCommits       int    `json:"total_commits"`
	TotalIssues        int    `json:"total_issues"`
	TotalMergeRequests int    `json:"total_merge_requests"`
	TotalContributions int    `json:"total_contributions"`
}

// FetchGitLabUserStats fetches the user stats from GitLab
func FetchGitLabUserStats(username, accessToken string) (*GitLabUserStats, error) {
	// Fetch the user ID
	userURL := fmt.Sprintf("https://gitlab.com/api/v4/users?username=%s", username)

	req, err := http.NewRequest("GET", userURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("PRIVATE-TOKEN", accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error fetching user: status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var users []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	err = json.Unmarshal(body, &users)
	if err != nil {
		return nil, err
	}

	if len(users) == 0 {
		return nil, fmt.Errorf("user not found")
	}

	userID := users[0].ID

	// initialize counters
	totalCommits := 0
	totalIssues := 0
	totalMergeRequests := 0

	// fetch the user events for the last year
	now := time.Now()
	oneYearAgo := now.AddDate(-1, 0, 0)
	page := 1
	pageSize := 100 // adjust pageSize as needed

	for {
		eventsURL := fmt.Sprintf("https://gitlab.com/api/v4/users/%d/events?after=%s&before=%s&page=%d&per_page=%d", userID, oneYearAgo.Format("2006-01-02"), now.Format("2006-01-02"), page, pageSize)

		eventsReq, err := http.NewRequest("GET", eventsURL, nil)
		if err != nil {
			return nil, err
		}

		eventsReq.Header.Add("PRIVATE-TOKEN", accessToken)

		eventsResp, err := client.Do(eventsReq)
		if err != nil {
			return nil, err
		}
		defer eventsResp.Body.Close()

		if eventsResp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("error fetching user events: status code %d", eventsResp.StatusCode)
		}

		eventsBody, err := io.ReadAll(eventsResp.Body)
		if err != nil {
			return nil, err
		}

		var events []struct {
			Action     string `json:"action_name"`
			TargetType string `json:"target_type"`
			PushData   struct {
				CommitCount int `json:"commit_count"`
			} `json:"push_data"`
		}
		err = json.Unmarshal(eventsBody, &events)
		if err != nil {
			return nil, err
		}

		if len(events) == 0 {
			break // Exit the loop if no more events
		}

		for _, event := range events {
			switch event.TargetType {
			case "Issue":
				totalIssues++
			case "MergeRequest":
				totalMergeRequests++
			}
			if strings.Contains(event.Action, "pushed") {
				totalCommits += event.PushData.CommitCount
			}
		}

		page++
	}

	// construct the user stats
	stats := GitLabUserStats{
		Name:               users[0].Name,
		Username:           username,
		TotalCommits:       totalCommits,
		TotalIssues:        totalIssues,
		TotalMergeRequests: totalMergeRequests,
		TotalContributions: totalCommits + totalIssues + totalMergeRequests,
	}

	return &stats, nil
}

// GenerateSVG generates the SVG for GitLab stats - this will generate the SVG using the provided template
func GenerateSVG(stats *GitLabUserStats, templatePath, outputPath string) error {
	// read the SVG template file
	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return err
	}

	printer := message.NewPrinter(language.English)
	// fill the template with stats data
	svgContent := printer.Sprintf(string(templateContent),
		stats.Name+"' GitLab Stats",
		stats.TotalCommits,
		stats.TotalMergeRequests,
		stats.TotalIssues,
		stats.TotalContributions,
	)

	// write the processed SVG content to the output file
	return os.WriteFile(outputPath, []byte(svgContent), 0644)
}

func main() {
	gitLabUsername := os.Getenv("GITLAB_USERNAME")
	gitLabAccessToken := os.Getenv("GITLAB_ACCESS_TOKEN")

	if gitLabUsername == "" || gitLabAccessToken == "" {
		fmt.Println("GITLAB_USERNAME and GITLAB_ACCESS_TOKEN environment variables are required")
		os.Exit(1)
	}

	stats, err := FetchGitLabUserStats(gitLabUsername, gitLabAccessToken)
	if err != nil {
		fmt.Println("Error fetching GitLab user stats:", err)
		os.Exit(1)
	}

	err = GenerateSVG(stats, "gitlab_stats.svg", "gitlab_stats_final.svg")
	if err != nil {
		fmt.Println("Error generating SVG report:", err)
		os.Exit(1)
	}

	fmt.Println("SVG generated successfully...")
}
