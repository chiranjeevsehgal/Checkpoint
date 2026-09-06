# Setup

## 1. Create `.env`

Create `.env` from `env.example`.

### Windows

```bash
copy env.example .env
```

### Linux / macOS

```bash
cp env.example .env
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

Or the full composed stack (API + migrate + mock VAD):

```bash
docker compose --profile dev up --build
```

Smoke flow (Bearer token is the dev user UUID):

Uploads are OGG-only (`audio/ogg`) and capped at 10 MB — anything else
is rejected with 415/413 before any bytes move.

```bash
UID=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa
curl -X POST localhost:8080/v1/uploads \
  -H "Authorization: Bearer $UID" \
  -d '{"filename":"m.ogg","content_type":"audio/ogg","size_bytes":8}'
# PUT 8 bytes to the returned upload.url, then:
curl -X POST localhost:8080/v1/uploads/<id>/complete \
  -H "Authorization: Bearer $UID" \
  -d '{"size_bytes":8}'
# GET /v1/uploads/<id> flips READY -> SUBMITTED once mock-vad accepts.
```

Idempotency: `Idempotency-Key` retries return the same `upload_id` with a
freshly minted 15m `upload.url`. `/complete` verifies MinIO size and
`audio/ogg` type; `checksum_sha256` must be 64-char lowercase hex if sent.

---

## 7. Stop containers

Stop and remove containers:

```bash
docker-compose down
```