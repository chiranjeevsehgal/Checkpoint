# AGENTS.md — Checkpoint

Monorepo with two separate Go modules plus firmware. No root module, no lint config, no Go CI.

- `ingestion-service/` (module `checkpoint/ingestion`) — HTTP API `:8080` + in-process outbox dispatcher. Entrypoints: `cmd/api/main.go`, `cmd/migrate/main.go`.
- `transcription-service/` (module `transcription-service`) — Kafka worker. Entrypoint: `cmd/main.go`, `cmd/migrate/`.
- `firmware/checkpoint/` — PROD ESP32-S3 pendant (Arduino `.ino`). Pins/timings single source: `config.h`.
- `firmware/tests/` — host-side pytest (grep/logic, no hardware). `firmware/host/checkpoint_client/` — Python BLE client.
- `vad-service/` — empty placeholder (`.gitkeep` only). Ignore.
- `loadtest/ingestion-service/` — k6 scripts (`smoke.js`, `spike.js`, `sustained.js`, `arrival-rate.js`).
- Infra: root `docker-compose.yaml` (postgres, kafka KRaft, minio, `*-migrate`, `kafka-init`, services). Docs: `docs/SETUP.md`.

## Setup / run

```bash
copy .env.example .env   # Windows; `cp` on Linux/macOS, then fill values
docker compose up --build
docker compose ps        # postgres + kafka should be healthy
```

- Local dev (from `ingestion-service/`): `make infra-up` → `make migrate` (needs `DATABASE_URL`) → `make run` (API on `:8080`).
- Compose runs `ingestion-migrate` / `transcription-migrate` to `service_completed_successfully` before starting `ingestion-api` / `transcription`. Migrations are goose (`make migrate`, `make status`).
- `POSTGRES_PASSWORD` applies only on first volume init; changing `.env` later is ignored and the TCP-auth healthcheck goes `unhealthy`. Rotate via `make db-rotate-password NEW_PASSWORD=...` or wipe: `make infra-reset` (destructive).
- MinIO uses a named volume (`minio-data`); do not switch to a Windows bind mount (measured p95 ~27s PUTs, hanging listings).
- Postgres stores `TIMESTAMPTZ` (UTC); server display timezone is `Asia/Kolkata` with a `Calcutta→Kolkata` compat shim for old DBeaver/Java clients. Prefer client `TimeZone=Asia/Kolkata`.

## Topic single source of truth (do not break)

`ingestion-service/config.yaml` (`kafka.topic_transcription`, currently `transcription.jobs.v1`) is canonical.

- `KAFKA_TOPIC_TRANSCRIPTION` env must be unset or exactly match it — `config.Load()` fails otherwise.
- `.env.example`, `.env`, and the `docker-compose.yaml` fallback `${KAFKA_TOPIC_TRANSCRIPTION:-...}` must carry the same value.
- Never hardcode a `transcription.*` literal in `internal/config/config.go`; never hardcode `--topic transcription.*` in compose (use the env var).
- Enforced by `TestTopicSingleSourceOfTruth`: `go test ./internal/config/ -run TestTopicSingleSourceOfTruth -v` (from `ingestion-service/`).
- `kafka-init` pre-creates `transcription|embedding|extraction.jobs.v1` plus `.dlq` (6 partitions) so first publish doesn't auto-create with wrong settings.

## Ingestion API quirks

- Flow (Postman collection `ingestion-service/postman.json`, run 1→4): `POST /v1/uploads` → `PUT` bytes directly to MinIO presigned URL → `POST /v1/uploads/<id>/complete` → `GET /v1/uploads/<id>` flips `READY → SUBMITTED` once the outbox dispatcher publishes (~2s).
- Bearer token is the dev user UUID (`aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa` works).
- Uploads are `audio/ogg` only, 10 MB cap — rejected 415/413 before any bytes move. `/complete` verifies MinIO size + type; `checksum_sha256` must be 64-char lowercase hex if sent.
- `Idempotency-Key` retries return the same `upload_id` with a freshly minted 15 min `upload.url`.
- Object layout in bucket `audio`: `{userID}/{YYYY}/{MM}/{DD}/<uploadUUID>`. Buckets `audio`, `temp-audio`, `filtered-audio` are created by `minio-init`.
- `MINIO_ENDPOINT` (API→MinIO, e.g. `minio:9000` in compose) vs `MINIO_PUBLIC_ENDPOINT` (host placed in presigned URLs, e.g. `localhost:9000`). `/health/ready` checks Postgres + MinIO only, not Kafka.
- Outbox dispatcher runs in-process in every API replica (`SKIP LOCKED` claiming) and retries broker errors indefinitely by design.
- `config.Load()` is strict: bad `MINIO_USE_SSL`/`PORT`/negative durations fail fast; production requires `DATABASE_URL`, `MINIO_ACCESS_KEY/SECRET_KEY`, `MINIO_BUCKET`, `KAFKA_BROKERS`. `CONFIG_FILE` overrides the yaml path.

## Transcription worker quirks

- Config: `CONFIG_PATH` (default `config.yaml`, `/app/config.yaml` in container). `TRANSCRIPTION_PROVIDER` env overrides yaml (`elevenlabs` default, `deepgram` alt); the matching `*_API_KEY` env must be set or `Load` fails.
- Consumes `transcription.jobs.v1`, publishes `embedding.jobs.v1` + `extraction.jobs.v1`. Failed messages are deliberately not committed → redelivered on restart.

## Test / verify

Each Go service is its own module — run from the service dir:

```bash
go test ./...          # unit; DB/MinIO integration tests SKIP without env
go vet ./...
go build ./...
go test ./internal/config/ -run TestTopicSingleSourceOfTruth -v   # after any topic/config change
TEST_DATABASE_URL=postgres://... go test ./internal/repository/postgres/ -v   # integration
TEST_MINIO_ENDPOINT=... TEST_MINIO_ACCESS_KEY=... TEST_MINIO_SECRET_KEY=... TEST_MINIO_BUCKET=... go test ./internal/storage/minio/ -v
```

- k6: `k6 run loadtest/ingestion-service/smoke.js` (`BASE_URL` env, default `http://localhost:8080`).
- Firmware host tests: `pytest firmware/tests -v`. BLE client: `pip install bleak cryptography && python firmware/host/checkpoint_client/client.py --bench`.

## Firmware build

- Arduino IDE: open `firmware/checkpoint/checkpoint.ino` (folder must equal `.ino` name). Each `firmware/examples/*/` sketch opens standalone. Edit pins in `firmware/checkpoint/config.h` only.
- CI (`.github/workflows/firmware-build.yml`, only CI in repo) pins: `esp32:esp32@3.3.11`, `ArduinoJson@7.4.3`, `NimBLE-Arduino@2.5.1`, `Adafruit NeoPixel@1.15.5`, `PCMFlow@0.2.1`, `PCMFlowOpus@0.2.0`; FQBN `esp32:esp32:esp32s3:FlashSize=16M,PartitionScheme=app3M_fat9M_16MB,PSRAM=opi,CDCOnBoot=default,USBMode=hwcdc,FlashMode=qio,UploadSpeed=921600`.
