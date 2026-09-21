"""Pure builder for RETRY_REQUESTED handoffs to the central retry service.

Kept free of Kafka imports so it can be unit-tested without confluent_kafka.
"""

import json
import uuid
from datetime import datetime, timezone


def build_retry_event(
    source_service: str,
    source_topic: str,
    stage: str,
    error_code: str,
    error_message: str,
    original_payload: bytes,
) -> dict:
    """Wrap a failed event for delayed re-delivery by the retry service.

    The original rides along inside data.original_event; unparseable bytes are
    passed through as text so the retry topic's DLQ receives intact evidence.
    """
    try:
        original = json.loads(original_payload)
    except (json.JSONDecodeError, UnicodeDecodeError):
        original = original_payload.decode("utf-8", errors="replace")
    return {
        "schema_version": 2,
        "event_id": str(uuid.uuid4()),
        "event_type": "RETRY_REQUESTED",
        "occurred_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "Z",
        "data": {
            "source_service": source_service,
            "source_topic": source_topic,
            "stage": stage,
            "error_code": error_code,
            "error_message": error_message,
            "original_event": original,
        },
    }
