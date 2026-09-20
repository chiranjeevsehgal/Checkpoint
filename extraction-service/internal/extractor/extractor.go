package extractor

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"extraction-service/internal/llm"
	"extraction-service/internal/model"
)

// Extractor turns a claimed batch of transcripts into todos, reminders and
// insights in a single LLM call. One batch always belongs to a single user —
// the batcher guarantees it. The prompt assigns every entry to exactly one
// list, and a deterministic guard enforces the reminder-over-todo precedence
// so the same entry can never land in two tables.
type Extractor struct {
	loc *time.Location
}

// New builds an extractor that resolves relative reminder times in the user's
// wall-clock timezone; a nil loc falls back to UTC.
func New(loc *time.Location) Extractor {
	if loc == nil {
		loc = time.UTC
	}
	return Extractor{loc: loc}
}

// PromptVersion identifies the system prompt that produced a result. It is
// persisted alongside the model name (model@version) so prompt changes stay
// visible in the output tables.
const PromptVersion = "v1"

const systemPrompt = `Role: you are an extraction engine for personal audio transcripts (meetings, voice notes, calls). Each numbered item is a separate transcript.

For every item, extract up to three kinds of output:
- todos: an action item the speaker must do, schedule, follow up on, or deliver.
- reminders: an action item with a concrete date and/or time.
- insights: a reflection, realization, conclusion, idea, decision, or opinion worth remembering.

Assignment rules:
- Put each entry in exactly one list. If it has a concrete date or time it is a reminder, never a todo, and never repeat an entry in another list.
- Extract only what was explicitly said. Do not invent entries, do not extract vague intentions or topics that were merely discussed, and emit nothing for an item that contains nothing relevant.
- "item" is the 1-based number of the transcript the entry came from.
- "text" is concise and self-contained. Todos and reminders are imperatives (e.g. "Send the Q3 report to Priya"); insights are statements (e.g. "Vendor quotes are 30% higher").
- "important" is true only for meetings, appointments, scheduled calls, deadlines, and time-bound commitments the user must act on; it is false for casual, optional, or low-stakes items. When unsure, set it to true.

Reminder time rules:
- "remind_at" is the local wall-clock datetime with no UTC offset (e.g. "2026-09-18T17:00:00"), and "remind_at_zone" is the IANA zone id it belongs to (e.g. "Asia/Kolkata").
- Use the user's timezone (given in the user message) unless the speaker names a different zone.
- Resolve relative expressions ("tonight", "tomorrow", "Friday") against the recorded date/time in the item's heading; use the current date/time only when an item has no recorded time.
- A stated date without a time resolves to 09:00 on that date; a stated time without a date resolves to the next occurrence after the anchor.
- "remind_at" must be strictly in the future. If the stated moment has already passed, move it forward to the next day at the same clock time; a weekday moves to its next occurrence.
- If the statement is time-bound but no concrete date or time can be determined, set both "remind_at" and "remind_at_zone" to null.

Return one JSON object and nothing else, exactly in this shape:
{"todos":[{"item":<number>,"text":"<imperative>"}],"reminders":[{"item":<number>,"text":"<imperative>","remind_at":"<local datetime, no offset>","remind_at_zone":"<IANA zone id or null>","important":<boolean>}],"insights":[{"item":<number>,"text":"<statement>"}]}
If nothing was extracted at all, return every list empty. Never output anything outside the JSON object.`

func (e Extractor) Messages(items []model.Job) []llm.Message {
	var b strings.Builder
	fmt.Fprintf(&b, "Current date/time for the user (%s): %s\n", e.loc, time.Now().In(e.loc).Format(time.RFC3339))
	b.WriteString("\nExtract todos, reminders and insights from the following numbered transcripts.\n")
	for i, it := range items {
		fmt.Fprintf(&b, "\n--- item %d%s ---\n%s\n", i+1, e.recordedLabel(it.RecordedAt), strings.TrimSpace(it.Text))
	}
	return []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: b.String()},
	}
}

func (e Extractor) Parse(content string, items []model.Job) ([]model.Result, error) {
	var payload struct {
		Todos []struct {
			Item int    `json:"item"`
			Text string `json:"text"`
		} `json:"todos"`
		Reminders []struct {
			Item         int     `json:"item"`
			Text         string  `json:"text"`
			RemindAt     *string `json:"remind_at"`
			RemindAtZone *string `json:"remind_at_zone"`
			Important    *bool   `json:"important"`
		} `json:"reminders"`
		Insights []struct {
			Item int    `json:"item"`
			Text string `json:"text"`
		} `json:"insights"`
	}
	// Strict decoding: json_object mode guarantees JSON, not the shape. An
	// unexpected key means the model drifted, so fail loudly (retryable)
	// instead of persisting a silently partial result.
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("parsing llm json: %w", err)
	}

	results := newResults(items)

	for _, t := range payload.Todos {
		idx, err := itemIndex(t.Item, len(items), "todo")
		if err != nil {
			return nil, err
		}
		if text := strings.TrimSpace(t.Text); text != "" {
			results[idx].Todos = append(results[idx].Todos, text)
		}
	}
	for _, rem := range payload.Reminders {
		idx, err := itemIndex(rem.Item, len(items), "reminder")
		if err != nil {
			return nil, err
		}
		text := strings.TrimSpace(rem.Text)
		if text == "" {
			continue
		}
		important := true
		if rem.Important != nil {
			important = *rem.Important
		}
		reminder := model.Reminder{Text: text, Important: important}
		if rem.RemindAt != nil && strings.TrimSpace(*rem.RemindAt) != "" {
			at, err := e.parseReminderTime(strings.TrimSpace(*rem.RemindAt), rem.RemindAtZone)
			if err != nil {
				return nil, fmt.Errorf("llm returned unparseable remind_at %q: %w", *rem.RemindAt, err)
			}
			at = rollForwardPast(at, time.Now())
			reminder.RemindAt = &at
		}
		results[idx].Reminders = append(results[idx].Reminders, reminder)
	}
	for _, ins := range payload.Insights {
		idx, err := itemIndex(ins.Item, len(items), "insight")
		if err != nil {
			return nil, err
		}
		if text := strings.TrimSpace(ins.Text); text != "" {
			results[idx].Insights = append(results[idx].Insights, model.Insight{Text: text})
		}
	}

	for i := range results {
		dropTodosShadowedByReminders(&results[i])
	}
	return results, nil
}

// newResults seeds one Result per claimed job so an item with no output still
// replaces any stale rows for its audio.
func newResults(items []model.Job) []model.Result {
	results := make([]model.Result, len(items))
	for i, it := range items {
		results[i] = model.Result{
			JobID:      it.ID,
			UserID:     it.UserID,
			AudioID:    it.AudioID,
			RecordedAt: it.RecordedAt,
		}
	}
	return results
}

// itemIndex converts the LLM's 1-based item number into a slice index.
func itemIndex(item, n int, kind string) (int, error) {
	idx := item - 1
	if idx < 0 || idx >= n {
		return 0, fmt.Errorf("llm returned %s item %d outside batch of %d", kind, item, n)
	}
	return idx, nil
}

// recordedLabel renders " (recorded 2026-09-17 23:17:47 +05:30)" when the item
// carries a recording time, and "" otherwise.
func (e Extractor) recordedLabel(recordedAt string) string {
	formatted := e.formatRecordedAt(recordedAt)
	if formatted == "" {
		return ""
	}
	return " (recorded " + formatted + ")"
}

// formatRecordedAt renders a UTC RFC3339 recording time in the user's zone,
// with its offset, so the model reasons about the speaker's local wall clock.
func (e Extractor) formatRecordedAt(recordedAt string) string {
	recorded := strings.TrimSpace(recordedAt)
	if recorded == "" {
		return ""
	}
	at, err := time.Parse(time.RFC3339, recorded)
	if err != nil {
		return recorded
	}
	return at.In(e.loc).Format("2006-01-02 15:04:05 -07:00")
}

// parseReminderTime resolves a naive local datetime in the named IANA zone,
// falling back to the user's zone. An offset in the value, if the model emits
// one anyway, is honored as-is.
func (e Extractor) parseReminderTime(raw string, zone *string) (time.Time, error) {
	if at, err := time.Parse(time.RFC3339, raw); err == nil {
		return at, nil
	}
	loc := e.loc
	if zone != nil {
		if named := strings.TrimSpace(*zone); named != "" {
			if parsed, err := time.LoadLocation(named); err == nil {
				loc = parsed
			}
		}
	}
	return time.ParseInLocation("2006-01-02T15:04:05", raw, loc)
}

// rollForwardPast moves at to the next future occurrence of its clock time
// when it has already passed, so stored reminders are always actionable.
func rollForwardPast(at, now time.Time) time.Time {
	if at.After(now) {
		return at
	}
	days := int(now.Sub(at)/(24*time.Hour)) + 1
	return at.AddDate(0, 0, days)
}

// normalize lowercases and collapses whitespace for duplicate detection.
func normalize(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// dropTodosShadowedByReminders enforces the prompt's precedence rule in code:
// a time-bound entry is a reminder, so an identical todo is a duplicate.
func dropTodosShadowedByReminders(r *model.Result) {
	if len(r.Todos) == 0 || len(r.Reminders) == 0 {
		return
	}
	reminders := make(map[string]struct{}, len(r.Reminders))
	for _, rem := range r.Reminders {
		reminders[normalize(rem.Text)] = struct{}{}
	}
	kept := make([]string, 0, len(r.Todos))
	for _, todo := range r.Todos {
		if _, duplicate := reminders[normalize(todo)]; duplicate {
			continue
		}
		kept = append(kept, todo)
	}
	r.Todos = kept
}
