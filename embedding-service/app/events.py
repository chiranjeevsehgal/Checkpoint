import json
from dataclasses import dataclass

SCHEMA_VERSION = 2
EVENT_TYPE_EMBEDDING_REQUESTED = "EMBEDDING_REQUESTED"

class InvalidEvent(Exception):
    """Raised for poison messages: bad JSON, wrong envelope, or unusable data.

    These never succeed on redelivery, so the caller routes them to the DLQ
    instead of retrying.
    """


@dataclass(frozen=True)
class EmbeddingJobEvent:
    event_id: str
    audio_id: str
    user_id: str
    text: str
    language: str

    @classmethod
    def from_raw(cls, raw: bytes) -> "EmbeddingJobEvent":
        try:
            payload = json.loads(raw)
        except (json.JSONDecodeError, UnicodeDecodeError) as exc:
            raise InvalidEvent(f"invalid JSON: {exc}") from exc

        if not isinstance(payload, dict):
            raise InvalidEvent("payload is not a JSON object")

        schema_version = payload.get("schema_version")
        if schema_version != SCHEMA_VERSION:
            raise InvalidEvent(f"unsupported schema_version: {schema_version!r}")

        event_type = payload.get("event_type")
        if event_type != EVENT_TYPE_EMBEDDING_REQUESTED:
            raise InvalidEvent(f"unexpected event_type: {event_type!r}")

        data = payload.get("data")
        if not isinstance(data, dict):
            raise InvalidEvent("missing data object")

        event_id = payload.get("event_id")
        if not isinstance(event_id, str) or not event_id:
            raise InvalidEvent("missing event_id")

        audio_id = data.get("audio_id")
        if not isinstance(audio_id, str) or not audio_id:
            raise InvalidEvent("missing audio_id")

        user_id = data.get("user_id")
        if not isinstance(user_id, str) or not user_id:
            raise InvalidEvent("missing user_id")

        text = data.get("text")
        if not isinstance(text, str):
            raise InvalidEvent("missing text")

        language = data.get("language")
        if language is not None and not isinstance(language, str):
            raise InvalidEvent("language must be a string")

        return cls(
            event_id=event_id,
            audio_id=audio_id,
            user_id=user_id,
            text=text,
            language=language or "",
        )
