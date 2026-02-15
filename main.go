package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
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

// GenerateSVG generates an SVG stats card using the provided template and values
func GenerateSVG(title string, totalCommits, totalPRsOrMRs, totalIssuesOrWIs, totalContributions int, templatePath, outputPath string) error {
	// read the SVG template file
	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return err
	}

	printer := message.NewPrinter(language.English)
	// fill the template with stats data
	svgContent := printer.Sprintf(string(templateContent),
		title,
		totalCommits,
		totalPRsOrMRs,
		totalIssuesOrWIs,
		totalContributions,
	)

	// write the processed SVG content to the output file
	return os.WriteFile(outputPath, []byte(svgContent), 0644)
}

// AzureDevOpsUserStats represents the Azure DevOps statistics
type AzureDevOpsUserStats struct {
	Name               string
	TotalCommits       int
	TotalPullRequests  int
	TotalWorkItems     int
	TotalContributions int
}

// FetchAzureDevOpsUserStats fetches the user stats from Azure DevOps
func FetchAzureDevOpsUserStats(organization, accessToken string) (*AzureDevOpsUserStats, error) {
	client := &http.Client{}
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+accessToken))

	// helper to create authenticated requests
	newRequest := func(method, reqURL string, body io.Reader) (*http.Request, error) {
		req, err := http.NewRequest(method, reqURL, body)
		if err != nil {
			return nil, err
		}
		req.Header.Add("Authorization", authHeader)
		return req, nil
	}

	// helper to perform a request and read the response body
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

	// get authenticated user info
	connURL := fmt.Sprintf("https://dev.azure.com/%s/_apis/connectionData?api-version=7.0", url.PathEscape(organization))
	req, err := newRequest("GET", connURL, nil)
	if err != nil {
		return nil, err
	}

	body, statusCode, err := doRequest(req)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("error fetching connection data: status code %d", statusCode)
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

	// set up time range for the last year
	now := time.Now()
	oneYearAgo := now.AddDate(-1, 0, 0)
	fromDate := oneYearAgo.Format("2006-01-02T15:04:05Z")

	// get all projects in the organization
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
			return nil, fmt.Errorf("error fetching projects: status code %d", resp.StatusCode)
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

	totalCommits := 0
	totalPRs := 0
	totalWorkItems := 0

	for _, proj := range projects {
		// get git repositories for this project
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

		// count commits by the user in each repo
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
				}
				if err = json.Unmarshal(body, &commitsResult); err != nil {
					break
				}

				totalCommits += commitsResult.Count
				if commitsResult.Count < 100 {
					break
				}
				skip += 100
			}
		}

		// count pull requests created by the user in this project
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
					totalPRs++
				}
			}

			if prsResult.Count < 100 {
				break
			}
			skip += 100
		}
	}

	// count work items assigned to the authenticated user via WIQL
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
			totalWorkItems = len(wiqlResult.WorkItems)
		}
	}

	return &AzureDevOpsUserStats{
		Name:               displayName,
		TotalCommits:       totalCommits,
		TotalPullRequests:  totalPRs,
		TotalWorkItems:     totalWorkItems,
		TotalContributions: totalCommits + totalPRs + totalWorkItems,
	}, nil
}

func main() {
	ranAny := false

	// GitLab stats
	gitLabUsername := os.Getenv("GITLAB_USERNAME")
	gitLabAccessToken := os.Getenv("GITLAB_ACCESS_TOKEN")
	if gitLabUsername != "" && gitLabAccessToken != "" {
		stats, err := FetchGitLabUserStats(gitLabUsername, gitLabAccessToken)
		if err != nil {
			fmt.Println("Error fetching GitLab user stats:", err)
			os.Exit(1)
		}

		err = GenerateSVG(stats.Name+"' GitLab Stats", stats.TotalCommits, stats.TotalMergeRequests, stats.TotalIssues, stats.TotalContributions, "gitlab_stats.svg", "gitlab_stats_final.svg")
		if err != nil {
			fmt.Println("Error generating GitLab SVG report:", err)
			os.Exit(1)
		}

		fmt.Println("GitLab SVG generated successfully...")
		ranAny = true
	}

	// Azure DevOps stats
	azureDevOpsOrg := os.Getenv("AZURE_DEVOPS_ORG")
	azureDevOpsAccessToken := os.Getenv("AZURE_DEVOPS_ACCESS_TOKEN")
	if azureDevOpsOrg != "" && azureDevOpsAccessToken != "" {
		stats, err := FetchAzureDevOpsUserStats(azureDevOpsOrg, azureDevOpsAccessToken)
		if err != nil {
			fmt.Println("Error fetching Azure DevOps user stats:", err)
			os.Exit(1)
		}

		err = GenerateSVG(stats.Name+"' Azure DevOps Stats", stats.TotalCommits, stats.TotalPullRequests, stats.TotalWorkItems, stats.TotalContributions, "azure_devops_stats.svg", "azure_devops_stats_final.svg")
		if err != nil {
			fmt.Println("Error generating Azure DevOps SVG report:", err)
			os.Exit(1)
		}

		fmt.Println("Azure DevOps SVG generated successfully...")
		ranAny = true
	}

	if !ranAny {
		fmt.Println("No platform credentials configured. Set GITLAB_USERNAME/GITLAB_ACCESS_TOKEN or AZURE_DEVOPS_ORG/AZURE_DEVOPS_ACCESS_TOKEN environment variables.")
		os.Exit(1)
	}
}
