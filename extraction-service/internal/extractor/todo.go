package extractor

import (
	"encoding/json"
	"fmt"
	"strings"

	"extraction-service/internal/llm"
	"extraction-service/internal/model"
)

// TodoExtractor pulls action items out of transcripts. The LLM answers in
// strict JSON (response_format json_object) referencing items by their
// 1-based position in the prompt.
type TodoExtractor struct{}

func (TodoExtractor) Type() string { return "todo" }

const todoSystemPrompt = `You are a precise extraction engine for personal audio transcripts (meetings, voice notes, calls).
Find explicit action items / todos: things a speaker said they (or someone) need to do, schedule, follow up on, or deliver.
Do NOT invent tasks, do not extract vague intentions or topics that were merely discussed.
Return a JSON object and nothing else, exactly in this shape:
{"todos":[{"item":<1-based item number the todo came from>,"text":"<short imperative todo>"}]}
Rules:
- "item" must be the number of the item the todo was extracted from.
- "text" must be a concise, self-contained task phrased as an imperative (e.g. "Send the Q3 report to Priya").
- If an item contains no action items, it simply has no todos in the output.
- Never output anything outside the JSON object.`

func (TodoExtractor) Messages(items []model.Job) []llm.Message {
	var b strings.Builder
	b.WriteString("Extract the action items from the following numbered transcripts.\n")
	for i, it := range items {
		fmt.Fprintf(&b, "\n--- item %d ---\n%s\n", i+1, strings.TrimSpace(it.Text))
	}
	return []llm.Message{
		{Role: "system", Content: todoSystemPrompt},
		{Role: "user", Content: b.String()},
	}
}

func (TodoExtractor) Parse(content string, items []model.Job) ([]model.Result, error) {
	var payload struct {
		Todos []struct {
			Item int    `json:"item"`
			Text string `json:"text"`
		} `json:"todos"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil, fmt.Errorf("parsing llm json: %w", err)
	}

	// Every claimed job gets a Result, even with zero todos — a "no action
	// items" outcome must still replace any stale rows for that audio.
	results := make([]model.Result, len(items))
	for i, it := range items {
		results[i] = model.Result{JobID: it.ID, UserID: it.UserID, AudioID: it.AudioID, RecordedAt: it.RecordedAt}
	}
	for _, t := range payload.Todos {
		idx := t.Item - 1
		if idx < 0 || idx >= len(items) {
			return nil, fmt.Errorf("llm returned item %d outside batch of %d", t.Item, len(items))
		}
		text := strings.TrimSpace(t.Text)
		if text == "" {
			continue
		}
		results[idx].Todos = append(results[idx].Todos, text)
	}
	return results, nil
}
