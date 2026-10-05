# Contributing to Checkpoint

First off, thank you for considering contributing to Checkpoint! Whether you are designing enclosures, optimizing DSP filters on the ESP32-S3, improving mobile sync, or adding MCP tools, all contributions are welcome.

Checkpoint is a polyglot monorepo containing embedded firmware, a React Native mobile app, and distributed backend microservices. This guide will help you get set up and submit your contributions smoothly.

---

## Code of Conduct

We are committed to providing a welcoming, inclusive, and harassment-free environment for everyone. Please be respectful, constructive, and kind in discussions, issues, and code reviews.

---

## Repository Structure

Checkpoint consists of several independent modules:

```text
checkpoint/
├── firmware/              # ESP32-S3 pendant firmware (Arduino/C++) & host tests
├── checkpoint-app/        # React Native / Expo companion mobile app
├── ingestion-service/     # Central Go REST API (:8080) & outbox dispatcher
├── transcription-service/ # Go Kafka worker (Speech-to-Text)
├── extraction-service/    # Go Kafka worker (Groq LLM extraction)
├── retry-service/         # Go Kafka worker (central delayed-retry queue)
├── rollup-service/        # Go worker (nightly daily/weekly summaries)
├── notification-service/  # Go worker (scheduled reminders -> ntfy push)
├── embedding-service/     # Python Kafka worker (BGE-M3 -> pgvector)
├── mcp-service/           # Python MCP server (:1417, Streamable HTTP + OAuth)
├── admin-console/         # Local web tooling (Angular 21 + Fastify)
├── infra/                 # Postgres bootstrap, Kratos, ntfy, observability
└── docs/                  # Architecture, setup, and runbooks
```

---

## Development Prerequisites

Depending on what you plan to work on, you will need:

- **Backend / Infra:**
  - Docker Desktop (Compose v2) with ~8 GB RAM
  - Go 1.25+
  - Python 3.11+
- **Mobile App:**
  - Node.js 20+ and npm
  - JDK 17, Android Studio / SDK (NDK 27) for Android native builds
- **Hardware & Firmware:**
  - `arduino-cli` with the `esp32` core (`3.3.11`) or Arduino IDE 2.x
  - Python 3.11+ with `pytest` for host-side firmware tests

---

## Local Development Setup

### 1. Boot the Backend Services

1. Copy the example environment file:
   ```bash
   cp .env.example .env    # Linux / macOS
   copy .env.example .env  # Windows
   ```
2. Fill in the required API keys (e.g. `GROQ_API_KEY`, `ELEVENLABS_API_KEY` or `DEEPGRAM_API_KEY`).
3. Start the infrastructure stack:
   ```bash
   docker compose up -d --build
   ```
4. Verify backend health:
   ```bash
   curl http://localhost:8080/health/ready
   curl http://localhost:4433/health/alive
   ```

Detailed configuration options are documented in [`docs/DEVELOPER_SETUP.md`](docs/DEVELOPER_SETUP.md).

---

## Subsystem Development & Testing

Before opening a pull request, run the test suites for the areas you modified:

### 1. Firmware (`firmware/`)
Firmware logic and protocol decoders can be tested on your host machine without hardware:

```bash
# Run host-side pytest suite
pytest firmware/tests -v
```

To verify the Arduino firmware compiles cleanly:
```bash
arduino-cli compile \
  --fqbn "esp32:esp32:esp32s3:FlashSize=16M,PartitionScheme=app3M_fat9M_16MB,PSRAM=opi,CDCOnBoot=default,USBMode=hwcdc,FlashMode=qio" \
  firmware/checkpoint
```

### 2. Mobile App (`checkpoint-app/`)
```bash
cd checkpoint-app
npm install
npm run check        # Runs TypeScript check, ESLint, and Prettier
npm test             # Runs Jest unit tests
```

### 3. Go Backend Services
Each Go service is a standalone module. Test each modified module:

```bash
# Ingestion API
cd ingestion-service
go test ./... -v
# Enforces topic consistency across configurations:
go test ./internal/config/ -run TestTopicSingleSourceOfTruth -v

# Other services (transcription, extraction, retry, rollup, notification)
cd ../transcription-service && go test ./... -v
cd ../extraction-service && go test ./... -v
cd ../retry-service && go test ./... -v
cd ../rollup-service && go test ./... -v
cd ../notification-service && go test ./... -v
```

### 4. MCP Service (`mcp-service/`)
```bash
cd mcp-service
python -m unittest discover -s tests -v
```

---

## Critical Repository Guidelines

Please keep these invariants in mind when submitting code:

1. **Kafka Topic Single Source of Truth:**
   `ingestion-service/config.yaml` (`kafka.topic_transcription`, defaulting to `transcription.jobs.v1`) is canonical. Never hardcode topic literals in Go code; always reference configuration.
2. **Database Migrations:**
   Each database-backed service owns its schema via Goose migrations (`migrations/`). Never modify existing migrations that have already shipped—always create a new sequential migration file.
3. **PostgreSQL Roles & Security:**
   - Client HTTP requests operate under the `checkpoint_request` role with Row-Level Security (RLS) enforced (`set_config('app.user_id', ...)`).
   - Trusted asynchronous workers connect using `checkpoint_worker` (`BYPASSRLS`).
   - MCP queries operate under the restricted `checkpoint_mcp` role.
4. **Hardware Pinouts:**
   All hardware GPIO assignments and timings must remain centralized in [`firmware/checkpoint/config.h`](firmware/checkpoint/config.h).

---

## How to Submit a Contribution

1. **Fork & Branch:**
   - Fork the repository on GitHub.
   - Create a feature branch with a descriptive name:
     ```bash
     git checkout -b feat/mcp-filter-by-date
     # or: fix/ble-transfer-timeout
     ```
2. **Make Your Changes:**
   - Write clean, self-documenting code.
   - Add unit tests for new logic or bug fixes.
   - Keep pull requests focused on a single logical change.
3. **Commit Messages:**
   - Use conventional commit messages:
     - `feat(firmware): add configurable silence hangover window`
     - `fix(ingestion): handle empty payload in presigned upload completion`
     - `docs(readme): clarify local docker compose setup`
4. **Open a Pull Request:**
   - Push your branch to GitHub and open a PR against `main`.
   - Fill out the PR description explaining:
     - **What** changed
     - **Why** the change was made
     - **How** you tested it (screenshots, test outputs, or hardware verification logs are encouraged!)

---

## Reporting Issues & Feature Requests

- **Bug Reports:** Search existing issues first. If not found, open a new issue detailing:
  - Operating system / hardware board revision
  - Steps to reproduce
  - Relevant logs (`docker compose logs <service>` or serial monitor output)
  - Expected vs. actual behavior
- **Feature Requests & Ideas:** Open an issue with the `enhancement` label or start a discussion. Hardware designs, 3D printable enclosure files, and new MCP integrations are especially welcome!

Thank you for building Checkpoint with us! 🚀
