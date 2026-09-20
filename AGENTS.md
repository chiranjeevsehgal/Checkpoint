# AGENTS.md — Checkpoint

Monorepo with five separate Go modules plus firmware and Python services. No root module, no lint config, no Go CI.

- `ingestion-service/` (module `checkpoint/ingestion`) — HTTP API `:8080`, in-process outbox dispatcher, upload cleanup, and account-deletion worker. Entrypoints: `cmd/api/main.go`, `cmd/migrate/main.go`, `cmd/device-admin/main.go`.
- `transcription-service/` (module `transcription-service`) — Kafka worker. Entrypoint: `cmd/main.go`, `cmd/migrate/`.
- `extraction-service/` (module `extraction-service`) — Kafka worker → LLM (Groq) combined todo/reminder/insight extractor. Entrypoints: `cmd/main.go`, `cmd/migrate/`.
- `rollup-service/` (module `rollup-service`) — nightly Postgres summarizer writing English daily/weekly narrative recaps to `summaries`. No Kafka. Entrypoint: `cmd/main.go`, `cmd/migrate/`.
- `notification-service/` (module `notification-service`) — polls Postgres for due reminders and publishes push notifications to self-hosted ntfy. No Kafka. Entrypoint: `cmd/main.go`, `cmd/migrate/`.
- `embedding-service/` — Python Kafka worker, bge-m3 → pgvector. Entrypoint: `app/main.py`, `migrate.py`.
- `mcp-service/` — Python MCP server (official `mcp` SDK + Haystack/TurboVec) over the derived `search_documents` read model; per-account `cp_mcp_` access keys plus an OAuth 2.1 authorization server for Claude/ChatGPT connectors. Entrypoints: `app/main.py`, `migrate.py`. Single worker only (in-process index).
- `firmware/checkpoint/` — PROD ESP32-S3 pendant (Arduino `.ino`). Pins/timings single source: `config.h`.
- `firmware/tests/` — host-side pytest (grep/logic, no hardware). `firmware/host/checkpoint_client/` — Python BLE client.
- `vad-service/` — empty placeholder (`.gitkeep` only). Ignore.
- `loadtest/ingestion-service/` — k6 scripts (`smoke.js`, `spike.js`, `sustained.js`, `arrival-rate.js`).
- `admin-console/` — local-only admin UI. Angular 21 in `web/`, Fastify+TS agent in `server/`. Binds `127.0.0.1:4300`, not in Compose. Details: `admin-console/README.md`.
- Infra: root `docker-compose.yaml` (postgres, kafka KRaft, minio, Kratos + mailpit, ntfy, `*-migrate`, `kafka-init`, services). Kratos config: `infra/kratos/`; ntfy config: `infra/ntfy/server.yml`; Postgres roles/DB bootstrap: `infra/postgres/bootstrap.sh`. Docs: `docs/SETUP.md`.
- CI: `.github/workflows/firmware-build.yml`, `.github/workflows/checkpoint-app-build.yml`, and `.github/workflows/admin-console-build.yml` (three workflows).

## Setup / run

```bash
copy .env.example .env   # Windows; `cp` on Linux/macOS, then fill values
docker compose up --build
docker compose ps        # postgres + kafka should be healthy
```

- Local dev (from `ingestion-service/`): `make infra-up` → `make migrate` (needs `DATABASE_URL`) → `make run` (API on `:8080`).
- Compose runs `ingestion-migrate` / `transcription-migrate` / `embedding-migrate` / `extraction-migrate` / `rollup-migrate` / `notification-migrate` / `mcp-migrate` to `service_completed_successfully` before starting their workers. `mcp-migrate` creates the `checkpoint_mcp` role idempotently (existing volumes included) and then applies the `search_documents` schema. Migrations are goose (`make migrate`, `make status`).
- Postgres init (`infra/postgres/bootstrap.sh`, first volume only) creates roles `checkpoint_request` (`NOBYPASSRLS`), `checkpoint_worker` (`BYPASSRLS`), `checkpoint_mcp` (`NOBYPASSRLS`, read-only MCP) and the separate `kratos` database. Changing those passwords in `.env` later is ignored; wipe with `make infra-reset`.
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
- Pendants: `GET /v1/device`, `POST /v1/device/claim`, `POST /v1/device/release` (release needs re-auth ≤5 min or 403 `REAUTH_REQUIRED`). `POST /v1/uploads` requires `device_id` (32 lowercase hex) owned by the caller. `DELETE /v1/me` records a deletion tombstone (202) purged by the in-process deletion worker, which also purges downstream `transcripts`, `embeddings`, `extraction_jobs` and `todos`; it needs a fresh session or an email verified within 5 min (`HasRecentEmailVerification`).
- `GET/PUT /v1/me/settings` holds per-user `languages` and `timezone` (an IANA id validated with `time.LoadLocation`; unset falls back to the extraction worker's `reminders.timezone`). `PUT` is partial: only the fields present in the body are written, so languages and timezone save independently (`user_settings.timezone` is nullable).
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
- The app auto-detects the device IANA zone (`expo-localization`, `lib/device-timezone.ts`) and silently `PUT`s it to `/v1/me/settings` on sign-in and on app foreground via `AuthSyncBridge`; languages and timezone save independently.
- Signup collects a mandatory `name` (Kratos trait `name` is `required` in `infra/kratos/identity.schema.json`). Changing a password re-authenticates with the current password first, then refreshes the privileged session Kratos needs (`privileged_session_max_age: 5m`); recovery (forgot password) never asks for it.
- The verify-email screen is pinned to the account email; `Use a different email` signs out so a mistyped signup can be redone. Unverified identities are reaped server-side (see ingestion quirks).

## Transcription worker quirks

- Config: `CONFIG_PATH` (default `config.yaml`, `/app/config.yaml` in container). `TRANSCRIPTION_PROVIDER` env overrides yaml (`elevenlabs` default, `deepgram` alt); the matching `*_API_KEY` env must be set or `Load` fails.
- Consumes `transcription.jobs.v1`, publishes `embedding.jobs.v1` + `extraction.jobs.v1`. Failed messages are deliberately not committed → redelivered on restart. Malformed `audio_id`/`user_id` are dropped (committed); events for tombstoned accounts are logged as `stale_deleted_user_event` and dropped.

## Extraction worker quirks

- Kafka is only the trigger: the consumer validates `EXTRACTION_REQUESTED` (schema v2, same envelope transcription publishes), inserts one pending `extraction_jobs` row per audio, commits. Poison → `extraction.jobs.v1.dlq` + commit.
- One combined job per audio (`extraction_type='all'`): a single Groq call returns todos, reminders and insights, and the code guard drops a todo that duplicates a reminder. Postgres is the batch queue: the batcher claims `batch.size` (default 10) rows per user with `FOR UPDATE SKIP LOCKED` → one Groq call per batch (`openai/gpt-oss-120b`, `response_format: json_object`; `groq.temperature`/`top_p`/`reasoning_effort` are sent) → all three output tables replaced per audio in one tx.
- Job states `pending|processing|done|skipped|failed`; a run that extracts nothing is terminal `skipped` (tables still cleared). Retryable failures release back to `pending` with attempts+1, `failed` after `batch.max_attempts` (requeue: `UPDATE ... SET status='pending', attempts=0`). Stuck `processing` rows are reclaimed after `batch.reclaim_after_seconds`.
- Reminder times use the user's `user_settings.timezone` (read per batch; fallback `reminders.timezone`). The model returns a naive local `remind_at` plus an IANA `remind_at_zone` that Go resolves through embedded tzdata; relative expressions resolve against each item's `recorded_at` (rendered in that zone), and a resolved time already in the past is rolled forward to the next day's same clock time, so `remind_at` is always actionable.
- Batches flush once a user has `batch.size` pending rows or the oldest has waited `batch.max_wait_seconds` (default 300). `GROQ_API_KEY` and `POSTGRES_DSN` must be set or the worker fails fast at startup.
- New output types = extend the prompt + `model.Result` + an output table + one migration; consumer/batcher/claim/retry are shared.

## Rollup worker quirks

- `rollup-service/` polls Postgres directly (no Kafka) and runs once per night inside the `processing` window (`window_enabled`, `window_start`/`window_end` "HH:MM" 24h with midnight wrap, `window_timezone` defaulting to `summaries.timezone`, `poll_interval_seconds`). With the window disabled it runs continuously once per day.
- Each run takes every user with a transcript since `now - (lookback_days + 2)`, computes their previous local day from `user_settings.timezone` (fallback `summaries.timezone`), and upserts one English narrative per `(user_id, 'daily', period_start)` from that day's transcripts + todos/reminders/insights. `lookback_days` also refreshes recent days for late-arriving audio. On Monday the look-back widens to 9 days so the weekly run sees the whole previous week.
- On the Monday run it backfills any missing daily recaps for the previous Mon-Sun week, then writes `period='weekly'` from those dailies. Tombstoned users are skipped.
- A run advances its nightly gate only on success, so enumeration failures retry with capped backoff inside the window. Groq calls retry 429/5xx/network up to `groq.max_attempts` (default 3). Transient per-user failures are counted in the run's `failed=` log rather than aborting the whole run.
- Inputs above `summaries.max_input_chars` are map-reduced (chunk recaps, then a merge call). Writes connect as `checkpoint_worker` and own `rollup_schema_version` / the `summaries` table; `GROQ_API_KEY` and `POSTGRES_DSN` must be set or it fails fast.
- Deletion coupling: `summaries` is purged by ingestion's account-deletion worker.

## Notification worker quirks

- `notification-service/` polls Postgres directly (no Kafka) for reminders whose advance fire time (`remind_at - ntfy.advance_seconds`, default 900s) or due time (`remind_at`) has arrived, and POSTs one ntfy message per fire time. `NTFY_TOKEN` and `POSTGRES_DSN` must be set or it fails fast.
- Delivery is claimed in `notification_deliveries`, keyed by `(user_id, audio_id, reminder_text, fire_at, kind)`. Because extraction replaces reminder rows on every batch, the ledger — not the reminder id — is the dedup key; `fire_at` is part of it so a rescheduled reminder becomes a new delivery. Sending is driven from `reminders`, so a deleted reminder never fires.
- Per-user isolation: `GET/POST /v1/me/notifications` mints a server-side random `cp-<base32>` topic stored in `user_notification_settings` (RLS + `app.user_id`), exposed only to its owner. With ntfy auth configured (`NTFY_ADMIN_TOKEN`), ingestion provisions a dedicated ntfy user via the admin API, grants it read-only on exactly that topic, and returns its `tk_` token; the app subscribes with `?auth=`. `DELETE` revokes the ACL/user and rotates the topic. Without the admin token the service falls back to anonymous read-only topics (dev). `infra/ntfy/server.yml` runs `deny-all`; `checkpoint-publisher` is write-only on `cp-*`.
- Catch-up policy: an advance fire time older than `delivery.advance_grace_seconds` is skipped (the due one still fires); a due fire time older than `delivery.max_lateness_seconds` is ignored. Retries use `delivery.max_attempts` with a `reclaim_after_seconds` gate on stale `processing` rows. Rows older than `retention_days` are pruned hourly.
- The Android app subscribes to `wss://<ntfy-host>/<topic>/ws` from its foreground service and shows local notifications via Notifee; it registers/clears the channel through `AuthSyncBridge`. Self-hosted ntfy does not use FCM, so background delivery requires that foreground service.
- Deletion coupling: `user_notification_settings` and `notification_deliveries` are purged by ingestion's account-deletion worker.

## MCP service quirks

- `mcp-service/` exposes retrieval tools over Streamable HTTP on `:1417` (`search`, `timeline`, `list_todos`, `list_reminders`, `get_summaries`, `get_transcript`, `whoami`) for ChatGPT/Claude Code/Cursor. Built on the official `mcp` SDK with a `TokenVerifier`, not `hayhooks mcp run` (that endpoint cannot be auth-wrapped). When OAuth is configured it is also the authorization server via `auth_server_provider` (see below).
- It reads only RLS-protected tables: `search_documents` + `user_settings` as `checkpoint_mcp`, and resolves keys via `mcp_resolve_key()` (SECURITY DEFINER). The indexer writes as `checkpoint_worker` (BYPASSRLS). Every query sets `app.user_id` per transaction, so a key can never read another account's rows.
- `search_documents` is a **derived read model** rebuilt from `transcripts`/`embeddings`/`todos`/`reminders`/`insights`/`summaries`; source tables stay authoritative. Transcript chunk vectors are copied from `embeddings`; structured text is newly embedded with bge-m3. Full transcript text is stored with no embedding and served by `get_transcript`.
- Access keys: `cp_mcp_<base32>`, SHA-256 at rest in `mcp_access_keys` (ingestion, RLS). Mint/list/revoke via `GET/POST /v1/me/mcp-keys` and `DELETE /v1/me/mcp-keys/{id}` behind Kratos auth; the secret is returned only on create. `last_used_at` is updated by the resolve function.
- Index lifecycle: one global in-process TurboVec index (4-bit, cosine), built from `search_documents` at startup and refreshed on a ~30 s poll (`MCP_POLL_SECONDS`) that rebuilds any changed audio wholesale and embeds pending rows. TurboVec is single-process: **run exactly one replica/worker**.
- The index watches `embeddings.created_at` too, so chunks that land after the transcript are picked up. Out-of-band `search_documents` deletes (account deletion) are only reflected on restart/rebuild — there is no live reconciliation.
- In-place `embeddings` re-embeds (`ON CONFLICT` updates) keep their original `created_at`, so they do not trigger an index rebuild.
- Timestamps are returned twice (UTC and the user's IANA zone, from `user_settings.timezone` falling back to `MCP_FALLBACK_TIMEZONE`); `occurred_at = coalesce(recorded_at, created_at)` and `recorded_at` is exposed separately (nullable = pendant clock unsynced).
- Deletion coupling: `search_documents`, `mcp_access_keys`, `oauth_tokens` and `oauth_authorization_codes` are purged by ingestion's account-deletion worker.
- Auth is static `cp_mcp_` bearer keys for CLI/desktop clients **plus** OAuth 2.1 for hosted clients (Claude web/desktop/mobile, ChatGPT web). `build_server` passes `auth_server_provider=CheckpointOAuthProvider` when `MCP_OAUTH_SESSION_SECRET` and `KRATOS_PUBLIC_URL` are set, and otherwise falls back to `token_verifier=KeyTokenVerifier`. `CheckpointOAuthProvider.load_access_token` accepts both key and OAuth access (`cp_oauth_`) tokens, so tools still read the account id from `client_id`.
- OAuth routes are served by the SDK (`/.well-known/oauth-authorization-server`, `/.well-known/oauth-protected-resource`, `/register` DCR, `/authorize`, `/token`, `/revoke`); `/login` and `/consent` are custom routes (`app/oauth_web.py`) that drive the Kratos native login API and require a verified email. One scope: `checkpoint`. Access tokens 1h, refresh 30d rotated; codes and tokens are stored SHA-256 at rest in `oauth_*` (mcp-service migration `00002_create_oauth.sql`).
- TLS terminates at the `caddy` compose service for `160-236-239-95.sslip.io` (`infra/caddy/Caddyfile`), which proxies every path to `mcp-service:1417`. `MCP_PUBLIC_URL` must be that HTTPS origin; `BIND_MCP` stays loopback, so the bare `:1417` is no longer public. OAuth redirect hosts are restricted by `MCP_OAUTH_ALLOWED_REDIRECT_HOSTS` (default `claude.ai,chatgpt.com` plus loopback).

## Test / verify

Each Go service is its own module — run from the service dir:

```bash
go test ./...          # unit; DB/MinIO integration tests SKIP without env
go vet ./...
go build ./...
go test ./internal/config/ -run TestTopicSingleSourceOfTruth -v   # after any topic/config change
TEST_DATABASE_URL=postgres://... go test ./internal/repository/postgres/ -v   # integration
TEST_MINIO_ENDPOINT=... TEST_MINIO_ACCESS_KEY=... TEST_MINIO_SECRET_KEY=... TEST_MINIO_BUCKET=... go test ./internal/storage/minio/ -v
TEST_DATABASE_URL=postgres://... go test ./internal/storage/ -v   # notification-service integration
TEST_DATABASE_URL=postgres://... TEST_MCP_DATABASE_URL=postgres://checkpoint_mcp:... python -m unittest discover -s tests -v   # mcp-service integration (from mcp-service/)
```

- k6: `k6 run loadtest/ingestion-service/smoke.js` (`BASE_URL` env, default `http://localhost:8080`).
- Firmware host tests: `pytest firmware/tests -v`. BLE client: `pip install bleak cryptography && python -m client_app --cli --session-token <kratos> --device-id <32hex>` (from `firmware/host/checkpoint_client`).
- Embedding: `cd embedding-service && python -m unittest discover -s tests -v`.
- MCP service: `cd mcp-service && python -m unittest discover -s tests -v` (pure tests run without ML deps; DB tests skip without `TEST_DATABASE_URL`). MCP end-to-end needs the stack up: mint a key via `POST /v1/me/mcp-keys`, then `claude mcp add --transport http checkpoint https://160-236-239-95.sslip.io/mcp --header "Authorization: Bearer cp_mcp_..."`, or add `https://160-236-239-95.sslip.io/mcp` as an OAuth connector in Claude/ChatGPT.
- Device provisioning CLI: `DATABASE_URL=... go run ./cmd/device-admin provision -device <32hex> -claim-hash <64hex>` (also `status`, `unquarantine`, `list`, `deletions`, `delete-account`). The pendant emits the hash over USB with `auth provision`.
- App: `cd checkpoint-app && npm run check && npm test`.
- Admin console: `cd admin-console && npm --prefix server test && npm --prefix web test`; dev `npm run dev` (Angular :4200 + agent :4300), build `npm run build`, run `npm start`.

## Firmware build

- Arduino IDE: open `firmware/checkpoint/checkpoint.ino` (folder must equal `.ino` name). Each `firmware/examples/*/` sketch opens standalone. Edit pins in `firmware/checkpoint/config.h` only.
- CI (`.github/workflows/firmware-build.yml`, only CI in repo) pins: `esp32:esp32@3.3.11`, `ArduinoJson@7.4.3`, `NimBLE-Arduino@2.5.1`, `Adafruit NeoPixel@1.15.5`, `PCMFlow@0.2.1`, `PCMFlowOpus@0.2.0`; FQBN `esp32:esp32:esp32s3:FlashSize=16M,PartitionScheme=app3M_fat9M_16MB,PSRAM=opi,CDCOnBoot=default,USBMode=hwcdc,FlashMode=qio,UploadSpeed=921600`.
