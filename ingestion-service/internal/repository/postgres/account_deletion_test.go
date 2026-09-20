package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestPurgeDownstreamRemovesExtractionData guards the account-deletion purge
// against the extraction service's tables. Only rows with a random UUID are
// inserted/deleted, so the local dev database is safe to use.
func TestPurgeDownstreamRemovesExtractionData(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	userID := uuid.NewString()
	audioID := uuid.NewString()

	t.Cleanup(func() {
		for _, table := range []string{"transcripts", "embeddings", "extraction_jobs", "todos", "reminders", "insights", "summaries", "oauth_tokens", "oauth_authorization_codes"} {
			_, _ = p.inner.Exec(ctx, `DELETE FROM `+table+` WHERE user_id = $1`, userID)
		}
	})

	if hasTable(t, p, "transcripts") {
		if _, err := p.inner.Exec(ctx,
			`INSERT INTO transcripts (audio_id, user_id, text) VALUES ($1, $2, 't')`,
			audioID, userID); err != nil {
			t.Fatalf("seed transcripts: %v", err)
		}
	}
	if hasTable(t, p, "embeddings") {
		if _, err := p.inner.Exec(ctx,
			`INSERT INTO embeddings (user_id, audio_id, chunk_index, chunk_text, model, embedding)
			 VALUES ($1, $2, 0, 't', 'test', array_fill(0.0::real, ARRAY[1024])::vector)`,
			userID, audioID); err != nil {
			t.Fatalf("seed embeddings: %v", err)
		}
	}
	if hasTable(t, p, "extraction_jobs") {
		if _, err := p.inner.Exec(ctx,
			`INSERT INTO extraction_jobs (user_id, audio_id, extraction_type, text)
			 VALUES ($1, $2, 'todo', 't')`,
			userID, audioID); err != nil {
			t.Fatalf("seed extraction_jobs: %v", err)
		}
	}
	if hasTable(t, p, "todos") {
		if _, err := p.inner.Exec(ctx,
			`INSERT INTO todos (user_id, audio_id, text, model) VALUES ($1, $2, 't', 'test')`,
			userID, audioID); err != nil {
			t.Fatalf("seed todos: %v", err)
		}
	}
	if hasTable(t, p, "reminders") {
		if _, err := p.inner.Exec(ctx,
			`INSERT INTO reminders (user_id, audio_id, text, model) VALUES ($1, $2, 't', 'test')`,
			userID, audioID); err != nil {
			t.Fatalf("seed reminders: %v", err)
		}
	}
	if hasTable(t, p, "insights") {
		if _, err := p.inner.Exec(ctx,
			`INSERT INTO insights (user_id, audio_id, text, model) VALUES ($1, $2, 't', 'test')`,
			userID, audioID); err != nil {
			t.Fatalf("seed insights: %v", err)
		}
	}
	if hasTable(t, p, "summaries") {
		if _, err := p.inner.Exec(ctx,
			`INSERT INTO summaries (user_id, period, period_start, text, model)
			 VALUES ($1, 'daily', CURRENT_DATE, 't', 'test')`,
			userID); err != nil {
			t.Fatalf("seed summaries: %v", err)
		}
	}
	if hasTable(t, p, "oauth_tokens") {
		if _, err := p.inner.Exec(ctx,
			`INSERT INTO oauth_tokens (token_hash, kind, family_id, user_id, client_id, scopes, expires_at)
			 VALUES ($1, 'access', $2, $3, 'test-client', 'checkpoint', NOW() + interval '1 hour')`,
			[]byte("token-hash"), uuid.NewString(), userID); err != nil {
			t.Fatalf("seed oauth_tokens: %v", err)
		}
	}

	if err := p.PurgeDownstream(ctx, userID); err != nil {
		t.Fatalf("purge: %v", err)
	}

	for _, table := range []string{"transcripts", "embeddings", "extraction_jobs", "todos", "reminders", "insights", "summaries", "oauth_tokens"} {
		if !hasTable(t, p, table) {
			continue
		}
		var n int
		if err := p.inner.QueryRow(ctx,
			`SELECT COUNT(*) FROM `+table+` WHERE user_id = $1`, userID).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Fatalf("%s still has %d rows for purged user", table, n)
		}
	}
}

func hasTable(t *testing.T, p *Pool, name string) bool {
	t.Helper()
	var exists bool
	if err := p.inner.QueryRow(context.Background(),
		`SELECT to_regclass($1) IS NOT NULL`, "public."+name).Scan(&exists); err != nil {
		t.Fatalf("checking table %s: %v", name, err)
	}
	return exists
}
