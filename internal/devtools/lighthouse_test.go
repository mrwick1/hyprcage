package devtools

import (
	"os"
	"sort"
	"testing"
)

func TestSummarizeLighthouse(t *testing.T) {
	raw, err := os.ReadFile("testdata/lighthouse.json")
	if err != nil {
		t.Fatal(err)
	}
	cats := []string{"performance", "accessibility", "best-practices", "seo"}
	s, err := SummarizeLighthouse(raw, cats, 10)
	if err != nil {
		t.Fatal(err)
	}
	if s.Version == "" || len(s.Scores) != 4 {
		t.Fatalf("version %q scores %v", s.Version, s.Scores)
	}
	if _, ok := s.Scores["agentic-browsing"]; ok {
		t.Error("unrequested category reported")
	}
	for n, v := range s.Scores {
		if v < 0 || v > 100 {
			t.Errorf("%s score %d out of range", n, v)
		}
	}
	if len(s.Worst) > 10 {
		t.Errorf("%d worst audits", len(s.Worst))
	}
	ids := map[string]bool{}
	for _, w := range s.Worst {
		if w.Score >= 1 || w.Weight <= 0 || w.Title == "" || ids[w.ID] {
			t.Errorf("bad finding %+v", w)
		}
		ids[w.ID] = true
	}
	if !sort.SliceIsSorted(s.Worst, func(i, j int) bool {
		a, b := s.Worst[i], s.Worst[j]
		return a.Weight > b.Weight || (a.Weight == b.Weight && a.Score < b.Score)
	}) {
		t.Errorf("not sorted: %+v", s.Worst)
	}
	one, _ := SummarizeLighthouse(raw, []string{"seo"}, 1)
	if len(one.Scores) != 1 || len(one.Worst) > 1 {
		t.Errorf("single category: %+v", one)
	}
	if _, err := SummarizeLighthouse(raw, []string{"nope"}, 10); err == nil {
		t.Error("unknown category accepted")
	}
	if _, err := SummarizeLighthouse([]byte("{"), cats, 10); err == nil {
		t.Error("bad JSON accepted")
	}
}
