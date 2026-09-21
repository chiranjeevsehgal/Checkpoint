# Checkpoint Admin Console

Local-only administration console for Checkpoint: an Angular SPA plus a small
Node agent that does the privileged work (flash firmware, drive the pendant
serial console, provision devices, manage identities).

It is **not** part of `docker-compose.yaml` and never ships to a server. The
agent binds `127.0.0.1` by default, so the console is reachable only from the
machine it runs on.

## Requirements

- Node.js 24.x (Angular 21 needs >= 24.0)
- Go (the agent shells `go run ./cmd/device-admin`)
- `arduino-cli` on `PATH` (or set `ADMIN_ARDUINO_CLI`)
- The Checkpoint services running (Postgres, Kratos, ingestion API)

## Setup

```powershell
cd admin-console
cp .env.example .env        # optional overrides; repo-root .env provides POSTGRES_*
npm install
npm --prefix server install
npm --prefix web install
```

## Run

```powershell
npm run dev     # Angular dev server :4200 + agent :4300 (proxied)
npm run build   # build SPA + agent
npm start       # agent serves the built SPA on http://127.0.0.1:4300
```

Open either `http://localhost:4200` (dev) or `http://127.0.0.1:4300` (built).

## Features

- **Devices** — list all pendants, read IDs over USB, provision, status, unquarantine, enrollment QR.
- **Firmware** — compile, upload a build dir, or compile + upload with `arduino-cli`.
- **Serial** — open the pendant console (115200) and run `auth`/`power` commands.
- **Users** — list Kratos identities, revoke sessions, queue account deletions.
- **Deletions** — watch the account-deletion tombstones drain to `COMPLETE`.
- **Dashboard** — service health, compose status, effective agent config.
- **Settings** — per-admin backend URLs, paths, FQBN and device-admin mode; with a directory browser, arduino-cli detection and connection tests.

## Configuration

Most settings are editable in the console's **Settings** page: backend URLs, the
database URL, `arduino-cli` path, sketch/build directories, FQBN, serial default,
and how the privileged `device-admin` CLI runs. Values are saved to the gitignored
`admin-console/server/data/settings.json` and applied without restarting.

Resolution order (highest wins): real environment variables → Settings page
(`settings.json`) → `admin-console/.env` → repo-root `.env` → built-in defaults.
The bind address and port (`ADMIN_HOST`/`ADMIN_PORT`) come from the environment and
need an agent restart.

### Device admin CLI modes

- `go` (default) — `go run ./cmd/device-admin` from the local repo; needs the repo + Go.
- `binary` — a prebuilt `device-admin` executable (`ADMIN_DEVICE_ADMIN_BINARY`).
- `ssh` — runs `./device-admin` on a remote host (`ADMIN_DEVICE_ADMIN_SSH_HOST`,
  `ADMIN_DEVICE_ADMIN_SSH_DIR`); the remote host supplies its own `DATABASE_URL`.

### Remote services (VPS) with SSH tunnels

Kratos admin and Postgres must stay private. Publish them loopback-only on the
VPS, then tunnel from this machine:

```powershell
ssh -N -L 15432:127.0.0.1:5432 -L 14434:127.0.0.1:4434 user@your-vps
```

and point the console at the tunnels (Settings page or `admin-console/.env`):

```dotenv
ADMIN_DATABASE_URL=postgres://checkpoint_worker:...@localhost:15432/checkpoint_db?sslmode=disable
ADMIN_KRATOS_ADMIN_URL=http://127.0.0.1:14434
```

Firmware flashing and serial always use the local USB device.

## Security

- The agent binds `127.0.0.1` by default. Binding anywhere else requires
  `ADMIN_TOKEN`: the agent refuses a non-loopback bind without it, and every
  `/api` + `/events` route then requires `Authorization: Bearer <token>` (the
  web UI asks for it once per browser session; `/api/health` stays open).
- `ADMIN_SETTINGS_KEY` encrypts `server/data/settings.json` at rest
  (AES-256-GCM). Without it the file stays plaintext; an encrypted file
  without the key reads as empty rather than exposing secrets.
- The Kratos admin API has no auth of its own; expose it on loopback only.
- Secrets (`DATABASE_URL` password) are redacted in `/api/config`.
