# Setup

## 1. Create `.env`

Create `.env` from `.env.example`.

### Windows

```bash
copy .env.example .env
```

### Linux / macOS

```bash
cp .env.example .env
```

Update `.env` with your actual values..

---

## 2. Start everything

```bash
docker-compose up
```

Or run in detached mode:

```bash
docker-compose up -d
```

---

## 3. Check containers

```bash
docker-compose ps
```

Both `postgres` and `kafka` should be up and healthy.

---

## 4. Test PostgreSQL

```bash
docker-compose exec postgres psql -U <POSTGRES_USER> -d <POSTGRES_DB> -c "SELECT version();"
```

Troubleshooting `FATAL: password authentication failed`:

* `POSTGRES_PASSWORD` only applies on first init (empty `postgres-data` volume). Changing `.env` later is ignored — the healthcheck now uses TCP password auth so it correctly goes `unhealthy`.
* Keep data: `docker exec postgres psql -U <POSTGRES_USER> -d <POSTGRES_DB> -c "ALTER USER <POSTGRES_USER> WITH PASSWORD '<new>';"`
* Wipe dev data: `docker compose down postgres minio && docker volume rm <project>_postgres-data <project>_minio-data && docker compose up -d postgres minio` (expect `initializing`, not `Skipping initialization` in `docker logs postgres`).
* DBeaver/pgAdmin on laptop: Host `localhost`, Port `5432`, SSL Disable. Host `postgres` only inside the `backend` network.
* `FATAL: invalid value for parameter "TimeZone": "Asia/Calcutta"`: older DBeaver/Java sends the pre-rename zone name. The compose file recreates the `Calcutta -> Kolkata` tzdata link on every postgres start as a compat shim, so it connects; still prefer setting the client driver `TimeZone` to `Asia/Kolkata` (server default is `Asia/Kolkata`).

---

## 5. Test Kafka

### Create topic

```bash
docker-compose exec kafka /opt/kafka/bin/kafka-topics.sh --create --topic test-topic --bootstrap-server kafka:9092 --partitions 1 --replication-factor 1
```

### List topics

```bash
docker-compose exec kafka /opt/kafka/bin/kafka-topics.sh --list --bootstrap-server kafka:9092
```

### Produce messages

```bash
docker-compose exec kafka /opt/kafka/bin/kafka-console-producer.sh --topic test-topic --bootstrap-server kafka:9092
```

Enter messages:

```text
message 1
message 2
```

Press `Ctrl+C` to exit.

### Consume messages

```bash
docker-compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh --topic test-topic --bootstrap-server kafka:9092
```

Press `Ctrl+C` to exit.

---

## 6. Ingestion service (local dev)

From `ingestion-service/`:

```bash
make infra-up   # postgres + minio
make migrate    # goose up (needs DATABASE_URL)
make run        # API on :8080
```

Or the full composed stack (API + migrate + Kafka):

```bash
docker compose up --build
```

Authentication is an opaque Ory Kratos session token. Register/login through
the Kratos public API at `http://localhost:4433`, then send the returned
`session_token` as `Authorization: Bearer <token>`; there is no UUID-bearer
shortcut. Compose starts `kratos`/`kratos-migrate` and `mailpit`
(verification/recovery email UI at `http://localhost:8025`); the Kratos admin
API is bridge-only and never published. The `checkpoint_request` (RLS) and
`checkpoint_worker` roles are created by `infra/postgres/bootstrap.sh` on the
first Postgres volume.

Before uploading, claim your pendant (one per account):

```bash
TOKEN=<kratos session token>
DEVICE=<32 lowercase hex device id>
curl localhost:8080/v1/device -H "Authorization: Bearer $TOKEN"
curl -X POST localhost:8080/v1/device/claim -H "Authorization: Bearer $TOKEN" \
  -d '{"device_id":"'"$DEVICE"'","cloud_claim_secret":"<64 hex chars>"}'
```

Uploads are OGG-only (`audio/ogg`) and capped at 10 MB — anything else
is rejected with 415/413 before any bytes move. `device_id` is required and
must be a pendant owned by the caller.

```bash
curl -X POST localhost:8080/v1/uploads \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":8,"device_id":"'"$DEVICE"'"}'
# PUT 8 bytes to the returned upload.url, then:
curl -X POST localhost:8080/v1/uploads/<id>/complete \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"size_bytes":8}'
# GET /v1/uploads/<id> flips READY -> SUBMITTED once the outbox dispatcher
# publishes to Kafka (transcription.jobs.v1, ~2s). Verify with:
# docker compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh --topic transcription.jobs.v1 --bootstrap-server kafka:9092
```

`DELETE /v1/me` records a deletion tombstone (202) and returns immediately; the
in-process deletion worker purges MinIO objects, uploads, transcripts,
embeddings and finally the Kratos identity. It requires a fresh session or an
email verified within the last 5 minutes. Release a pendant with
`POST /v1/device/release` only within 5 minutes of a fresh login.

Migrations `00007`–`00009` add `uploads.device_id NOT NULL` and device-aware
idempotency, so an existing dev volume must be reset
(`docker compose down` + remove the `postgres-data` volume) before `up`.

Idempotency: `Idempotency-Key` retries return the same `upload_id` with a
freshly minted 15m `upload.url`. `/complete` verifies MinIO size and
`audio/ogg` type; `checksum_sha256` must be 64-char lowercase hex if sent.

Storage layout in bucket `audio`: `{userID}/{YYYY}/{MM}/{DD}/<uploadUUID>`
(day-partitioned for future daily summarization; existing `.../{MM}/<id>`
objects remain valid and are never rewritten).

---

## 7. Embedding service

Consumes `EMBEDDING_REQUESTED` events from `embedding.jobs.v1` (published by
the transcription service after a transcript lands), chunks the text with the
model's own tokenizer (default 2048 tokens per chunk, 100-token overlap,
configurable via `EMBEDDING_CHUNK_TOKENS`/`EMBEDDING_CHUNK_OVERLAP_TOKENS`),
embeds each chunk with [BAAI/bge-m3](https://huggingface.co/BAAI/bge-m3)
(1024-dim, L2-normalized) and upserts into pgvector.

Postgres runs on the `pgvector/pgvector:pg18` image — the same stock
PostgreSQL 18 with the `vector` extension compiled in. All relational
metadata tables (`uploads`, `transcripts`, outbox, ...) live unchanged in
the same database; the migration adds the `vector` extension and one table:

```text
embeddings(user_id, audio_id, chunk_index, chunk_text, language, model, embedding vector(1024), created_at)
```

### Namespace (username space)

`user_id` is the namespace. Every vector row carries its owner, and every
retrieval query must scope by it — one user can never see another's
vectors:

```bash
docker-compose exec postgres psql -U <POSTGRES_USER> -d <POSTGRES_DB> -c \
  "SELECT audio_id, chunk_index, 1 - (embedding <=> (SELECT embedding FROM embeddings WHERE user_id='<USER_A>' LIMIT 1)) AS sim
   FROM embeddings WHERE user_id='<USER_A>' ORDER BY embedding <=> (SELECT embedding FROM embeddings WHERE user_id='<USER_A>' LIMIT 1) LIMIT 5;"
```

`embeddings(user_id, audio_id, chunk_index)` is unique, so Kafka
redeliveries are idempotent; a GIN-free HNSW index over cosine distance
serves nearest-neighbor queries.

### Build & run

The bge-m3 model (~2.4 GB, CPU torch) is baked into the Docker image — the
first `docker compose build embedding` downloads it once; no volume or
internet is needed at runtime. Worker RAM sits around 3–4 GB during
inference, so give Docker Desktop at least ~6 GB.

Watch progress with plain output — the model step prints a rolling
`[model] ...` line every 15 s so the big download is never a silent hang:

```bash
docker compose build --progress=plain embedding
docker compose up -d embedding
docker compose logs -f embedding     # "listening on kafka topic embedding.jobs.v1 ..."
```

The `embedding-migrate` job builds from a separate slim stage (psycopg
only) and does not pull the model layers.

Delivery semantics mirror the transcription service: offsets commit only
after a message is embedded and stored; poison messages (bad JSON, empty
text) go to `embedding.jobs.v1.dlq` with the standard envelope
(`EMBEDDING_FAILED`); transient failures (pg down) retry with backoff.

Verify end-to-end after an upload flows through transcription:

```bash
docker-compose exec postgres psql -U <POSTGRES_USER> -d <POSTGRES_DB> -c \
  "SELECT user_id, count(*) FROM embeddings GROUP BY user_id;"
```

---

## 8. Native app (Expo)

The app authenticates against Kratos directly (no SDK). Point it at the Kratos
public API with `EXPO_PUBLIC_KRATOS_URL` (defaults to `http://localhost:4433`,
but a phone needs the host LAN IP, e.g. `http://192.168.1.5:4433`) as well as
`EXPO_PUBLIC_API_URL` for the ingestion API. Web authentication is out of scope;
the session token is stored with SecureStore on native only.

Flow: register → verify the email code (view it in Mailpit at
`http://localhost:8025`) → BLE-enroll the pendant with its claim key → the app
fetches the cloud claim secret over the authenticated BLE session and calls
`POST /v1/device/claim` → uploads carry `Authorization: Bearer <session token>`
and `device_id`.

Pendant provisioning (offline, USB serial):

```bash
# On the pendant serial console:
auth provision          # prints: device <32hex>  cloud-sha256 <64hex>
auth export             # prints the BLE claim key + checkpoint://claim URI
```

Register the printed hash with the privileged CLI:

```bash
cd ingestion-service
DATABASE_URL=postgres://... go run ./cmd/device-admin provision \
  -device <32hex> -claim-hash <64hex>
```

Release (in the app) requires a fresh login, erases local recordings, clears the
pendant's trusted BLE slots, then releases cloud ownership. `Forget` is local
only and leaves cloud ownership unchanged.

Run the checks from `checkpoint-app/`:

```bash
npm run check
npm test
```

---

## 9. Stop containers

Stop and remove containers:

```bash
docker-compose down
```