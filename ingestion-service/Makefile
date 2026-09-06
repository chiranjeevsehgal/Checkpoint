# Ingestion service workflows. Run from this directory.
# Local infra (Postgres, MinIO, Kafka) is defined in the repo-root compose file.

.PHONY: infra-up infra-down infra-reset db-rotate-password migrate status run test build vet

infra-up:
	cd .. && docker compose up -d postgres minio kafka kafka-init

infra-down:
	cd .. && docker compose stop postgres minio kafka

# Wipe dev postgres + minio state (DESTRUCTIVE). Re-inits from current .env.
infra-reset:
	cd .. && docker compose down postgres minio && docker volume rm -f checkpoint-100_postgres-data checkpoint-100_minio-data checkpoint_postgres-data checkpoint_minio-data; docker compose up -d postgres minio

# Rotate DB password without wiping (uses socket trust inside container).
db-rotate-password:
	cd .. && docker compose exec postgres psql -U ${POSTGRES_USER} -d ${POSTGRES_DB} -c "ALTER USER \"${POSTGRES_USER}\" WITH PASSWORD '${NEW_PASSWORD}';"

migrate:
	go run ./cmd/migrate up

status:
	go run ./cmd/migrate status

run:
	go run ./cmd/api

test:
	go test ./...

vet:
	go vet ./...

build:
	go build ./...
