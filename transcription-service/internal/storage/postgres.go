package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"transcription-service/internal/model"
)

// ErrOwnershipConflict means an existing transcript row belongs to a
// different user than the event. The row must not be overwritten.
var ErrOwnershipConflict = errors.New("transcript ownership conflict")

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (p *PostgresStore) Close() {
	p.pool.Close()
}

// IsUserDeleting reports whether a deletion tombstone exists for the user.
// A missing table means downstream runs against a separate database, so
// there is nothing to gate on.
func (p *PostgresStore) IsUserDeleting(ctx context.Context, userID string) (bool, error) {
	var deleting bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM account_deletions WHERE user_id = $1::uuid)`, userID).Scan(&deleting)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return false, nil
		}
		return false, err
	}
	return deleting, nil
}

func (p *PostgresStore) SaveTranscript(ctx context.Context, t *model.TranscriptResult) error {
	segments, err := json.Marshal(t.SpeakerSegments)
	if err != nil {
		return fmt.Errorf("marshalling speaker segments: %w", err)
	}

	tag, err := p.pool.Exec(ctx, `
		INSERT INTO transcripts (audio_id, user_id, text, language, duration_seconds, speaker_segments, provider, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (audio_id) DO UPDATE SET
			text = EXCLUDED.text,
			language = EXCLUDED.language,
			duration_seconds = EXCLUDED.duration_seconds,
			speaker_segments = EXCLUDED.speaker_segments,
			provider = EXCLUDED.provider,
			request_id = EXCLUDED.request_id
		WHERE transcripts.user_id = EXCLUDED.user_id`,
		t.AudioID, t.UserID, t.Text, t.Language, t.DurationSeconds, segments, t.Provider, t.RequestID,
	)
	if err != nil {
		return fmt.Errorf("saving transcript: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: audio_id=%s", ErrOwnershipConflict, t.AudioID)
	}
	return nil
}