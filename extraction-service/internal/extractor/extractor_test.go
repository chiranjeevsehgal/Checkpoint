package extractor

import (
	"strings"
	"testing"
	"time"

	"extraction-service/internal/model"
)

func jobs(n int) []model.Job {
	out := make([]model.Job, n)
	for i := range out {
		out[i] = model.Job{
			ID:         int64(i + 1),
			UserID:     "u1",
			AudioID:    string(rune('a' + i)),
			Text:       "we should do thing " + string(rune('A'+i)),
			RecordedAt: "2026-09-13T10:15:00Z",
		}
	}
	return out
}

func TestMessagesIncludeCurrentTimeAndItems(t *testing.T) {
	loc := time.FixedZone("IST", 5*60*60+30*60)
	msgs := New(loc).Messages(jobs(3))
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("expected system+user messages, got %+v", msgs)
	}
	if !strings.Contains(msgs[1].Content, "Current date/time for the user (IST): ") {
		t.Fatalf("user prompt missing current date/time: %q", msgs[1].Content)
	}
	if !strings.Contains(msgs[1].Content, "+05:30") {
		t.Fatalf("user prompt missing the user's UTC offset: %q", msgs[1].Content)
	}
	for i := 1; i <= 3; i++ {
		marker := "--- item " + string(rune('0'+i)) + " ---"
		if !strings.Contains(msgs[1].Content, marker) {
			t.Fatalf("user prompt missing marker %q: %q", marker, msgs[1].Content)
		}
	}
}

func TestParseMapsAllThreeLists(t *testing.T) {
	content := `{
		"todos":[{"item":1,"text":"Send the Q3 report to Priya"}],
		"reminders":[{"item":2,"text":"Call the bank","remind_at":"2026-09-18T17:00:00+05:30"},{"item":1,"text":"Submit the report","remind_at":null}],
		"insights":[{"item":2,"text":"Vendor quotes are 30% higher"}]
	}`
	results, err := New(time.UTC).Parse(content, jobs(3))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results (one per job), got %d", len(results))
	}
	if len(results[0].Todos) != 1 || results[0].Todos[0] != "Send the Q3 report to Priya" {
		t.Fatalf("job 1 todos wrong: %+v", results[0])
	}
	if len(results[0].Reminders) != 1 || results[0].Reminders[0].Text != "Submit the report" || results[0].Reminders[0].RemindAt != nil {
		t.Fatalf("job 1 reminders wrong: %+v", results[0])
	}
	if len(results[1].Reminders) != 1 {
		t.Fatalf("job 2 reminders wrong: %+v", results[1])
	}
	rem := results[1].Reminders[0]
	// "17:00+05:30" is the same instant as 11:30 UTC — offset must be honored.
	if want := time.Date(2026, 9, 18, 11, 30, 0, 0, time.UTC); rem.Text != "Call the bank" || rem.RemindAt == nil || !rem.RemindAt.Equal(want) {
		t.Fatalf("job 2 reminder wrong: %+v", rem)
	}
	if len(results[1].Insights) != 1 || results[1].Insights[0].Text != "Vendor quotes are 30% higher" {
		t.Fatalf("job 2 insights wrong: %+v", results[1])
	}
	if !results[2].IsEmpty() {
		t.Fatalf("job 3 should have no output: %+v", results[2])
	}
}

func TestParseDropsTodoShadowedByReminder(t *testing.T) {
	content := `{
		"todos":[{"item":1,"text":"  Call  the Bank. "},{"item":1,"text":"Book the venue"}],
		"reminders":[{"item":1,"text":"call the bank.","remind_at":null}],
		"insights":[]
	}`
	results, err := New(time.UTC).Parse(content, jobs(1))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results[0].Todos) != 1 || results[0].Todos[0] != "Book the venue" {
		t.Fatalf("duplicate todo not dropped: %+v", results[0].Todos)
	}
}

func TestParseRejectsOutOfRangeItem(t *testing.T) {
	cases := map[string]string{
		"todo":     `{"todos":[{"item":4,"text":"x"}]}`,
		"reminder": `{"reminders":[{"item":0,"text":"x","remind_at":null}]}`,
		"insight":  `{"insights":[{"item":5,"text":"x"}]}`,
	}
	for name, content := range cases {
		if _, err := New(time.UTC).Parse(content, jobs(3)); err == nil {
			t.Fatalf("expected out-of-range %s item to fail", name)
		}
	}
}

func TestParseRejectsMalformedJSON(t *testing.T) {
	if _, err := New(time.UTC).Parse(`{"todos": [oops`, jobs(1)); err == nil {
		t.Fatal("expected malformed json to fail")
	}
}

func TestParseRejectsUnparseableRemindAt(t *testing.T) {
	if _, err := New(time.UTC).Parse(`{"reminders":[{"item":1,"text":"x","remind_at":"tomorrow"}]}`, jobs(1)); err == nil {
		t.Fatal("expected unparseable remind_at to fail")
	}
}

func TestParseAlwaysYieldsResultPerJob(t *testing.T) {
	// "nothing to extract" is a valid outcome — jobs must still complete so
	// their (possibly stale) rows get replaced.
	results, err := New(time.UTC).Parse(`{"todos":[],"reminders":[],"insights":[]}`, jobs(2))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected one result per job, got %d", len(results))
	}
	for _, r := range results {
		if !r.IsEmpty() {
			t.Fatalf("expected empty result, got %+v", r)
		}
	}
}

func TestParsePreservesRecordedAt(t *testing.T) {
	results, err := New(time.UTC).Parse(`{"todos":[{"item":1,"text":"x"}]}`, jobs(1))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if results[0].RecordedAt != "2026-09-13T10:15:00Z" {
		t.Fatalf("result lost recorded_at: %+v", results[0])
	}
}
