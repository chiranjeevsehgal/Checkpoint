# Runbook

Incident response for the Checkpoint stack. Onboarding is in `SETUP.md` /
`DEVELOPER_SETUP.md`; this covers "it's broken, what now". All services log
JSON to stdout (`LOG_LEVEL=debug` for detail); Grafana `Checkpoint / Logs`
filters by `service`, `audio_id`, `user_id`, `request_id`.

## Outbox backlog (uploads stuck in READY)

Ingestion publishes via an in-process dispatcher and retries broker errors
forever, so a backlog means Kafka is down or the dispatcher is wedged.

1. `docker compose ps` — kafka healthy?
2. `GET ingestion-api:8080/metrics` — `http_requests_total` flowing?
3. Kafka down: `docker compose restart kafka`, then `kafka-init` if topics
   vanished. Missed publishes redeliver on restart (at-least-once).
4. Poison upload: check `outbox` rows (`delivered_at IS NULL`, old
   `next_attempt_at`); the dispatcher skips nothing — a stuck row blocks its
   partition claim only, not others (`SKIP LOCKED`).

## Kafka down

- Transcription/extraction/embedding stop consuming; ingestion stays up
  (`/health/ready` excludes the broker by design).
- DLQ topics (`*.dlq`, 6 partitions, pre-created by `kafka-init`) hold
  poison events. Inspect with `kcat`/console-consumer before replaying.
- Extraction batches just wait; notification/rollup/MCP are Kafka-free and
  unaffected.

## Kratos down

- Ingestion returns `503 AUTH_UNAVAILABLE` (never a logout); the app shows
  `unavailable` → `reconnecting` with backoff and keeps its token.
- Do NOT rotate/admin anything during the outage; `whoami` failures are
  fail-closed on the client.
- Unverified identities older than `IDENTITY_TTL_HOURS` are reaped by the
  `identitycleanup` worker — during a long Kratos outage, verification
  emails can't land, so expect reaps after recovery.

## Device stuck in `reset_required`

1. App: Release flow = fresh re-auth + BLE erase + cloud release
   (`POST /v1/device/release`, needs re-auth ≤ 5 min or `REAUTH_REQUIRED`).
2. Server: `DATABASE_URL=... go run ./cmd/device-admin status -device
   <32hex>` (also `unquarantine`, `list`).
3. Pendant still misbehaving: `auth provision` over USB re-emits the claim
   hash, then `provision -device <id> -claim-hash <hash>`.

## Reminder never fired

1. Extraction: `extraction_batches_failed_total` rising? Check Groq
   (`groq.max_attempts`, 429/5xx retry) and the `extraction_jobs` row status.
2. `reminders` row present? A re-extract replaces rows; the ledger key
   includes `fire_at`, so reschedules create new deliveries.
3. `notification_deliveries` row `failed`? `MaxAttempts` exhaustion needs a
   manual reset to `pending`. Due times older than
   `delivery.max_lateness_seconds` are intentionally dropped.
4. ntfy: topic `cp-<...>` provisioned? `NTFY_ADMIN_TOKEN` set? Without it the
   service falls back to anonymous topics (dev only).

## Weekly recap looks thin

Normal when days had no recordings: the weekly prompt records its coverage
(`based on N of 7 days`). If a daily failed, its `summaries` row is simply
absent — re-run by waiting for the next nightly gate (advances only on
success).

## Rotating secrets

- ntfy passwords: generate with `ntfy user hash <password>`, set
  `NTFY_ADMIN_PASS_HASH`/`NTFY_PUBLISHER_PASS_HASH` in `.env`, restart ntfy,
  then `ntfy user change-pass` or wipe `ntfy-data` (provisioned users sync
  from config on start).
- `POSTGRES_PASSWORD` applies only on first volume init; rotate live via
  `make db-rotate-password NEW_PASSWORD=...` or wipe with `make
  infra-reset` (destructive).
- Grafana/ntfy alerts share `NTFY_TOKEN` via compose (`NTFY_AUTH_TOKENS`);
  rotate all three together.

## Gmail SMTP operations

- Rotation: revoke/regenerate the App Password in your Google Account,
  update `KRATOS_SMTP_URI` in the VPS `.env`, `docker compose up -d kratos`.
  Changing any Google security setting can silently invalidate it.
- Cap: ~500/day. A stuck retry loop can exhaust it — courier failures show
  in `docker compose logs kratos`.
- "Code never arrived": check spam → confirm the App Password is still
  valid → check kratos logs for courier errors. First send from a new VPS
  IP may need one manual "yes, that was me" approval in Google.

## Prod hardening checklist

Caddy still terminates `160-236-239-95.sslip.io` (dev convenience, third-party
dependency) — use a real domain in prod. Kratos `allowed_return_urls`
(`infra/kratos/kratos.yml`) is localhost-only — parameterize per
environment. DB passwords travel via compose env — use Docker secrets in
prod. Benchmark tables in the paper are representative measurements; add a
`bench/` harness before quoting them as guarantees.
