FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download 2>/dev/null || true

COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o /transcription-service ./cmd
RUN CGO_ENABLED=0 GOOS=linux go build -o /migrate ./cmd/migrate

FROM alpine:3.19 AS migrate

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /migrate .
COPY migrations ./migrations

ENTRYPOINT ["./migrate"]
CMD ["up"]

FROM alpine:3.19 AS worker

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /transcription-service .
COPY config.yaml .

ENTRYPOINT ["./transcription-service"]