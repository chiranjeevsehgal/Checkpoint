import logging
import signal
import time

from .config import load
from .chunker import TokenChunker
from .embedder import Embedder
from .events import EmbeddingJobEvent, InvalidEvent
from .kafka import Kafka
from .logging_setup import setup
from .store import Store

log = logging.getLogger("embedding")

RETRY_BACKOFFS = [1, 5, 15, 60]


class StaleDeletedUser(Exception):
    """The event belongs to an account being deleted; commit and drop it."""


def process(event: EmbeddingJobEvent, embedder: Embedder, chunker: TokenChunker, store: Store) -> None:
    if store.is_user_deleting(event.user_id):
        raise StaleDeletedUser(event.user_id)

    chunks = chunker.chunk(event.text)
    if not chunks:
        raise InvalidEvent("text is empty after normalization")
    vectors = embedder.embed(chunks)

    if store.is_user_deleting(event.user_id):
        raise StaleDeletedUser(event.user_id)

    store.save(
        user_id=event.user_id,
        audio_id=event.audio_id,
        language=event.language,
        model=embedder.model_name,
        chunks=chunks,
        vectors=vectors,
    )
    log.info(
        "embedded",
        extra={"audio_id": event.audio_id, "user_id": event.user_id, "chunks": len(chunks)},
    )


def main() -> None:
    setup("embedding-service")
    cfg = load()

    embedder = Embedder(cfg.embedding_model)
    chunker = TokenChunker(embedder.tokenizer, cfg.chunk_tokens, cfg.chunk_overlap_tokens)
    store = Store(cfg.database_url)
    store.connect()
    kafka = Kafka(cfg.kafka_brokers, cfg.kafka_topic, cfg.kafka_consumer_group)
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

    retry_attempt = 0

    while not stop:
        msg = kafka.poll(1.0)
        if msg is None:
            continue
        if Kafka.error(msg):
            if msg.error().fatal():
                log.error("fatal kafka error", extra={"error": str(msg.error())})
                break
            log.warning("kafka error", extra={"error": str(msg.error())})
            continue

        try:
            event = EmbeddingJobEvent.from_raw(msg.value())
        except InvalidEvent as exc:
            kafka.send_to_dlq("INVALID_EVENT", str(exc), msg.value())
            log.error(
                "dlq",
                extra={"offset": msg.offset(), "partition": msg.partition(), "error": str(exc)},
            )
            kafka.commit(msg)
            retry_attempt = 0
            continue

        try:
            process(event, embedder, chunker, store)
        except InvalidEvent as exc:
            kafka.send_to_dlq("INVALID_EVENT", str(exc), msg.value())
            log.error("dlq", extra={"audio_id": event.audio_id, "error": str(exc)})
            kafka.commit(msg)
            retry_attempt = 0
            continue
        except StaleDeletedUser:
            log.info("stale_deleted_user_event", extra={"audio_id": event.audio_id})
            kafka.commit(msg)
            retry_attempt = 0
            continue
        except Exception as exc:
            retry_attempt += 1
            backoff = RETRY_BACKOFFS[min(retry_attempt - 1, len(RETRY_BACKOFFS) - 1)]
            log.error(
                "transient failure; retrying",
                extra={
                    "attempt": retry_attempt,
                    "audio_id": event.audio_id,
                    "error": str(exc),
                    "retry_seconds": backoff,
                },
            )
            # Drop the pooled pg connection: if the failure was a broken
            # socket, reusing it would fail every subsequent attempt.
            try:
                store.reset()
            except Exception:
                log.warning("pg reconnect failed; will retry next attempt")
            time.sleep(backoff)
            # Seek back so the same message is redelivered after the pause;
            # the offset is only committed once processing succeeds.
            kafka.seek(msg.partition(), msg.offset())
            continue

        retry_attempt = 0
        kafka.commit(msg)

    kafka.close()
    store.close()
    log.info("shut down cleanly")


if __name__ == "__main__":
    main()
