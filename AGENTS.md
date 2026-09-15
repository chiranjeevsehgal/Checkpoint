# AGENTS.md — Checkpoint

Monorepo with two separate Go modules plus firmware. No root module, no lint config, no Go CI.

- `ingestion-service/` (module `checkpoint/ingestion`) — HTTP API `:8080`, in-process outbox dispatcher, upload cleanup, and account-deletion worker. Entrypoints: `cmd/api/main.go`, `cmd/migrate/main.go`, `cmd/device-admin/main.go`.
- `transcription-service/` (module `transcription-service`) — Kafka worker. Entrypoint: `cmd/main.go`, `cmd/migrate/`.
- `embedding-service/` — Python Kafka worker (pgvector). Migrations: `migrate.py up`. Omitted from older docs but in Compose.
- `firmware/checkpoint/` — PROD ESP32-S3 pendant (Arduino `.ino`). Pins/timings single source: `config.h`.
- `firmware/tests/` — host-side pytest (grep/logic, no hardware). `firmware/host/checkpoint_client/` — Python BLE client.
- `vad-service/` — empty placeholder (`.gitkeep` only). Ignore.
- `loadtest/ingestion-service/` — k6 scripts (`smoke.js`, `spike.js`, `sustained.js`, `arrival-rate.js`).
- `admin-console/` — local-only admin UI. Angular 21 in `web/`, Fastify+TS agent in `server/`. Binds `127.0.0.1:4300`, not in Compose. Details: `admin-console/README.md`.
- Infra: root `docker-compose.yaml` (postgres, kafka KRaft, minio, Kratos + mailpit, `*-migrate`, `kafka-init`, services). Kratos config: `infra/kratos/`; Postgres roles/DB bootstrap: `infra/postgres/bootstrap.sh`. Docs: `docs/SETUP.md`.
- CI: `.github/workflows/firmware-build.yml`, `.github/workflows/checkpoint-app-build.yml`, and `.github/workflows/admin-console-build.yml` (three workflows).

## Setup / run

```bash
copy .env.example .env   # Windows; `cp` on Linux/macOS, then fill values
docker compose up --build
docker compose ps        # postgres + kafka should be healthy
```

- Local dev (from `ingestion-service/`): `make infra-up` → `make migrate` (needs `DATABASE_URL`) → `make run` (API on `:8080`).
- Compose runs `ingestion-migrate` / `transcription-migrate` / `embedding-migrate` to `service_completed_successfully` before starting their workers. Migrations are goose (`make migrate`, `make status`).
- Postgres init (`infra/postgres/bootstrap.sh`, first volume only) creates role `checkpoint_request` (`NOBYPASSRLS`), `checkpoint_worker` (`BYPASSRLS`) and the separate `kratos` database. Changing those passwords in `.env` later is ignored; wipe with `make infra-reset`.
- Migrations `00007`–`00009` add `uploads.device_id NOT NULL` and device-aware idempotency, so an old dev volume must be reset (data is disposable).
- Kratos public API is `http://localhost:4433`; the admin API is bridge-only (never published). Mailpit UI is `http://localhost:8025`.
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
- Auth is an opaque Ory Kratos session token: `Authorization: Bearer <token>`. `internal/auth` validates it via `GET /sessions/whoami` (`X-Session-Token`); `identity.id` becomes `Principal.UserID`. There is no UUID-bearer shortcut. 401 = invalid session, 403 `VERIFICATION_REQUIRED` = unverified email, 503 `AUTH_UNAVAILABLE` = Kratos unreachable (never a logout).
- Pendants: `GET /v1/device`, `POST /v1/device/claim`, `POST /v1/device/release` (release needs re-auth ≤5 min or 403 `REAUTH_REQUIRED`). `POST /v1/uploads` requires `device_id` (32 lowercase hex) owned by the caller. `DELETE /v1/me` records a deletion tombstone (202) purged by the in-process deletion worker; it needs a fresh session or an email verified within 5 min (`HasRecentEmailVerification`).
- Request/worker DB pools: `DATABASE_REQUEST_URL` (RLS-enforced `checkpoint_request`) for handlers, `DATABASE_WORKER_URL` (`checkpoint_worker`) for outbox/cleanup/deletion. Request transactions set `app.user_id` with `set_config(..., true)`; RLS policies on `uploads`, `idempotency_keys`, `devices`, `account_deletions`.
- Uploads are `audio/ogg` only, 10 MB cap — rejected 415/413 before any bytes move. `/complete` verifies MinIO size + type; `checksum_sha256` must be 64-char lowercase hex if sent.
- `Idempotency-Key` retries return the same `upload_id` with a freshly minted 15 min `upload.url`.
- Object layout in bucket `audio`: `{userID}/{YYYY}/{MM}/{DD}/<uploadUUID>`. Buckets `audio`, `temp-audio`, `filtered-audio` are created by `minio-init`.
- `MINIO_ENDPOINT` (API→MinIO, e.g. `minio:9000` in compose) vs `MINIO_PUBLIC_ENDPOINT` (host placed in presigned URLs, e.g. `localhost:9000`). `/health/ready` checks Postgres + MinIO only, not Kafka.
- Outbox dispatcher runs in-process in every API replica (`SKIP LOCKED` claiming) and retries broker errors indefinitely by design.
- `config.Load()` is strict: bad `MINIO_USE_SSL`/`PORT`/negative durations fail fast; production requires `DATABASE_URL`, `DATABASE_REQUEST_URL`, `DATABASE_WORKER_URL`, `KRATOS_PUBLIC_URL`, `MINIO_ACCESS_KEY/SECRET_KEY`, `MINIO_BUCKET`, `KAFKA_BROKERS`. In development the request/worker URLs fall back to `DATABASE_URL`. `CONFIG_FILE` overrides the yaml path.
- Unverified identities are reaped: the `identitycleanup` worker deletes identities with no completed email verification older than `IDENTITY_TTL_HOURS` (default 1h) every `IDENTITY_CLEANUP_INTERVAL_MINUTES` (default 15m), sweeping once at startup. Kratos writes the row at registration, so this bounds abandoned/mistyped signups.

## Native app quirks (`checkpoint-app/`)

- Auth is Kratos-native, no SDK: `features/auth/kratos-client.ts` over a small `KratosTransport` (raw fetch). Session (`sessionToken` + `identityId`) lives in `lib/session` on SecureStore (native only; web auth out of scope).
- Auth states: `loading | anonymous | unverified | authenticated | unavailable | deleting`. 401 clears the session, 503 keeps it (never auto-logout); 403 `VERIFICATION_REQUIRED` routes to verify-email.
- `EXPO_PUBLIC_KRATOS_URL` (default localhost:4433, LAN IP for hardware) — same resolution rules as `EXPO_PUBLIC_API_URL` in `lib/env.ts`.
- Uploads send `Bearer <session token>` and `device_id`; the app claims the pendant via `GET/POST /v1/device*` after BLE enrollment. BLE credentials are namespaced `Checkpoint.<identityId>.<deviceId>`.
- Forget is local-only; Release = fresh re-auth + BLE erase + clear trusted slots + cloud release. Test glob is `src/**/__tests__/*.test.ts` (`npm test`).
- Sign-out, account deletion and 401 session loss wipe local recordings: `AuthSyncBridge` calls `syncEngine.clearLocalData()`, deleting `document/checkpoint/` (received audio, part files, `transfers.json`). A transient `unavailable` status never clears.
- Signup collects a mandatory `name` (Kratos trait `name` is `required` in `infra/kratos/identity.schema.json`). Changing a password re-authenticates with the current password first, then refreshes the privileged session Kratos needs (`privileged_session_max_age: 5m`); recovery (forgot password) never asks for it.
- The verify-email screen is pinned to the account email; `Use a different email` signs out so a mistyped signup can be redone. Unverified identities are reaped server-side (see ingestion quirks).

## Transcription worker quirks

- Config: `CONFIG_PATH` (default `config.yaml`, `/app/config.yaml` in container). `TRANSCRIPTION_PROVIDER` env overrides yaml (`elevenlabs` default, `deepgram` alt); the matching `*_API_KEY` env must be set or `Load` fails.
- Consumes `transcription.jobs.v1`, publishes `embedding.jobs.v1` + `extraction.jobs.v1`. Failed messages are deliberately not committed → redelivered on restart. Malformed `audio_id`/`user_id` are dropped (committed); events for tombstoned accounts are logged as `stale_deleted_user_event` and dropped.

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
- Firmware host tests: `pytest firmware/tests -v`. BLE client: `pip install bleak cryptography && python -m client_app --cli --session-token <kratos> --device-id <32hex>` (from `firmware/host/checkpoint_client`).
- Embedding: `cd embedding-service && python -m unittest discover -s tests -v`.
- Device provisioning CLI: `DATABASE_URL=... go run ./cmd/device-admin provision -device <32hex> -claim-hash <64hex>` (also `status`, `unquarantine`, `list`, `deletions`, `delete-account`). The pendant emits the hash over USB with `auth provision`.
- App: `cd checkpoint-app && npm run check && npm test`.
- Admin console: `cd admin-console && npm --prefix server test && npm --prefix web test`; dev `npm run dev` (Angular :4200 + agent :4300), build `npm run build`, run `npm start`.

## Firmware build

- Arduino IDE: open `firmware/checkpoint/checkpoint.ino` (folder must equal `.ino` name). Each `firmware/examples/*/` sketch opens standalone. Edit pins in `firmware/checkpoint/config.h` only.
- CI (`.github/workflows/firmware-build.yml`, only CI in repo) pins: `esp32:esp32@3.3.11`, `ArduinoJson@7.4.3`, `NimBLE-Arduino@2.5.1`, `Adafruit NeoPixel@1.15.5`, `PCMFlow@0.2.1`, `PCMFlowOpus@0.2.0`; FQBN `esp32:esp32:esp32s3:FlashSize=16M,PartitionScheme=app3M_fat9M_16MB,PSRAM=opi,CDCOnBoot=default,USBMode=hwcdc,FlashMode=qio,UploadSpeed=921600`.
