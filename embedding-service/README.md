# embedding-service

Python Kafka worker: `embedding.jobs.v1` → bge-m3 chunk embeddings →
pgvector. Batches up to `EMBEDDING_BATCH_MAX` events into one encode call;
saves are idempotent upserts, offsets commit only after the batch stores.

## Run

```bash
python -m unittest discover -s tests -v
python -u -m app.main   # needs DATABASE_URL, KAFKA_BROKERS
```

## Behavior

Tombstoned accounts are dropped (`stale_deleted_user_event`). Poison events
go to `<topic>.dlq`. Anything else retries with `EMBEDDING_RETRY_BACKOFFS`
(default `1,5,15,60` seconds) and rewinds the batch offsets; once those are
exhausted the event is handed to the central retry service. A missing
`account_deletions` table disables the tombstone check with a one-time
warning (split-database setups).

## Config (env)

`DATABASE_URL`, `KAFKA_BROKERS`, `KAFKA_TOPIC_EMBEDDING`,
`KAFKA_TOPIC_RETRY` (default `retry.jobs.v1`), `KAFKA_CONSUMER_GROUP`,
`EMBEDDING_MODEL`, `EMBEDDING_CHUNK_TOKENS/_OVERLAP`,
`EMBEDDING_BATCH_MAX` (default 8), `EMBEDDING_RETRY_BACKOFFS`,
`METRICS_ADDR`/`METRICS_PORT` (default `0.0.0.0:9085`).

## Probes

`GET /health/live`, `GET /health/ready`, `GET /metrics`
(`embedding_events_{ok,failed,stale,invalid}_total`).
