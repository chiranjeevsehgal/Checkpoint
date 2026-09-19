# Checkpoint — Developer Quick Start

First-time setup for the whole local stack: backend services, pendant firmware,
and the mobile app. Expected to take a while on the first run because Docker
builds images and the embedding worker bakes in the `bge-m3` model.

Replace `<HOST_LAN_IP>` everywhere with this machine's LAN address
(`ipconfig` / `ifconfig`; e.g. `192.168.1.5`). A phone on the same Wi-Fi cannot
reach `localhost`.

## 0. Prerequisites

- Docker Desktop (Compose v2) with ~8 GB RAM
- Go 1.25+ (four modules: `ingestion-service`, `transcription-service`, `extraction-service`, `rollup-service`)
- Node 20+ and npm
- Python 3.11+ (embedding worker, mcp-service, firmware host tests)
- `arduino-cli` with the `esp32` core `3.3.11` (firmware)
- Android platform-tools (`adb`) and a device/emulator
- JDK 17 + Android SDK/NDK 27 (Android app build)

## 1. Configure environment

### Root `.env` (drives Docker Compose)

```bash
copy .env.example .env     # Windows; cp on Linux/macOS
```

Fill in:

- `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB`
- `CHECKPOINT_REQUEST_PASSWORD`, `CHECKPOINT_WORKER_PASSWORD`,
  `CHECKPOINT_MCP_PASSWORD`, `KRATOS_DB_PASSWORD` (create the RLS request role,
  the trusted worker role, the read-only MCP role, and the Kratos database)
- `MCP_PUBLIC_URL` (advertised to MCP clients; the LAN IP or public HTTPS URL)
- `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD`
- `ELEVENLABS_API_KEY` and/or `DEEPGRAM_API_KEY`
- `MINIO_PUBLIC_ENDPOINT=<HOST_LAN_IP>:9000` (phones fetch presigned URLs here)
- `KRATOS_PUBLIC_BASE_URL=http://<HOST_LAN_IP>:4433/` (URL Kratos puts in flow
  actions; must be reachable by the phone)

Notes:

- `POSTGRES_PASSWORD` and the role/database passwords apply only on the **first**
  Postgres volume init. Changing them later is ignored unless you reset the
  volume (`docker compose down -v`).
- `KAFKA_TOPIC_TRANSCRIPTION` must match `ingestion-service/config.yaml`
  (`transcription.jobs.v1`). A test enforces this.

### App `.env` (in `checkpoint-app/`)

```dotenv
EXPO_PUBLIC_API_URL=http://<HOST_LAN_IP>:8080
EXPO_PUBLIC_KRATOS_URL=http://<HOST_LAN_IP>:4433
EXPO_PUBLIC_ENV=development
# Optional: full MCP endpoint shown in the app's key snippets.
# Defaults to http://<api host>:1417/mcp.
EXPO_PUBLIC_MCP_URL=http://<HOST_LAN_IP>:1417/mcp
```

These `EXPO_PUBLIC_*` values are inlined into the JS bundle at build time, so
rebuild after changing them. Use `.env.local` if you prefer it gitignored.

Dev builds (`npm run build:android:dev`) also show a **Server host** field on the
sign-in screen. Enter the machine's LAN IP there (e.g. `192.168.1.5`) to point
the app at `:8080` (API) and `:4433` (Kratos) at runtime, so a DHCP address
change no longer needs a rebuild. Leave it blank to use the build-time
`EXPO_PUBLIC_*` values; the choice is remembered until cleared.

## 2. Start the backend

```bash
docker compose up -d --build
docker compose ps
```

Wait until `postgres`, `kafka`, `minio`, `kratos`, `mailpit` are healthy and the
`*-migrate` jobs have exited successfully. The app is only up once
`ingestion-api`, `transcription` and `embedding` are running.

| Service        | URL / port                      |
| -------------- | ------------------------------- |
| Ingestion API  | http://localhost:8080           |
| Kratos public  | http://localhost:4433           |
| Mailpit UI     | http://localhost:8025           |
| MinIO console  | http://localhost:9001           |
| PostgreSQL     | localhost:5432                  |
| Kafka          | localhost:9092                  |

## 3. Verify the backend

```bash
curl http://localhost:8080/health/ready   # {"status":"ok"}
curl http://localhost:4433/health/alive   # {"status":"ok"}
```

## 4. Flash the pendant (firmware)

Open `firmware/checkpoint/checkpoint.ino` in the Arduino IDE (folder name must
match the `.ino`), or build with `arduino-cli`:

```bash
arduino-cli compile \
  --fqbn "esp32:esp32:esp32s3:FlashSize=16M,PartitionScheme=app3M_fat9M_16MB,PSRAM=opi,CDCOnBoot=default,USBMode=hwcdc,FlashMode=qio,UploadSpeed=921600" \
  firmware/checkpoint

arduino-cli upload -p <COM_PORT> \
  --fqbn "esp32:esp32:esp32s3:FlashSize=16M,PartitionScheme=app3M_fat9M_16MB,PSRAM=opi,CDCOnBoot=default,USBMode=hwcdc,FlashMode=qio,UploadSpeed=512000" \
  firmware/checkpoint
```

Then on the serial console (115200 baud):

```text
auth provision     # device <32hex>   cloud-sha256 <64hex>
auth export        # BLE claim key + checkpoint://claim?... URI
```

## 5. Register the pendant (privileged CLI)

`auth provision` prints the cloud hash, feed it to the offline CLI from
`ingestion-service/`:

```powershell
cd ingestion-service
# point at the host-published Postgres (bash: export DATABASE_URL="...")
$env:DATABASE_URL="postgres://<POSTGRES_USER>:<POSTGRES_PASSWORD>@localhost:5432/<POSTGRES_DB>?sslmode=disable"

go run ./cmd/device-admin provision -device <32hex> -claim-hash <64hex>
go run ./cmd/device-admin status    -device <32hex>   # state=unowned, owner=none
```

An unprovisioned pendant cannot be claimed by the app.

### Admin console (optional)

Steps 4–5 have a web shortcut. From `admin-console`:

```powershell
npm install
npm run dev     # Angular dev server :4200 + local agent :4300
```

The console selects the `arduino-cli` path and COM port, compiles/uploads the
firmware, drives the serial console (`auth list`, `auth export`,
`auth provision`, `auth reset`, `auth forget 0|1`, `power sleep`), registers
and lists devices, and manages identities. It reads the repo `.env` for
`DATABASE_URL` values and binds to `127.0.0.1` only. See `admin-console/README.md`.

## 6. Build the mobile app

```bash
cd checkpoint-app
npm install          # runs patch-package (Android native patches)
npm run check        # typecheck + lint + prettier
npm test
```

Install a dev build on a connected device (release-signed with the debug key):

```powershell
$env:EXPO_PUBLIC_API_URL="http://<HOST_LAN_IP>:8080"
$env:EXPO_PUBLIC_KRATOS_URL="http://<HOST_LAN_IP>:4433"
npm run build:android:dev -- -PreactNativeArchitectures=arm64-v8a
adb install -r android\app\build\outputs\apk\release\app-release.apk
```

For a Metro/dev-client loop instead, use `npm run android`.

## 7. First run (app + pendant)

1. **Create account**: Sign up with a name, email + password (min 12 chars).
2. **Verify email**: tap "Send code", then read the 6-digit code in Mailpit
   (`http://localhost:8025`) and enter it. Unverified identities are deleted
   after `IDENTITY_TTL_HOURS` (default 1h); use "Use a different email" on that
   screen to abandon a mistyped signup and start over.
3. **Pair pendant**: hold the button 5 s (60 s window), open
   **Pendant → Set up pendant**, paste the `auth export` claim key (or scan the
   URI), tap **Find & link**. The app fetches the cloud secret over the
   authenticated BLE session and claims ownership automatically.
4. **Record & sync**: record a clip; it transfers over BLE and uploads. The
   Transfers tab flips `READY → SUBMITTED`.

## Verify the pipeline

```bash
# uploads + transcripts
docker compose exec postgres psql -U <POSTGRES_USER> -d <POSTGRES_DB> \
  -c "SELECT id,status,device_id,user_id FROM uploads ORDER BY created_at DESC LIMIT 3;"
docker compose exec postgres psql -U <POSTGRES_USER> -d <POSTGRES_DB> \
  -c "SELECT audio_id,user_id,language,left(text,60) FROM transcripts ORDER BY created_at DESC LIMIT 3;"
docker compose exec postgres psql -U <POSTGRES_USER> -d <POSTGRES_DB> \
  -c "SELECT user_id,audio_id,count(*) FROM embeddings GROUP BY 1,2;"
docker compose exec postgres psql -U <POSTGRES_USER> -d <POSTGRES_DB> \
  -c "SELECT source_type,count(*) FROM search_documents GROUP BY 1;"   # MCP read model

# Kafka job
docker compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh \
  --topic transcription.jobs.v1 --bootstrap-server kafka:9092 --from-beginning --max-messages 1
```

## MCP access keys (in the app)

The app mints, lists and revokes MCP access keys under **Settings → MCP access**
(`/mcp-keys`). Creating a key shows the secret, the Claude Code command and an
`mcp.json` block once; copy them before leaving the screen. Revoking a key stops
any connected MCP client immediately.

The snippet endpoint is `EXPO_PUBLIC_MCP_URL` when set, otherwise
`http://<api host>:1417/mcp` — the same host the API resolves to, on the MCP
port. `EXPO_PUBLIC_*` values are inlined at build time, so rebuild after changing
it (same as the other `EXPO_PUBLIC_*` values).

## Tests

```bash
# backend (each Go module is separate)
cd ingestion-service   && go test ./... && go vet ./... && go build ./...
cd transcription-service && go test ./... && go vet ./... && go build ./...

# embedding (TEST_DATABASE_URL enables the DB integration test)
cd embedding-service && python -m unittest discover -s tests -v

# mcp-service (TEST_DATABASE_URL enables the DB integration test)
cd mcp-service && python -m unittest discover -s tests -v

# firmware host tests
pytest firmware/tests -v

# app
cd checkpoint-app && npm run check && npm test
```

With the stack up, integration tests can point at the published Postgres:

```bash
TEST_DATABASE_URL="postgres://<POSTGRES_USER>:<POSTGRES_PASSWORD>@localhost:5432/<POSTGRES_DB>?sslmode=disable" \
  go test ./internal/repository/postgres/... -v
```

## Everyday commands

```bash
docker compose logs -f ingestion-api transcription embedding mcp-service kratos
docker compose restart kratos              # after editing infra/kratos/kratos.yml
docker compose down                        # stop
docker compose down -v                     # DESTRUCTIVE: wipe postgres/kafka/minio
```

Local backend without the containers for the app/worker:

```bash
cd ingestion-service
make infra-up          # postgres + minio + kafka
make migrate           # needs DATABASE_URL
make run               # API on :8080
```

## Troubleshooting

| Symptom                                   | Fix                                                                 |
| ----------------------------------------- | ------------------------------------------------------------------- |
| App: "Could not create the account"       | Confirm `KRATOS_PUBLIC_BASE_URL` uses `<HOST_LAN_IP>` and rebuild the app; then check `docker compose logs kratos`. |
| No verification email                     | It goes to Mailpit, not a real inbox: http://localhost:8025.        |
| `POST /v1/uploads` → 401                   | Session invalid/expired; sign in again. 503 `AUTH_UNAVAILABLE` means Kratos is down, not logged out. |
| Upload skipped "no cloud-owned pendant"   | Provision the pendant (step 5), then re-link in the app.            |
| Claim fails generically (409)             | Device not provisioned, wrong `cloud-sha256`, already owned, or `reset_required`. Check `device-admin status`. |
| Enrollment window not active              | Hold the pendant button 5 s immediately before linking.            |
| Phone can't reach services                | Phone must be on the same Wi-Fi; update the LAN IPs and rebuild.    |
| `go build` fails in `transcription-service` | Run `go mod tidy` once (the Docker build does this automatically). |
