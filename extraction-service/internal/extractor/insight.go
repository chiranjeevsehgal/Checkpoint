package extractor

import (
	"encoding/json"
	"fmt"
	"strings"

	"extraction-service/internal/llm"
	"extraction-service/internal/model"
)

// InsightExtractor pulls reflections out of transcripts: realizations,
// ideas, conclusions, and decisions worth remembering. Action items are the
// todo/reminder extractors' job — the types never overlap.
type InsightExtractor struct{}

func (InsightExtractor) Type() string { return model.TypeInsight }

const insightSystemPrompt = `You are a precise extraction engine for personal audio transcripts (meetings, voice notes, calls).
Find insights: realizations, reflections, conclusions, ideas, decisions, and opinions worth remembering — things the speaker learned or figured out ("turns out the vendor quotes are 30% higher", "I think the delay is actually about budget", "we decided to pause the launch").
Do NOT invent insights, and do NOT extract action items, tasks, or reminders — those are other extraction types.
Return a JSON object and nothing else, exactly in this shape:
{"insights":[{"item":<1-based item number the insight came from>,"text":"<short insight as a statement>"}]}
Rules:
- "item" must be the number of the item the insight was extracted from.
- "text" must be concise and self-contained, phrased as a statement (not an imperative).
- If an item contains no insights, it simply has no insights in the output.
- Never output anything outside the JSON object.`

func (InsightExtractor) Messages(items []model.Job) []llm.Message {
	var b strings.Builder
	b.WriteString("Extract the insights from the following numbered transcripts.\n")
	for i, it := range items {
		fmt.Fprintf(&b, "\n--- item %d ---\n%s\n", i+1, strings.TrimSpace(it.Text))
	}
	return []llm.Message{
		{Role: "system", Content: insightSystemPrompt},
		{Role: "user", Content: b.String()},
	}
}

func (InsightExtractor) Parse(content string, items []model.Job) ([]model.Result, error) {
	var payload struct {
		Insights []struct {
			Item int    `json:"item"`
			Text string `json:"text"`
		} `json:"insights"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil, fmt.Errorf("parsing llm json: %w", err)
	}

	// Every claimed job gets a Result, even with zero insights — a "no
	// insights" outcome must still replace any stale rows for that audio.
	results := make([]model.Result, len(items))
	for i, it := range items {
		results[i] = model.Result{JobID: it.ID, UserID: it.UserID, AudioID: it.AudioID, ExtractionType: model.TypeInsight}
	}
	for _, ins := range payload.Insights {
		idx := ins.Item - 1
		if idx < 0 || idx >= len(items) {
			return nil, fmt.Errorf("llm returned item %d outside batch of %d", ins.Item, len(items))
		}
		text := strings.TrimSpace(ins.Text)
		if text == "" {
			continue
		}
		results[idx].Insights = append(results[idx].Insights, model.Insight{Text: text})
	}
	return results, nil
}
