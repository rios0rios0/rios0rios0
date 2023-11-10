package main

import (
	"fmt"
	"os"
	"time"

	svg "github.com/ajstarks/svgo"
	"github.com/xanzy/go-gitlab"
)

const (
	outputFileName = "heatmap.svg"
)

type Contribution struct {
	Date  string
	Count int
}

func main() {
	gitlabToken := os.Getenv("GITLAB_TOKEN")
	git, _ := gitlab.NewClient(gitlabToken)

	contributions, err := fetchContributions(git)
	if err != nil {
		fmt.Println("Error fetching contributions:", err)
		return
	}

	err = createHeatmapSVG(contributions, outputFileName)
	if err != nil {
		fmt.Println("Error creating heatmap SVG:", err)
		return
	}

	fmt.Printf("Heatmap created...\n")
}

func fetchContributions(client *gitlab.Client) ([]Contribution, error) {
	events, _, err := client.Events.ListCurrentUserContributionEvents(&gitlab.ListContributionEventsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
	})
	if err != nil {
		return nil, err
	}

	contributions := extractContributionsFromEvents(events)

	return contributions, nil
}

func extractContributionsFromEvents(events []*gitlab.ContributionEvent) []Contribution {
	contribMap := make(map[string]int)

	for _, e := range events {
		date := e.CreatedAt.Format("2006-01-02")
		contribMap[date]++
	}

	var contributions []Contribution
	for date, count := range contribMap {
		contributions = append(contributions, Contribution{
			Date:  date,
			Count: count,
		})
	}

	return contributions
}

func createHeatmapSVG(contributions []Contribution, filename string) error {
	width, height := 53*15, 7*15
	margin := 10

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	canvas := svg.New(file)
	canvas.Start(width+2*margin, height+2*margin)
	canvas.Rect(0, 0, width+2*margin, height+2*margin, "fill:white")
	canvas.Translate(margin, margin)

	maxCount := 0
	for _, c := range contributions {
		if c.Count > maxCount {
			maxCount = c.Count
		}
	}

	for i, c := range contributions {
		x := (i % 53) * 15
		y := (i / 53) * 15
		date, _ := time.Parse("2006-01-02", c.Date)

		alpha := float64(c.Count) / float64(maxCount)
		color := fmt.Sprintf("rgba(37, 118, 188, %.2f)", alpha)

		canvas.Rect(x, y, 10, 10, fmt.Sprintf("fill:%s", color))
		canvas.Title(fmt.Sprintf("%s: %d contributions", date.Format("2006-01-02"), c.Count))
	}

	canvas.Gend()
	canvas.End()

	return nil
}

//func createHeatmapPNG(contributions []Contribution, filename string) error {
//	p, err := plot.New()
//	if err != nil {
//		return err
//	}
//
//	// Convert the contributions to a heat.Map
//	contributionData := contributionsToHeatMap(contributions)
//	h := heat.New(contributionData, heat.Palette(palette.Magma()), heat.Min(0), heat.Max(contributionData.Max()))
//	p.Add(h)
//
//	// Save the plot to a PNG file
//	img := vgimg.New(p.DefaultWidth(), p.DefaultHeight())
//	dc := img.Renderer()
//	p.Draw(dc)
//	imgFile, err := os.Create(filename)
//	if err != nil {
//		return err
//	}
//	defer imgFile.Close()
//
//	return png.Encode(imgFile, img.Image())
//}
//
//func contributionsToHeatMap(contributions []Contribution) *heat.Map {
//	data := make([][]float64, 0)
//	for _, contribution := range contributions {
//		date, err := time.Parse("2006-01-02", contribution.Date)
//		if err != nil {
//			continue
//		}
//
//		x, y := dateToCoordinates(date)
//		for len(data) <= x {
//			data = append(data, make([]float64, 0))
//		}
//		for len(data[x]) <= y {
//			data[x] = append(data[x], 0)
//		}
//		data[x][y] = float64(contribution.Count)
//	}
//
//	return heat.NewMap(data)
//}

//func dateToCoordinates(date time.Time) (int, int) {
//	year, week := date.ISOWeek()
//	day := int(date.Weekday())
//	return year*53 + week, day
//}
