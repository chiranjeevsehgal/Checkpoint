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
		marker := "--- item " + string(rune('0'+i)) + " (recorded"
		if !strings.Contains(msgs[1].Content, marker) {
			t.Fatalf("user prompt missing marker %q: %q", marker, msgs[1].Content)
		}
	}
}

func TestParseMapsAllThreeLists(t *testing.T) {
	content := `{
		"todos":[{"item":1,"text":"Send the Q3 report to Priya"}],
		"reminders":[{"item":2,"text":"Call the bank","remind_at":"2099-09-18T17:00:00+05:30"},{"item":1,"text":"Submit the report","remind_at":null}],
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
	if want := time.Date(2099, 9, 18, 11, 30, 0, 0, time.UTC); rem.Text != "Call the bank" || rem.RemindAt == nil || !rem.RemindAt.Equal(want) {
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

func TestMessagesIncludeRecordedAt(t *testing.T) {
	loc := time.FixedZone("IST", 5*60*60+30*60)
	items := []model.Job{
		{ID: 1, Text: "a", RecordedAt: "2026-09-17T17:47:47Z"},
		{ID: 2, Text: "b"},
	}
	content := New(loc).Messages(items)[1].Content
	if !strings.Contains(content, "--- item 1 (recorded 2026-09-17 23:17:47 +05:30) ---") {
		t.Fatalf("item 1 heading not rendered in the user zone: %q", content)
	}
	if !strings.Contains(content, "--- item 2 ---") {
		t.Fatalf("item 2 heading wrong: %q", content)
	}
	if strings.Contains(content, "--- item 2 (recorded") {
		t.Fatalf("item 2 must not show a recorded time: %q", content)
	}
}

func TestRollForwardPast(t *testing.T) {
	loc := time.FixedZone("IST", 5*60*60+30*60)
	now := time.Date(2026, 9, 17, 23, 18, 0, 0, loc)
	cases := []struct {
		name string
		at   time.Time
		want time.Time
	}{
		{
			name: "future unchanged",
			at:   time.Date(2026, 9, 18, 9, 0, 0, 0, loc),
			want: time.Date(2026, 9, 18, 9, 0, 0, 0, loc),
		},
		{
			name: "earlier today rolls to tomorrow",
			at:   time.Date(2026, 9, 17, 19, 0, 0, 0, loc),
			want: time.Date(2026, 9, 18, 19, 0, 0, 0, loc),
		},
		{
			name: "just under a day ago rolls to tomorrow",
			at:   time.Date(2026, 9, 16, 23, 30, 0, 0, loc),
			want: time.Date(2026, 9, 17, 23, 30, 0, 0, loc),
		},
		{
			name: "over a day ago rolls to the next future day",
			at:   time.Date(2026, 9, 16, 23, 0, 0, 0, loc),
			want: time.Date(2026, 9, 18, 23, 0, 0, 0, loc),
		},
		{
			name: "exactly now rolls to tomorrow",
			at:   now,
			want: time.Date(2026, 9, 18, 23, 18, 0, 0, loc),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rollForwardPast(c.at, now); !got.Equal(c.want) {
				t.Fatalf("rollForwardPast(%v) = %v, want %v", c.at, got, c.want)
			}
		})
	}
}

func TestParseRollsForwardPastRemindAt(t *testing.T) {
	results, err := New(time.UTC).Parse(
		`{"reminders":[{"item":1,"text":"Call Dad","remind_at":"2000-01-01T19:00:00+05:30"}]}`, jobs(1))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := results[0].Reminders[0].RemindAt
	if got == nil {
		t.Fatal("expected a rolled-forward remind_at, got nil")
	}
	if !got.After(time.Now()) {
		t.Fatalf("remind_at must be in the future, got %v", got)
	}
	if got.Hour() != 19 || got.Minute() != 0 {
		t.Fatalf("clock time not preserved: %v", got)
	}
}

func TestParseKeepsFutureRemindAt(t *testing.T) {
	want, err := time.Parse(time.RFC3339, time.Now().Add(48*time.Hour).Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	content := `{"reminders":[{"item":1,"text":"x","remind_at":"` + want.Format(time.RFC3339) + `"}]}`
	results, err := New(time.UTC).Parse(content, jobs(1))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := results[0].Reminders[0].RemindAt; got == nil || !got.Equal(want) {
		t.Fatalf("future remind_at changed: got %v want %v", got, want)
	}
}

func TestParseResolvesNamedIANAZone(t *testing.T) {
	content := `{"reminders":[{"item":1,"text":"Call Dad","remind_at":"2099-09-18T19:00:00","remind_at_zone":"Europe/Berlin"}]}`
	results, err := New(time.UTC).Parse(content, jobs(1))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := results[0].Reminders[0].RemindAt
	// September in Berlin is CEST (UTC+2).
	if want := time.Date(2099, 9, 18, 17, 0, 0, 0, time.UTC); got == nil || !got.Equal(want) {
		t.Fatalf("remind_at = %v, want %v", got, want)
	}
}

func TestParseUsesUserZoneWhenZoneAbsent(t *testing.T) {
	loc := time.FixedZone("IST", 5*60*60+30*60)
	content := `{"reminders":[{"item":1,"text":"Call Dad","remind_at":"2099-09-18T19:00:00","remind_at_zone":null}]}`
	results, err := New(loc).Parse(content, jobs(1))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := results[0].Reminders[0].RemindAt
	if want := time.Date(2099, 9, 18, 13, 30, 0, 0, time.UTC); got == nil || !got.Equal(want) {
		t.Fatalf("remind_at = %v, want %v", got, want)
	}
}

func TestParseFallsBackToUserZoneOnUnknownZone(t *testing.T) {
	loc := time.FixedZone("IST", 5*60*60+30*60)
	content := `{"reminders":[{"item":1,"text":"Call Dad","remind_at":"2099-09-18T19:00:00","remind_at_zone":"Not/AZone"}]}`
	results, err := New(loc).Parse(content, jobs(1))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := results[0].Reminders[0].RemindAt
	if want := time.Date(2099, 9, 18, 13, 30, 0, 0, time.UTC); got == nil || !got.Equal(want) {
		t.Fatalf("remind_at = %v, want %v", got, want)
	}
}
