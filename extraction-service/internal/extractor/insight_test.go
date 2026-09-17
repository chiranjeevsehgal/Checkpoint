package extractor

import (
	"strings"
	"testing"

	"extraction-service/internal/model"
)

func TestInsightMessagesNumberItemsInOrder(t *testing.T) {
	msgs := InsightExtractor{}.Messages(jobs(3))
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("expected system+user messages, got %+v", msgs)
	}
	for i := 1; i <= 3; i++ {
		marker := "--- item " + string(rune('0'+i)) + " ---"
		if !strings.Contains(msgs[1].Content, marker) {
			t.Fatalf("user prompt missing marker %q: %q", marker, msgs[1].Content)
		}
	}
}

func TestInsightParseMapsItemsToJobs(t *testing.T) {
	content := `{"insights":[{"item":2,"text":"The delay is actually about budget"},{"item":1,"text":"Vendor quotes are 30% higher"},{"item":2,"text":""}]}`
	results, err := InsightExtractor{}.Parse(content, jobs(3))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results (one per job), got %d", len(results))
	}
	for _, r := range results {
		if r.ExtractionType != model.TypeInsight {
			t.Fatalf("expected extraction type %q, got %q", model.TypeInsight, r.ExtractionType)
		}
	}
	if len(results[0].Insights) != 1 || results[0].Insights[0].Text != "Vendor quotes are 30% higher" {
		t.Fatalf("job 1 insights wrong: %+v", results[0])
	}
	if len(results[1].Insights) != 1 || results[1].Insights[0].Text != "The delay is actually about budget" {
		t.Fatalf("job 2 insights wrong: %+v", results[1])
	}
	if len(results[2].Insights) != 0 {
		t.Fatalf("job 3 should have no insights: %+v", results[2])
	}
}

func TestInsightParseAlwaysYieldsResultPerJob(t *testing.T) {
	results, err := InsightExtractor{}.Parse(`{"insights":[]}`, jobs(2))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected one result per job, got %d", len(results))
	}
	for _, r := range results {
		if len(r.Insights) != 0 {
			t.Fatalf("expected empty insights, got %+v", r)
		}
	}
}

func TestInsightParseRejectsOutOfRangeItems(t *testing.T) {
	ie := InsightExtractor{}
	if _, err := ie.Parse(`{"insights":[{"item":4,"text":"x"}]}`, jobs(3)); err == nil {
		t.Fatal("expected out-of-range item to fail")
	}
	if _, err := ie.Parse(`{"insights":[{"item":0,"text":"x"}]}`, jobs(3)); err == nil {
		t.Fatal("expected item 0 to fail")
	}
}

func TestInsightParseRejectsMalformedJSON(t *testing.T) {
	if _, err := (InsightExtractor{}).Parse(`{"insights": [oops`, jobs(1)); err == nil {
		t.Fatal("expected malformed json to fail")
	}
}
