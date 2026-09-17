package extractor

import (
	"strings"
	"testing"
	"time"

	"extraction-service/internal/model"
)

func TestReminderMessagesIncludeCurrentTimeAndItems(t *testing.T) {
	msgs := ReminderExtractor{}.Messages(jobs(3))
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("expected system+user messages, got %+v", msgs)
	}
	if !strings.Contains(msgs[1].Content, "Current date/time (UTC): ") {
		t.Fatalf("user prompt missing current date/time: %q", msgs[1].Content)
	}
	for i := 1; i <= 3; i++ {
		marker := "--- item " + string(rune('0'+i)) + " ---"
		if !strings.Contains(msgs[1].Content, marker) {
			t.Fatalf("user prompt missing marker %q: %q", marker, msgs[1].Content)
		}
	}
}

func TestReminderParseMapsItemsToJobs(t *testing.T) {
	content := `{"reminders":[{"item":2,"text":"Call the bank","remind_at":"2026-09-18T11:30:00Z"},{"item":1,"text":"Submit the report","remind_at":null},{"item":2,"text":""}]}`
	results, err := ReminderExtractor{}.Parse(content, jobs(3))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results (one per job), got %d", len(results))
	}
	for _, r := range results {
		if r.ExtractionType != model.TypeReminder {
			t.Fatalf("expected extraction type %q, got %q", model.TypeReminder, r.ExtractionType)
		}
	}
	if len(results[0].Reminders) != 1 {
		t.Fatalf("job 1 reminders wrong: %+v", results[0])
	}
	if got := results[0].Reminders[0].Text; got != "Submit the report" {
		t.Fatalf("job 1 reminder text wrong: %q", got)
	}
	if results[0].Reminders[0].RemindAt != nil {
		t.Fatalf("job 1 remind_at should be nil: %+v", results[0].Reminders[0])
	}
	if len(results[1].Reminders) != 1 {
		t.Fatalf("job 2 reminders wrong: %+v", results[1])
	}
	rem := results[1].Reminders[0]
	if rem.Text != "Call the bank" || rem.RemindAt == nil {
		t.Fatalf("job 2 reminder wrong: %+v", rem)
	}
	if want := time.Date(2026, 9, 18, 11, 30, 0, 0, time.UTC); !rem.RemindAt.Equal(want) {
		t.Fatalf("job 2 remind_at wrong: got %v want %v", rem.RemindAt, want)
	}
	if len(results[2].Reminders) != 0 {
		t.Fatalf("job 3 should have no reminders: %+v", results[2])
	}
}

func TestReminderParseAlwaysYieldsResultPerJob(t *testing.T) {
	results, err := ReminderExtractor{}.Parse(`{"reminders":[]}`, jobs(2))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected one result per job, got %d", len(results))
	}
	for _, r := range results {
		if len(r.Reminders) != 0 {
			t.Fatalf("expected empty reminders, got %+v", r)
		}
	}
}

func TestReminderParseRejectsOutOfRangeItems(t *testing.T) {
	re := ReminderExtractor{}
	if _, err := re.Parse(`{"reminders":[{"item":4,"text":"x","remind_at":null}]}`, jobs(3)); err == nil {
		t.Fatal("expected out-of-range item to fail")
	}
	if _, err := re.Parse(`{"reminders":[{"item":0,"text":"x","remind_at":null}]}`, jobs(3)); err == nil {
		t.Fatal("expected item 0 to fail")
	}
}

func TestReminderParseRejectsUnparseableRemindAt(t *testing.T) {
	if _, err := (ReminderExtractor{}).Parse(`{"reminders":[{"item":1,"text":"x","remind_at":"tomorrow"}]}`, jobs(1)); err == nil {
		t.Fatal("expected unparseable remind_at to fail")
	}
}

func TestReminderParseRejectsMalformedJSON(t *testing.T) {
	if _, err := (ReminderExtractor{}).Parse(`{"reminders": [oops`, jobs(1)); err == nil {
		t.Fatal("expected malformed json to fail")
	}
}
