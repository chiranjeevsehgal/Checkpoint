# rollup-service

Nightly Postgres summarizer writing English daily/weekly narrative recaps to
`summaries`. No Kafka.

## Run

```bash
go build ./...
go test ./...
CONFIG_PATH=config.yaml go run ./cmd/main.go   # needs GROQ_API_KEY, POSTGRES_DSN
```

## Behavior

Runs once per night inside the `processing` window (or continuously once per
day with the window disabled). Each run refreshes every active user's
previous local day (`user_settings.timezone`, fallback
`summaries.timezone`); `lookback_days` also refreshes recent days for
late-arriving audio. On Monday it backfills the previous Mon–Sun week and
writes the weekly recap, whose prompt records how many of the 7 days had
recordings so partial weeks are explicit. Tombstoned users are skipped. The
nightly gate advances only on success; per-user failures are counted, not
fatal.

## Config

`CONFIG_PATH`, `GROQ_API_KEY`, `POSTGRES_DSN`, `METRICS_ADDR` (default
`:9084`).

## Probes

`GET /health/live`, `GET /health/ready`, `GET /metrics`
(`rollup_runs_{ok,failed}_total`).
