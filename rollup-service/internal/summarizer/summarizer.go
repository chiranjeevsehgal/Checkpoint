package summarizer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"rollup-service/internal/llm"
	"rollup-service/internal/model"
)

// Chatter is the subset of the LLM client the summarizer needs.
type Chatter interface {
	Chat(ctx context.Context, messages []llm.Message) (string, error)
}

// Summarizer turns a period's material into a narrative, map-reducing when the
// input exceeds maxInputChars.
type Summarizer struct {
	client        Chatter
	maxInputChars int
}

func New(client Chatter, maxInputChars int) Summarizer {
	if maxInputChars <= 0 {
		maxInputChars = 24000
	}
	return Summarizer{client: client, maxInputChars: maxInputChars}
}

const systemPrompt = `Role: you write short, factual recaps in English of a person's recorded conversations, for their own review.

Use only the material provided. Never invent or extrapolate details.
Write plain prose in the past tense: 3-6 sentences for a daily recap, 5-10 for a weekly recap.
Prioritize concrete decisions, follow-ups and noteworthy reflections; drop small talk and logistics with no lasting value.
Output the recap only: no heading, bullet points, labels or preamble.`

// Daily summarizes one local day from its transcripts and extracted items.
func (s Summarizer) Daily(ctx context.Context, day time.Time, src model.DaySources) (string, error) {
	heading := "Daily recap for " + day.Format("2006-01-02")
	return s.summarize(ctx, heading, dayBody(src))
}

// Weekly summarizes a week from its ordered daily recaps (index i is
// weekStart+i days). Empty entries are skipped.
func (s Summarizer) Weekly(ctx context.Context, weekStart time.Time, dailyTexts []string) (string, error) {
	heading := "Weekly recap for the week of " + weekStart.Format("2006-01-02")
	var b strings.Builder
	for i, text := range dailyTexts {
		if strings.TrimSpace(text) == "" {
			continue
		}
		day := weekStart.AddDate(0, 0, i).Format("2006-01-02")
		fmt.Fprintf(&b, "%s: %s\n", day, strings.TrimSpace(text))
	}
	return s.summarize(ctx, heading, b.String())
}

// summarize makes one LLM call, or a map-reduce when the body is large.
func (s Summarizer) summarize(ctx context.Context, heading, body string) (string, error) {
	chunks := splitIntoChunks(body, s.maxInputChars)
	if len(chunks) <= 1 {
		return s.call(ctx, heading, body)
	}
	partials := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		part, err := s.call(ctx, fmt.Sprintf("%s (part %d of %d)", heading, i+1, len(chunks)), chunk)
		if err != nil {
			return "", err
		}
		partials = append(partials, part)
	}
	return s.call(ctx, heading, strings.Join(partials, "\n\n"))
}

func (s Summarizer) call(ctx context.Context, heading, body string) (string, error) {
	content, err := s.client.Chat(ctx, []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: heading + "\n\n" + strings.TrimSpace(body)},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(content), nil
}

// dayBody renders a day's transcripts and extracted items as prompt sections.
func dayBody(src model.DaySources) string {
	var b strings.Builder
	writeSection(&b, "Transcripts", src.Transcripts)
	writeSection(&b, "Todos", src.Todos)
	writeSection(&b, "Reminders", src.Reminders)
	writeSection(&b, "Insights", src.Insights)
	return b.String()
}

func writeSection(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", title)
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", strings.TrimSpace(item))
	}
	b.WriteString("\n")
}

// splitIntoChunks packs whole lines into chunks no larger than maxChars. A
// single overlong line becomes its own chunk.
func splitIntoChunks(text string, maxChars int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var chunks []string
	var current strings.Builder
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if current.Len() > 0 && current.Len()+len(line)+1 > maxChars {
			chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
			current.Reset()
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
	}
	return chunks
}
