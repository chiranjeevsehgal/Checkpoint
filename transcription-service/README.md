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
must be set). Failed messages are deliberately not committed → redelivered
on restart. Malformed ids and tombstoned accounts are dropped (committed).

## Config

`CONFIG_PATH` (default `config.yaml`), `TRANSCRIPTION_PROVIDER`,
`ELEVENLABS_API_KEY`/`DEEPGRAM_API_KEY`, `POSTGRES_DSN`, MinIO + Kafka
settings in yaml, `METRICS_ADDR` (default `:9081`).

## Probes

`GET /health/live`, `GET /health/ready`, `GET /metrics`
(`transcription_processed_{ok,failed,stale,omitted,invalid}_total`).
