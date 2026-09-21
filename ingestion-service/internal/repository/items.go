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

// ItemsRepository reads and mutates the extraction worker's output tables.
// Every method runs under the RLS user context, so a caller can only touch
// their own rows.
type ItemsRepository interface {
	// ListTodos returns the user's to-dos, open first then newest. status is
	// "all", "open" or "done".
	ListTodos(ctx context.Context, userID, status string, limit, offset int) ([]Todo, error)
	// SetTodoDone flips one to-do's completion. Unknown ids are a no-op.
	SetTodoDone(ctx context.Context, userID string, id int64, done bool) error
	// DeleteTodo removes one to-do. Unknown ids are a no-op.
	DeleteTodo(ctx context.Context, userID string, id int64) error

	// ListReminders returns the user's reminders for the given window, which
	// is "upcoming", "past" or "all".
	ListReminders(ctx context.Context, userID, window string, limit, offset int) ([]Reminder, error)
	// DeleteReminder removes one reminder. Unknown ids are a no-op.
	DeleteReminder(ctx context.Context, userID string, id int64) error

	// ListInsights returns the user's insights, newest first.
	ListInsights(ctx context.Context, userID string, limit, offset int) ([]Insight, error)
	// DeleteInsight removes one insight. Unknown ids are a no-op.
	DeleteInsight(ctx context.Context, userID string, id int64) error
}
