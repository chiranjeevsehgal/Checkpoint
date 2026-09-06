# Ingestion service image. Targets:
#   api      - production API (HTTP + outbox dispatcher + cleaner)
#   mockvad  - local-dev VAD stand-in (compose dev profile only)
#   migrate  - one-shot schema migration job (runs before deploys)
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -o /out/mockvad ./mockvad && \
    CGO_ENABLED=0 go build -o /out/migrate ./cmd/migrate

FROM gcr.io/distroless/static-debian12 AS api
COPY --from=build /out/api /api
EXPOSE 8080
ENTRYPOINT ["/api"]

FROM gcr.io/distroless/static-debian12 AS mockvad
COPY --from=build /out/mockvad /mockvad
EXPOSE 8081
ENTRYPOINT ["/mockvad"]

FROM gcr.io/distroless/static-debian12 AS migrate
COPY --from=build /out/migrate /migrate
COPY migrations /migrations
ENTRYPOINT ["/migrate", "-dir", "/migrations", "up"]
