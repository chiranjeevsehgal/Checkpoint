package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"checkpoint/ingestion/internal/repository"
)

// ListTodos returns the user's to-dos, open first then newest.
func (p *Pool) ListTodos(ctx context.Context, userID, status string, limit, offset int) ([]repository.Todo, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	filter := ""
	switch status {
	case "open":
		filter = "AND is_done = FALSE"
	case "done":
		filter = "AND is_done = TRUE"
	}

	todos := []repository.Todo{}
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, audio_id::text, text, is_done, recorded_at, created_at
			FROM todos
			WHERE user_id = $1 `+filter+`
			ORDER BY is_done ASC, COALESCE(recorded_at, created_at) DESC, id DESC
			LIMIT $2 OFFSET $3`, userID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var todo repository.Todo
			var recordedAt pgtype.Timestamptz
			if err := rows.Scan(&todo.ID, &todo.AudioID, &todo.Text, &todo.IsDone, &recordedAt, &todo.CreatedAt); err != nil {
				return err
			}
			todo.UserID = userID
			todo.RecordedAt = timeFromPg(recordedAt)
			todos = append(todos, todo)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return todos, nil
}

// SetTodoDone flips one to-do's completion. Unknown ids are a no-op.
func (p *Pool) SetTodoDone(ctx context.Context, userID string, id int64, done bool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE todos SET is_done = $3 WHERE id = $1 AND user_id = $2`, id, userID, done)
		return err
	})
}

// DeleteTodo removes one to-do. Unknown ids are a no-op.
func (p *Pool) DeleteTodo(ctx context.Context, userID string, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM todos WHERE id = $1 AND user_id = $2`, id, userID)
		return err
	})
}

// ListReminders returns the user's reminders for the given window.
func (p *Pool) ListReminders(ctx context.Context, userID, window string, limit, offset int) ([]repository.Reminder, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	filter, order := "", "remind_at DESC NULLS LAST, id DESC"
	switch window {
	case "upcoming":
		filter = "AND remind_at IS NOT NULL AND remind_at >= now()"
		order = "important DESC, remind_at ASC, id ASC"
	case "past":
		filter = "AND remind_at IS NOT NULL AND remind_at < now()"
		order = "remind_at DESC, id DESC"
	}

	reminders := []repository.Reminder{}
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, audio_id, text, remind_at, important, created_at
			FROM reminders
			WHERE user_id = $1 `+filter+`
			ORDER BY `+order+`
			LIMIT $2 OFFSET $3`, userID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var reminder repository.Reminder
			var remindAt pgtype.Timestamptz
			if err := rows.Scan(&reminder.ID, &reminder.AudioID, &reminder.Text, &remindAt, &reminder.Important, &reminder.CreatedAt); err != nil {
				return err
			}
			reminder.UserID = userID
			reminder.RemindAt = timeFromPg(remindAt)
			reminders = append(reminders, reminder)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return reminders, nil
}

// DeleteReminder removes one reminder. Unknown ids are a no-op.
func (p *Pool) DeleteReminder(ctx context.Context, userID string, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM reminders WHERE id = $1 AND user_id = $2`, id, userID)
		return err
	})
}

// ListInsights returns the user's insights, newest first.
func (p *Pool) ListInsights(ctx context.Context, userID string, limit, offset int) ([]repository.Insight, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	insights := []repository.Insight{}
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, audio_id, text, created_at
			FROM insights
			WHERE user_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT $2 OFFSET $3`, userID, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var insight repository.Insight
			if err := rows.Scan(&insight.ID, &insight.AudioID, &insight.Text, &insight.CreatedAt); err != nil {
				return err
			}
			insight.UserID = userID
			insights = append(insights, insight)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return insights, nil
}

// DeleteInsight removes one insight. Unknown ids are a no-op.
func (p *Pool) DeleteInsight(ctx context.Context, userID string, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM insights WHERE id = $1 AND user_id = $2`, id, userID)
		return err
	})
}
