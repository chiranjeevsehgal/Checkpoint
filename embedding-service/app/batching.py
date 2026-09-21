"""Batch helpers for the embedding worker.

BGE-M3 encodes far faster in batches, so the worker drains up to batch_max
messages and encodes every chunk in one model call. Kafka redelivery stays
safe: saves are per-event upserts (idempotent) and offsets commit only after
the whole batch is stored.
"""

from .events import EmbeddingJobEvent, InvalidEvent


def collect_batch(kafka, batch_max: int, first_timeout: float = 1.0) -> list:
    """Poll up to batch_max messages: block for the first, drain the rest."""
    first = kafka.poll(first_timeout)
    if first is None:
        return []
    messages = [first]
    while len(messages) < batch_max:
        message = kafka.poll(0)
        if message is None:
            break
        messages.append(message)
    return messages


def split_vectors(chunks_per_event: list[int], vectors: list) -> list[list]:
    """Slice one flat encode output back into per-event chunk vectors."""
    if sum(chunks_per_event) != len(vectors):
        raise InvalidEvent(
            f"embedder returned {len(vectors)} vectors for {sum(chunks_per_event)} chunks"
        )
    grouped = []
    offset = 0
    for count in chunks_per_event:
        grouped.append(vectors[offset:offset + count])
        offset += count
    return grouped


def parse_valid(messages, parse=EmbeddingJobEvent.from_raw) -> tuple[list, list]:
    """Split raw messages into (valid events, invalid messages).

    Returns ([(message, event)], [message]): callers DLQ+commit the invalid
    ones individually and batch the rest.
    """
    valid, invalid = [], []
    for message in messages:
        try:
            valid.append((message, parse(message.value())))
        except InvalidEvent:
            invalid.append(message)
    return valid, invalid
