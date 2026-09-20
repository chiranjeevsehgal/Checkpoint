# notification-service

Polls Postgres for due reminders and publishes push notifications to
self-hosted ntfy. No Kafka.

## Run

```bash
go build ./...
go test ./...
CONFIG_PATH=config.yaml go run ./cmd/main.go   # needs NTFY_TOKEN, POSTGRES_DSN
```

## Behavior

Each poll delivers reminders whose advance fire time (`remind_at` −
`ntfy.advance_seconds`) or due time has arrived. Delivery is claimed in
`notification_deliveries` keyed by `(user, audio, text, fire_at, kind)`, so a
rescheduled reminder becomes a new delivery and a deleted reminder never
fires. Tombstoned users are skipped. An advance older than
`delivery.advance_grace_seconds` is skipped (the due still fires); a due
older than `delivery.max_lateness_seconds` is ignored. `MaxAttempts`
exhaustion marks the row `failed` (inspect manually, then requeue by
resetting it to `pending`). Rows older than `retention_days` are pruned
hourly.

## Config

`CONFIG_PATH`, `NTFY_TOKEN`, `POSTGRES_DSN`, `METRICS_ADDR` (default
`:9083`).

## Probes

`GET /health/live`, `GET /health/ready`, `GET /metrics`
(`notification_deliveries_{ok,failed}_total`).
