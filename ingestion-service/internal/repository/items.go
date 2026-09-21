package repository

import (
	"context"
	"time"
)

// Todo is one extracted to-do item. RecordedAt is the pendant clock carried
// from the recording and is nil when the device had no clock anchor.
type Todo struct {
	ID         int64
	UserID     string
	AudioID    string
	Text       string
	IsDone     bool
	RecordedAt *time.Time
	CreatedAt  time.Time
}

// Reminder is one extracted reminder. RemindAt is the resolved due instant and
// is nil when the time-bound expression could not be resolved.
type Reminder struct {
	ID        int64
	UserID    string
	AudioID   string
	Text      string
	RemindAt  *time.Time
	Important bool
	CreatedAt time.Time
}

// Insight is one extracted insight.
type Insight struct {
	ID        int64
	UserID    string
	AudioID   string
	Text      string
	CreatedAt time.Time
}

// TodoUpdate carries the optional fields of a to-do edit. A nil field is left
// unchanged.
type TodoUpdate struct {
	Text   *string
	IsDone *bool
}

// ReminderUpdate carries the optional fields of a reminder edit. A nil field is
// left unchanged; RemindAt is only applied when RemindAtSet is true, so a nil
// RemindAt with RemindAtSet true clears the due time.
type ReminderUpdate struct {
	Text        *string
	RemindAt    *time.Time
	RemindAtSet bool
	Important   *bool
}

// ItemsRepository reads and mutates the extraction worker's output tables.
// Every method runs under the RLS user context, so a caller can only touch
// their own rows.
type ItemsRepository interface {
	// ListTodos returns the user's to-dos, open first then newest, plus the
	// total number matching the filter. status is "all", "open" or "done".
	ListTodos(ctx context.Context, userID, status string, limit, offset int) ([]Todo, int, error)
	// UpdateTodo edits one to-do and returns it. Missing rows report
	// ErrItemNotFound; duplicate text reports ErrItemConflict.
	UpdateTodo(ctx context.Context, userID string, id int64, update TodoUpdate) (*Todo, error)
	// DeleteTodo removes one to-do. Unknown ids are a no-op.
	DeleteTodo(ctx context.Context, userID string, id int64) error

	// ListReminders returns the user's reminders for the given window, plus the
	// total number matching it. window is "upcoming", "past" or "all".
	ListReminders(ctx context.Context, userID, window string, limit, offset int) ([]Reminder, int, error)
	// UpdateReminder edits one reminder and returns it.
	UpdateReminder(ctx context.Context, userID string, id int64, update ReminderUpdate) (*Reminder, error)
	// DeleteReminder removes one reminder. Unknown ids are a no-op.
	DeleteReminder(ctx context.Context, userID string, id int64) error

	// ListInsights returns the user's insights, newest first, plus the total.
	ListInsights(ctx context.Context, userID string, limit, offset int) ([]Insight, int, error)
	// UpdateInsight edits one insight's text and returns it.
	UpdateInsight(ctx context.Context, userID string, id int64, text string) (*Insight, error)
	// DeleteInsight removes one insight. Unknown ids are a no-op.
	DeleteInsight(ctx context.Context, userID string, id int64) error
}
