package main

import (
	"encoding/json"
	"fmt"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"io/ioutil"
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
func FetchGitLabUserStats(username, gitLabToken string) (*GitLabUserStats, error) {
	// Fetch the user ID
	userURL := fmt.Sprintf("https://gitlab.com/api/v4/users?username=%s", username)

	req, err := http.NewRequest("GET", userURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("PRIVATE-TOKEN", gitLabToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error fetching user: status code %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
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
	totalEvents := 0
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

		eventsReq.Header.Add("PRIVATE-TOKEN", gitLabToken)

		eventsResp, err := client.Do(eventsReq)
		if err != nil {
			return nil, err
		}
		defer eventsResp.Body.Close()

		if eventsResp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("error fetching user events: status code %d", eventsResp.StatusCode)
		}

		eventsBody, err := ioutil.ReadAll(eventsResp.Body)
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
		totalEvents += len(events)

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
		TotalContributions: totalEvents,
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
	gitLabToken := os.Getenv("GITLAB_TOKEN")
	gitLabUsername := os.Getenv("GITLAB_USERNAME")

	if gitLabToken == "" || gitLabUsername == "" {
		fmt.Println("GITLAB_TOKEN and GITLAB_USERNAME environment variables are required")
		os.Exit(1)
	}

	stats, err := FetchGitLabUserStats(gitLabUsername, gitLabToken)
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
