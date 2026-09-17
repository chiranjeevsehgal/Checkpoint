package extractor

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"extraction-service/internal/llm"
	"extraction-service/internal/model"
)

// ReminderExtractor pulls time-bound commitments out of transcripts: things
// a speaker asked to do, attend, or submit at a specific date and/or time.
// Untimed tasks are the todo extractor's job — the two never overlap.
type ReminderExtractor struct{}

func (ReminderExtractor) Type() string { return model.TypeReminder }

const reminderSystemPrompt = `You are a precise extraction engine for personal audio transcripts (meetings, voice notes, calls).
Find reminders: things a speaker said they (or someone) must do, attend, submit, or follow up on at a specific date and/or time ("call the bank tomorrow at 5", "submit the report on Friday", "the landlord visit is on June 3rd").
Do NOT invent reminders, and do NOT include untimed tasks or intentions — those are todos, not reminders.
Return a JSON object and nothing else, exactly in this shape:
{"reminders":[{"item":<1-based item number the reminder came from>,"text":"<short imperative reminder>","remind_at":"<ISO 8601 UTC datetime>"}]}
Rules:
- "item" must be the number of the item the reminder was extracted from.
- "text" must be a concise, self-contained reminder phrased as an imperative (e.g. "Call the bank").
- "remind_at" must be the resolved due datetime in ISO 8601 UTC (e.g. "2026-09-18T11:30:00Z"), computed against the current date/time given in the user message:
  - a stated date without a time resolves to 09:00 on that date;
  - a stated time without a date resolves to the next occurrence after now;
  - if the statement is time-bound but no concrete date or time can be determined, set "remind_at" to null.
- If an item contains no reminders, it simply has no reminders in the output.
- Never output anything outside the JSON object.`

func (ReminderExtractor) Messages(items []model.Job) []llm.Message {
	var b strings.Builder
	fmt.Fprintf(&b, "Current date/time (UTC): %s\n", time.Now().UTC().Format(time.RFC3339))
	b.WriteString("\nExtract the reminders from the following numbered transcripts.\n")
	for i, it := range items {
		fmt.Fprintf(&b, "\n--- item %d ---\n%s\n", i+1, strings.TrimSpace(it.Text))
	}
	return []llm.Message{
		{Role: "system", Content: reminderSystemPrompt},
		{Role: "user", Content: b.String()},
	}
}

func (ReminderExtractor) Parse(content string, items []model.Job) ([]model.Result, error) {
	var payload struct {
		Reminders []struct {
			Item     int     `json:"item"`
			Text     string  `json:"text"`
			RemindAt *string `json:"remind_at"`
		} `json:"reminders"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil, fmt.Errorf("parsing llm json: %w", err)
	}

	// Every claimed job gets a Result, even with zero reminders — a "no
	// reminders" outcome must still replace any stale rows for that audio.
	results := make([]model.Result, len(items))
	for i, it := range items {
		results[i] = model.Result{JobID: it.ID, UserID: it.UserID, AudioID: it.AudioID, ExtractionType: model.TypeReminder}
	}
	for _, r := range payload.Reminders {
		idx := r.Item - 1
		if idx < 0 || idx >= len(items) {
			return nil, fmt.Errorf("llm returned item %d outside batch of %d", r.Item, len(items))
		}
		text := strings.TrimSpace(r.Text)
		if text == "" {
			continue
		}
		reminder := model.Reminder{Text: text}
		if r.RemindAt != nil && strings.TrimSpace(*r.RemindAt) != "" {
			at, err := time.Parse(time.RFC3339, strings.TrimSpace(*r.RemindAt))
			if err != nil {
				return nil, fmt.Errorf("llm returned unparseable remind_at %q: %w", *r.RemindAt, err)
			}
			reminder.RemindAt = &at
		}
		results[idx].Reminders = append(results[idx].Reminders, reminder)
	}
	return results, nil
}
