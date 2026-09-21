package summarizer

import (
	"context"
	"strings"
	"testing"
	"time"

	"rollup-service/internal/llm"
	"rollup-service/internal/model"
)

type fakeChatter struct {
	calls  int
	system string
	users  []string
	reply  func(call int) string
}

func (f *fakeChatter) Chat(_ context.Context, messages []llm.Message) (string, error) {
	f.calls++
	if len(messages) > 0 {
		f.system = messages[0].Content
	}
	if len(messages) > 1 {
		f.users = append(f.users, messages[len(messages)-1].Content)
	}
	if f.reply != nil {
		return f.reply(f.calls), nil
	}
	return "recap", nil
}

func TestDailyIncludesDayAndSections(t *testing.T) {
	fake := &fakeChatter{}
	src := model.DaySources{
		Transcripts: []string{"we discussed the launch"},
		Todos:       []string{"send the report"},
		Reminders:   []string{"call the bank"},
		Insights:    []string{"vendor quotes are higher"},
	}
	text, err := New(fake, 10000).Daily(context.Background(), time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC), src)
	if err != nil {
		t.Fatalf("daily: %v", err)
	}
	if text != "recap" {
		t.Fatalf("text = %q, want recap", text)
	}
	if !strings.Contains(fake.system, "English") {
		t.Fatalf("system prompt must request English: %q", fake.system)
	}
	prompt := fake.users[0]
	for _, want := range []string{
		"Daily recap for 2026-09-17",
		"Transcripts:", "we discussed the launch",
		"Todos:", "send the report",
		"Reminders:", "call the bank",
		"Insights:", "vendor quotes are higher",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %q", want, prompt)
		}
	}
}

func TestDailyMapReducesLargeInput(t *testing.T) {
	fake := &fakeChatter{}
	var transcripts []string
	for i := 0; i < 10; i++ {
		transcripts = append(transcripts, "a reasonably long transcript line")
	}
	_, err := New(fake, 60).Daily(context.Background(), time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC), model.DaySources{Transcripts: transcripts})
	if err != nil {
		t.Fatalf("daily: %v", err)
	}
	if fake.calls < 2 {
		t.Fatalf("expected map-reduce (>=2 calls), got %d", fake.calls)
	}
	last := fake.users[len(fake.users)-1]
	if !strings.Contains(last, "recap") {
		t.Fatalf("merge call must receive partial recaps: %q", last)
	}
}

func TestWeeklySkipsEmptyDaysAndLabelsDates(t *testing.T) {
	fake := &fakeChatter{}
	weekStart := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC) // Monday
	texts := []string{"first day", "", "", "fourth day", "", "", "last day"}
	if _, err := New(fake, 10000).Weekly(context.Background(), weekStart, texts); err != nil {
		t.Fatalf("weekly: %v", err)
	}
	prompt := fake.users[0]
	for _, want := range []string{"Weekly recap for the week of 2026-09-14", "(based on 3 of 7 days with recordings)", "2026-09-14: first day", "2026-09-17: fourth day", "2026-09-20: last day"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %q", want, prompt)
		}
	}
}

func TestSplitIntoChunks(t *testing.T) {
	if got := splitIntoChunks("   ", 10); got != nil {
		t.Fatalf("blank text must yield no chunks, got %v", got)
	}
	if got := splitIntoChunks("a\nb", 100); len(got) != 1 {
		t.Fatalf("expected one chunk, got %v", got)
	}
	got := splitIntoChunks("1234567890\nsmall", 8)
	if len(got) != 2 || got[0] != "1234567890" {
		t.Fatalf("overlong line must be its own chunk: %v", got)
	}
}
