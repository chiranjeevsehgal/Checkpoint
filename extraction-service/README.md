# extraction-service

Kafka-triggered batch worker: `extraction.jobs.v1` → one pending
`extraction_jobs` row per audio → Postgres batch queue → one Groq call per
user batch (`openai/gpt-oss-120b`) returning todos + reminders + insights.

## Run

```bash
go build ./...
go test ./...
CONFIG_PATH=config.yaml go run ./cmd/main.go   # needs GROQ_API_KEY, POSTGRES_DSN
```

## Behavior

Kafka is only the trigger; Postgres is the queue (`pending|processing|done|
skipped|failed`). Batches flush at `batch.size` rows or after
`batch.max_wait_seconds`. LLM output is strictly decoded (unknown fields
fail the batch, retryable) and persisted with a `model@prompt-version` tag
(`internal/extractor.PromptVersion`). Reminder times resolve in the user's
`user_settings.timezone`.

Terminal failures (`batch.max_attempts` exhausted) are handed to the central
retry service, carrying the raw trigger event stored in
`extraction_jobs.source_event`; a retry redelivery resets the failed row to
`pending`/`attempts=0`. The handoff is best-effort: a publish failure leaves
the row `failed` for manual requeue.

## Config

`CONFIG_PATH`, `GROQ_API_KEY`, `POSTGRES_DSN`, `KAFKA_TOPIC_RETRY` (default
`retry.jobs.v1`), `METRICS_ADDR` (default `:9082`). Requeue a failed batch
with `UPDATE extraction_jobs SET status='pending', attempts=0 WHERE ...`.

## Probes

`GET /health/live`, `GET /health/ready`, `GET /metrics`
(`extraction_consumer_*`, `extraction_batches_{ok,failed}_total`).
