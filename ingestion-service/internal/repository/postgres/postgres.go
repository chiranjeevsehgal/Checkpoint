// Package postgres implements repository interfaces on PostgreSQL via pgx.
package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps the pgx connection pool with service-wide settings.
type Pool struct {
	inner *pgxpool.Pool
}

// NewPool opens a pool sized for a lightweight API service. With a few
// replicas each holding at most 20 connections, total usage stays well
// under typical PostgreSQL defaults.
func NewPool(ctx context.Context, databaseURL string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 20
	cfg.MinConns = 5
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Pool{inner: pool}, nil
}

// Ping checks database reachability for the readiness probe.
func (p *Pool) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return p.inner.Ping(ctx)
}

// Close drains the pool. Called last during graceful shutdown.
func (p *Pool) Close() {
	p.inner.Close()
}

// WithUserTx runs fn in a transaction whose RLS user context is userID.
// set_config(..., true) is transaction-local, so the value cannot leak
// when pgx returns the connection to the pool.
func (p *Pool) WithUserTx(ctx context.Context, userID string, fn func(pgx.Tx) error) error {
	tx, err := p.inner.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, userID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
