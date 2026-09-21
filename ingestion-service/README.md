# ingestion-service

HTTP API (`:8080`) for uploads, pendants, settings, notifications, MCP keys
and account deletion. Outbox dispatcher, upload-expiry sweep and
account-deletion worker run in-process.

## Run

```bash
make infra-up     # postgres, kafka, minio (from repo root compose or service Makefile)
make migrate      # needs DATABASE_URL
make run          # API on :8080
go test ./...
```

## Flow

`POST /v1/uploads` → `PUT` bytes to the presigned MinIO URL →
`POST /v1/uploads/<id>/complete` → `GET /v1/uploads/<id>` flips
`READY → SUBMITTED` once the outbox dispatcher publishes (~2 s).
Full sequence: `postman.json` (requests 1→4).

## Auth

Opaque Ory Kratos session token: `Authorization: Bearer <token>`, validated
via `GET /sessions/whoami`. `401` invalid session, `403
VERIFICATION_REQUIRED` unverified email, `503 AUTH_UNAVAILABLE` Kratos down
(never a logout). Upload creation is per-user rate limited
(`RATE_LIMIT_RPS`/`RATE_LIMIT_BURST`).

## Config (env)

`DATABASE_URL`, `DATABASE_REQUEST_URL` (RLS role), `DATABASE_WORKER_URL`
(worker role), `KRATOS_PUBLIC_URL`, `MINIO_ENDPOINT` vs
`MINIO_PUBLIC_ENDPOINT`, `MINIO_ACCESS_KEY/SECRET_KEY/BUCKET`,
`KAFKA_BROKERS`, `POOL_MAX_CONNS/MIN_CONNS`. `config.yaml` is the single
source of truth for the transcription topic (enforced by
`TestTopicSingleSourceOfTruth`).

## Probes

`GET /health/live`, `GET /health/ready` (Postgres + MinIO), `GET /metrics`.
