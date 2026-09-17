package extractor

import (
	"strings"
	"testing"

	"extraction-service/internal/model"
)

func jobs(n int) []model.Job {
	out := make([]model.Job, n)
	for i := range out {
		out[i] = model.Job{ID: int64(i + 1), UserID: "u1", AudioID: string(rune('a' + i)), Text: "we should do thing " + string(rune('A'+i)), RecordedAt: "2026-09-13T10:15:00Z"}
	}
	return out
}

func TestTodoMessagesNumberItemsInOrder(t *testing.T) {
	msgs := TodoExtractor{}.Messages(jobs(3))
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

func TestTodoParseMapsItemsToJobs(t *testing.T) {
	content := `{"todos":[{"item":2,"text":"Call the dentist"},{"item":1,"text":"Send the Q3 report to Priya"},{"item":2,"text":""}]}`
	results, err := TodoExtractor{}.Parse(content, jobs(3))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results (one per job), got %d", len(results))
	}
	if len(results[0].Todos) != 1 || results[0].Todos[0] != "Send the Q3 report to Priya" {
		t.Fatalf("job 1 todos wrong: %+v", results[0])
	}
	for i, r := range results {
		if r.RecordedAt != "2026-09-13T10:15:00Z" {
			t.Fatalf("result %d lost recorded_at: %+v", i, r)
		}
	}
	if len(results[1].Todos) != 1 || results[1].Todos[0] != "Call the dentist" {
		t.Fatalf("job 2 todos wrong: %+v", results[1])
	}
	if len(results[2].Todos) != 0 {
		t.Fatalf("job 3 should have no todos: %+v", results[2])
	}
}

func TestTodoParseAlwaysYieldsResultPerJob(t *testing.T) {
	// "No action items found" is a valid outcome — jobs must still complete
	// so their (possibly stale) todos get replaced.
	results, err := TodoExtractor{}.Parse(`{"todos":[]}`, jobs(2))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected one result per job, got %d", len(results))
	}
	for _, r := range results {
		if len(r.Todos) != 0 {
			t.Fatalf("expected empty todos, got %+v", r)
		}
	}
}

func TestTodoParseRejectsOutOfRangeItems(t *testing.T) {
	te := TodoExtractor{}
	if _, err := te.Parse(`{"todos":[{"item":4,"text":"x"}]}`, jobs(3)); err == nil {
		t.Fatal("expected out-of-range item to fail")
	}
	if _, err := te.Parse(`{"todos":[{"item":0,"text":"x"}]}`, jobs(3)); err == nil {
		t.Fatal("expected item 0 to fail")
	}
}

func TestTodoParseRejectsMalformedJSON(t *testing.T) {
	te := TodoExtractor{}
	if _, err := te.Parse(`{"todos": [oops`, jobs(1)); err == nil {
		t.Fatal("expected malformed json to fail")
	}
}
