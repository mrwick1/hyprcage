package devtools

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// AuditFinding is one Lighthouse audit that scored below 1.
type AuditFinding struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Score        float64 `json:"score"`
	Weight       float64 `json:"weight"`
	Category     string  `json:"category"`
	DisplayValue string  `json:"display_value,omitempty"`
}

// AuditSummary is the short form of a Lighthouse JSON report.
type AuditSummary struct {
	Version string         `json:"lighthouse_version"`
	URL     string         `json:"url"`
	Scores  map[string]int `json:"scores"`
	Worst   []AuditFinding `json:"worst_audits"`
}

type lhReport struct {
	Version string `json:"lighthouseVersion"`
	URL     string `json:"finalDisplayedUrl"`
	Cats    map[string]struct {
		Score *float64 `json:"score"`
		Refs  []struct {
			ID     string  `json:"id"`
			Weight float64 `json:"weight"`
		} `json:"auditRefs"`
	} `json:"categories"`
	Audits map[string]struct {
		Title        string   `json:"title"`
		Score        *float64 `json:"score"`
		DisplayValue string   `json:"displayValue"`
	} `json:"audits"`
}

// SummarizeLighthouse reduces a Lighthouse JSON report to the scores (0-100)
// of the given categories and the worst audits of those categories: score
// below 1 and weight above 0, by weight descending, then score ascending.
// An audit in two categories is listed once, under the first. limit <= 0
// means 10.
func SummarizeLighthouse(raw []byte, categories []string, limit int) (*AuditSummary, error) {
	var r lhReport
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("lighthouse report is not valid JSON: %w", err)
	}
	if limit <= 0 {
		limit = 10
	}
	sum := &AuditSummary{Version: r.Version, URL: r.URL, Scores: map[string]int{}, Worst: []AuditFinding{}}
	seen := map[string]bool{}
	for _, name := range categories {
		c, ok := r.Cats[name]
		if !ok {
			return nil, fmt.Errorf("category %q is not in the report", name)
		}
		if c.Score != nil {
			sum.Scores[name] = int(math.Round(*c.Score * 100))
		}
		for _, ref := range c.Refs {
			a, ok := r.Audits[ref.ID]
			if !ok || a.Score == nil || *a.Score >= 1 || ref.Weight <= 0 || seen[ref.ID] {
				continue
			}
			seen[ref.ID] = true
			sum.Worst = append(sum.Worst, AuditFinding{ID: ref.ID, Title: a.Title, Score: *a.Score,
				Weight: ref.Weight, Category: name, DisplayValue: a.DisplayValue})
		}
	}
	sort.SliceStable(sum.Worst, func(i, j int) bool {
		a, b := sum.Worst[i], sum.Worst[j]
		if a.Weight != b.Weight {
			return a.Weight > b.Weight
		}
		return a.Score < b.Score
	})
	if len(sum.Worst) > limit {
		sum.Worst = sum.Worst[:limit]
	}
	return sum, nil
}

// TailLines returns the last n lines of s.
func TailLines(s string, n int) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}
