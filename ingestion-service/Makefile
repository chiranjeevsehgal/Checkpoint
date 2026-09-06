# Ingestion service workflows. Run from this directory.
# Local infra (Postgres, MinIO) is defined in the repo-root compose file.

.PHONY: infra-up infra-down migrate status run test build vet

infra-up:
	cd .. && docker compose up -d postgres minio

infra-down:
	cd .. && docker compose stop postgres minio

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
