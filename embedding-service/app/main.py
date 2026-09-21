import logging
import signal
import time

import psycopg

from .batching import collect_batch, parse_valid, split_vectors
from .config import load
from .chunker import TokenChunker
from .embedder import Embedder
from .health import HealthServer
from .kafka import Kafka
from .logging_setup import setup
from .store import Store

log = logging.getLogger("embedding")


def reject(message, kafka: Kafka, health: HealthServer, error: str, audio_id=None) -> None:
    kafka.send_to_dlq("INVALID_EVENT", error, message.value())
    log.error("dlq", extra={"offset": message.offset(), "partition": message.partition(),
                            "audio_id": audio_id, "error": error})
    kafka.commit(message)
    health.inc("embedding_events_invalid_total")


def main() -> None:
    setup("embedding-service")
    cfg = load()

    embedder = Embedder(cfg.embedding_model)
    chunker = TokenChunker(embedder.tokenizer, cfg.chunk_tokens, cfg.chunk_overlap_tokens)
    store = Store(cfg.database_url)
    store.connect()
    kafka = Kafka(cfg.kafka_brokers, cfg.kafka_topic, cfg.kafka_consumer_group, cfg.kafka_retry_topic)
    log.info(
        "listening",
        extra={
            "topic": cfg.kafka_topic,
            "group": cfg.kafka_consumer_group,
            "model": cfg.embedding_model,
        },
    )

    stop = False

    def handle_signal(signum, frame):
        nonlocal stop
        stop = True

    signal.signal(signal.SIGINT, handle_signal)
    signal.signal(signal.SIGTERM, handle_signal)

    health = HealthServer(cfg.metrics_addr, cfg.metrics_port)
    health.start()
    health.set_ready()

    retry_attempt = 0

    while not stop:
        batch = collect_batch(kafka, cfg.batch_max)
        if not batch:
            continue

        live: list = []
        for msg in batch:
            if Kafka.error(msg):
                if msg.error().fatal():
                    log.error("fatal kafka error", extra={"error": str(msg.error())})
                    stop = True
                    break
                log.warning("kafka error", extra={"error": str(msg.error())})
                continue
            live.append(msg)
        if stop:
            break
        if not live:
            continue

        valid, invalid = parse_valid(live)
        for msg in invalid:
            reject(msg, kafka, health, "unparseable event")
        if not valid:
            retry_attempt = 0
            continue

        jobs: list = []
        for msg, event in valid:
            if store.is_user_deleting(event.user_id):
                log.info("stale_deleted_user_event", extra={"audio_id": event.audio_id})
                kafka.commit(msg)
                health.inc("embedding_events_stale_total")
                continue
            chunks = chunker.chunk(event.text)
            if not chunks:
                reject(msg, kafka, health, "text is empty after normalization", event.audio_id)
                continue
            jobs.append((msg, event, chunks))
        if not jobs:
            retry_attempt = 0
            continue

        try:
            all_chunks = [chunk for _, _, chunks in jobs for chunk in chunks]
            vectors = embedder.embed(all_chunks)
            grouped = split_vectors([len(chunks) for _, _, chunks in jobs], vectors)
            for (msg, event, chunks), event_vectors in zip(jobs, grouped):
                if store.is_user_deleting(event.user_id):
                    log.info("stale_deleted_user_event", extra={"audio_id": event.audio_id})
                    kafka.commit(msg)
                    health.inc("embedding_events_stale_total")
                    continue
                store.save(
                    user_id=event.user_id,
                    audio_id=event.audio_id,
                    language=event.language,
                    model=embedder.model_name,
                    chunks=chunks,
                    vectors=event_vectors,
                )
                log.info(
                    "embedded",
                    extra={"audio_id": event.audio_id, "user_id": event.user_id,
                           "chunks": len(chunks)},
                )
                kafka.commit(msg)
                health.inc("embedding_events_ok_total")
        except Exception as exc:
            stage = getattr(exc, "stage", "process")
            error_code = getattr(exc, "error_code", "PROCESSING_ERROR")
            retry_attempt += 1
            if retry_attempt > len(cfg.retry_backoffs):
                # Fast local retries exhausted: hand each event to the
                # central delayed-retry service instead of looping forever.
                log.error(
                    "handing off to retry service",
                    extra={
                        "stage": stage,
                        "events": len(jobs),
                        "error": str(exc),
                        "error_type": type(exc).__name__,
                    },
                )
                # Drop the pooled pg connection: if the failure was a
                # broken socket, reusing it would fail every subsequent
                # attempt.
                try:
                    store.reset()
                except psycopg.Error:
                    log.warning("pg reconnect failed; will retry next attempt")
                for msg, event, _ in jobs:
                    kafka.send_to_retry(
                        "embedding-service",
                        stage,
                        error_code,
                        str(exc),
                        msg.value(),
                        key=event.event_id,
                    )
                    kafka.commit(msg)
                retry_attempt = 0
                continue
            backoff = cfg.retry_backoffs[retry_attempt - 1]
            log.error(
                "transient failure; retrying",
                extra={
                    "attempt": retry_attempt,
                    "events": len(jobs),
                    "error": str(exc),
                    "error_type": type(exc).__name__,
                    "retry_seconds": backoff,
                },
            )
            health.inc("embedding_events_failed_total")
            # Drop the pooled pg connection: if the failure was a broken
            # socket, reusing it would fail every subsequent attempt.
            try:
                store.reset()
            except psycopg.Error:
                log.warning("pg reconnect failed; will retry next attempt")
            time.sleep(backoff)
            # Seek every uncommitted message back so the batch is redelivered
            # after the pause; offsets commit only once saving succeeds.
            # Saves are upserts, so redelivered events are no-ops.
            for msg, _, _ in jobs:
                kafka.seek(msg.partition(), msg.offset())
            continue

        retry_attempt = 0

    kafka.close()
    store.close()
    health.stop()
    log.info("shut down cleanly")


if __name__ == "__main__":
    main()
