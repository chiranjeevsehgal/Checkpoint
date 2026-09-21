# transcription-service

Kafka worker: `transcription.jobs.v1` → transcript → publishes
`embedding.jobs.v1` + `extraction.jobs.v1`.

## Run

```bash
go build ./...
go test ./...
CONFIG_PATH=config.yaml go run ./cmd/main.go
```

## Behavior

Provider abstraction over ElevenLabs (default) and Deepgram; retry budget is
`providers.<name>.max_attempts` in `config.yaml`
(`TRANSCRIPTION_PROVIDER` overrides the provider, matching `*_API_KEY` env
must be set). A transient failure is handed to the central retry service
(`KAFKA_TOPIC_RETRY`, default `retry.jobs.v1`) and the message committed; a
handoff-publish failure leaves the message uncommitted for redelivery.
Malformed ids and tombstoned accounts are dropped (committed).

## Config

`CONFIG_PATH` (default `config.yaml`), `TRANSCRIPTION_PROVIDER`,
`ELEVENLABS_API_KEY`/`DEEPGRAM_API_KEY`, `POSTGRES_DSN`, `KAFKA_TOPIC_RETRY`
(default `retry.jobs.v1`), MinIO + Kafka settings in yaml, `METRICS_ADDR`
(default `:9081`).

## Probes

`GET /health/live`, `GET /health/ready`, `GET /metrics`
(`transcription_processed_{ok,failed,stale,omitted,invalid}_total`).
