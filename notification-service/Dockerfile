FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /notification-service ./cmd
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

COPY --from=builder /notification-service .
COPY config.yaml .

ENTRYPOINT ["./notification-service"]
