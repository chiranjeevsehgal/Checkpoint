package service

import (
	"context"

	"checkpoint/ingestion/internal/domain"
	"checkpoint/ingestion/internal/repository"
)

const (
	itemsDefaultLimit = 50
	itemsMaxLimit     = 200
)

// ItemsService reads and mutates the extraction worker's output for the
// signed-in user, normalizing list filters and page bounds.
type ItemsService struct {
	items repository.ItemsRepository
}

// NewItemsService wires the items service.
func NewItemsService(items repository.ItemsRepository) *ItemsService {
	return &ItemsService{items: items}
}

// ClampPage applies the list defaults and bounds to a requested page. It is
// exported so the HTTP layer can compute the next offset from the same limit
// the repository will use.
func ClampPage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = itemsDefaultLimit
	}
	if limit > itemsMaxLimit {
		limit = itemsMaxLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// ListTodos returns the user's to-dos for the given status and the total.
func (s *ItemsService) ListTodos(ctx context.Context, userID, status string, limit, offset int) ([]repository.Todo, int, error) {
	normalized, err := normalizeTodoStatus(status)
	if err != nil {
		return nil, 0, err
	}
	limit, offset = ClampPage(limit, offset)
	return s.items.ListTodos(ctx, userID, normalized, limit, offset)
}

// UpdateTodo edits one to-do, validating any new text.
func (s *ItemsService) UpdateTodo(ctx context.Context, userID string, id int64, update repository.TodoUpdate) (*repository.Todo, error) {
	if update.Text != nil {
		text, err := domain.ValidateItemText(*update.Text)
		if err != nil {
			return nil, err
		}
		update.Text = &text
	}
	return s.items.UpdateTodo(ctx, userID, id, update)
}

// DeleteTodo removes one of the user's to-dos.
func (s *ItemsService) DeleteTodo(ctx context.Context, userID string, id int64) error {
	return s.items.DeleteTodo(ctx, userID, id)
}

// ListReminders returns the user's reminders for the given window and the total.
func (s *ItemsService) ListReminders(ctx context.Context, userID, window string, limit, offset int) ([]repository.Reminder, int, error) {
	normalized, err := normalizeReminderWindow(window)
	if err != nil {
		return nil, 0, err
	}
	limit, offset = ClampPage(limit, offset)
	return s.items.ListReminders(ctx, userID, normalized, limit, offset)
}

// UpdateReminder edits one reminder, validating any new text.
func (s *ItemsService) UpdateReminder(ctx context.Context, userID string, id int64, update repository.ReminderUpdate) (*repository.Reminder, error) {
	if update.Text != nil {
		text, err := domain.ValidateItemText(*update.Text)
		if err != nil {
			return nil, err
		}
		update.Text = &text
	}
	return s.items.UpdateReminder(ctx, userID, id, update)
}

// DeleteReminder removes one of the user's reminders.
func (s *ItemsService) DeleteReminder(ctx context.Context, userID string, id int64) error {
	return s.items.DeleteReminder(ctx, userID, id)
}

// ListInsights returns the user's insights, newest first, and the total.
func (s *ItemsService) ListInsights(ctx context.Context, userID string, limit, offset int) ([]repository.Insight, int, error) {
	limit, offset = ClampPage(limit, offset)
	return s.items.ListInsights(ctx, userID, limit, offset)
}

// UpdateInsight edits one insight's text, validating it.
func (s *ItemsService) UpdateInsight(ctx context.Context, userID string, id int64, text string) (*repository.Insight, error) {
	normalized, err := domain.ValidateItemText(text)
	if err != nil {
		return nil, err
	}
	return s.items.UpdateInsight(ctx, userID, id, normalized)
}

// DeleteInsight removes one of the user's insights.
func (s *ItemsService) DeleteInsight(ctx context.Context, userID string, id int64) error {
	return s.items.DeleteInsight(ctx, userID, id)
}

func normalizeTodoStatus(status string) (string, error) {
	switch status {
	case "", "all":
		return "all", nil
	case "open", "done":
		return status, nil
	default:
		return "", domain.ErrInvalidItemFilter
	}
}

func normalizeReminderWindow(window string) (string, error) {
	switch window {
	case "", "upcoming":
		return "upcoming", nil
	case "past", "all":
		return window, nil
	default:
		return "", domain.ErrInvalidItemFilter
	}
}
