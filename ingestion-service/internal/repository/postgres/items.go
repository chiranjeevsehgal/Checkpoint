package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"checkpoint/ingestion/internal/repository"
)

// mapItemWriteError translates a write failure into a domain error the HTTP
// layer can map: a missing row or a duplicate text for the same recording.
func mapItemWriteError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.ErrItemNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return repository.ErrItemConflict
	}
	return err
}

// ListTodos returns the user's to-dos, open first then newest, plus the total.
func (p *Pool) ListTodos(ctx context.Context, userID, status string, limit, offset int) ([]repository.Todo, int, error) {
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
	total := 0
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, audio_id::text, text, is_done, recorded_at, created_at, COUNT(*) OVER () AS total
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
			if err := rows.Scan(&todo.ID, &todo.AudioID, &todo.Text, &todo.IsDone, &recordedAt, &todo.CreatedAt, &total); err != nil {
				return err
			}
			todo.UserID = userID
			todo.RecordedAt = timeFromPg(recordedAt)
			todos = append(todos, todo)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	return todos, total, nil
}

// UpdateTodo edits one to-do and returns it.
func (p *Pool) UpdateTodo(ctx context.Context, userID string, id int64, update repository.TodoUpdate) (*repository.Todo, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var todo repository.Todo
	var recordedAt pgtype.Timestamptz
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			UPDATE todos SET text = COALESCE($3, text), is_done = COALESCE($4, is_done)
			WHERE id = $1 AND user_id = $2
			RETURNING id, audio_id::text, text, is_done, recorded_at, created_at`,
			id, userID, update.Text, update.IsDone).
			Scan(&todo.ID, &todo.AudioID, &todo.Text, &todo.IsDone, &recordedAt, &todo.CreatedAt)
	})
	if err != nil {
		return nil, mapItemWriteError(err)
	}
	todo.UserID = userID
	todo.RecordedAt = timeFromPg(recordedAt)
	return &todo, nil
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

// ListReminders returns the user's reminders for the given window, plus total.
func (p *Pool) ListReminders(ctx context.Context, userID, window string, limit, offset int) ([]repository.Reminder, int, error) {
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
	total := 0
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, audio_id, text, remind_at, important, created_at, COUNT(*) OVER () AS total
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
			if err := rows.Scan(&reminder.ID, &reminder.AudioID, &reminder.Text, &remindAt, &reminder.Important, &reminder.CreatedAt, &total); err != nil {
				return err
			}
			reminder.UserID = userID
			reminder.RemindAt = timeFromPg(remindAt)
			reminders = append(reminders, reminder)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	return reminders, total, nil
}

// UpdateReminder edits one reminder and returns it.
func (p *Pool) UpdateReminder(ctx context.Context, userID string, id int64, update repository.ReminderUpdate) (*repository.Reminder, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var reminder repository.Reminder
	var remindAt pgtype.Timestamptz
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			UPDATE reminders SET
				text      = COALESCE($3, text),
				remind_at = CASE WHEN $4 THEN $5 ELSE remind_at END,
				important = COALESCE($6, important)
			WHERE id = $1 AND user_id = $2
			RETURNING id, audio_id, text, remind_at, important, created_at`,
			id, userID, update.Text, update.RemindAtSet, update.RemindAt, update.Important).
			Scan(&reminder.ID, &reminder.AudioID, &reminder.Text, &remindAt, &reminder.Important, &reminder.CreatedAt)
	})
	if err != nil {
		return nil, mapItemWriteError(err)
	}
	reminder.UserID = userID
	reminder.RemindAt = timeFromPg(remindAt)
	return &reminder, nil
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

// ListInsights returns the user's insights, newest first, plus the total.
func (p *Pool) ListInsights(ctx context.Context, userID string, limit, offset int) ([]repository.Insight, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	insights := []repository.Insight{}
	total := 0
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, audio_id, text, created_at, COUNT(*) OVER () AS total
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
			if err := rows.Scan(&insight.ID, &insight.AudioID, &insight.Text, &insight.CreatedAt, &total); err != nil {
				return err
			}
			insight.UserID = userID
			insights = append(insights, insight)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	return insights, total, nil
}

// UpdateInsight edits one insight's text and returns it.
func (p *Pool) UpdateInsight(ctx context.Context, userID string, id int64, text string) (*repository.Insight, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var insight repository.Insight
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			UPDATE insights SET text = $3
			WHERE id = $1 AND user_id = $2
			RETURNING id, audio_id, text, created_at`, id, userID, text).
			Scan(&insight.ID, &insight.AudioID, &insight.Text, &insight.CreatedAt)
	})
	if err != nil {
		return nil, mapItemWriteError(err)
	}
	insight.UserID = userID
	return &insight, nil
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
