package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetLanguages returns the user's selected languages, or an empty slice when
// no settings row exists yet.
func (p *Pool) GetLanguages(ctx context.Context, userID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	languages := []string{}
	err := p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT languages FROM user_settings WHERE user_id = $1`, userID).Scan(&languages)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return languages, nil
}

// SetLanguages upserts the user's selection. The last write wins.
func (p *Pool) SetLanguages(ctx context.Context, userID string, languages []string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.WithUserTx(ctx, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO user_settings (user_id, languages, updated_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (user_id) DO UPDATE
			SET languages = EXCLUDED.languages, updated_at = NOW()`, userID, languages)
		return err
	})
}
