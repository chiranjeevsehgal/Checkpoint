# retry-service

Central delayed-retry Kafka worker: consumes `RETRY_REQUESTED` handoffs from
`retry.jobs.v1` and re-publishes each original event to its source topic after
a configurable delay.

## Run

```bash
go build ./...
go test ./...
CONFIG_PATH=config.yaml go run ./cmd/main.go   # needs POSTGRES_DSN
```

## Behavior

The delay is Postgres-scheduled, never an in-memory sleep: the consumer
upserts one `retry_jobs` row per `(source_topic, original_event_id)` with
`next_attempt_at = now() + retry.delay_seconds`, and the dispatcher claims due
rows with `FOR UPDATE SKIP LOCKED` and re-publishes the original payload to its
source topic (`original_payload` is BYTEA so bytes survive unmodified).

`attempts` counts completed re-deliveries, incremented only after a successful
publish. A handoff for a `dispatched` row re-arms it while
`attempts < retry.max_attempts`; once exhausted the row is terminal `failed`
with `last_stage`/`last_error` and a per-handoff `attempt_log`. A handoff for a
`pending`/`processing` row only appends to the log (the schedule stands), and a
handoff for a terminal row is ignored. Broker failures at dispatch release the
claim with the schedule untouched, so the next poll retries. Tombstoned
accounts are marked `skipped`, never re-delivered. Poison on the retry topic
goes to `retry.jobs.v1.dlq` as `RETRY_FAILED`.

## Config

`CONFIG_PATH` (default `config.yaml`), `POSTGRES_DSN`, `KAFKA_BROKERS`,
`KAFKA_TOPIC_RETRY` (default `retry.jobs.v1`), `RETRY_DELAY_SECONDS` (default
1800), `RETRY_MAX_ATTEMPTS` (default 2). Queue cadences
(`poll_interval_seconds`, `batch_size`, `reclaim_after_seconds`,
`retention_days`) live in `config.yaml`.
