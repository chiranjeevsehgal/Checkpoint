"""Pure builders mapping source rows to search_documents rows.

Kept free of database and model imports so the mapping (and the occurred_at
fallback) is unit-testable without psycopg, torch, or TurboVec.
"""

import hashlib
from datetime import date, datetime, time, timezone

SOURCE_TRANSCRIPT = "transcript"
SOURCE_TODO = "todo"
SOURCE_REMINDER = "reminder"
SOURCE_INSIGHT = "insight"
SOURCE_SUMMARY = "summary"

SOURCE_TYPES = (SOURCE_TRANSCRIPT, SOURCE_TODO, SOURCE_REMINDER, SOURCE_INSIGHT, SOURCE_SUMMARY)

WHOLE_DOCUMENT = -1


def content_hash(content: str) -> str:
    return hashlib.sha256(content.encode("utf-8")).hexdigest()


def _base(
    user_id: str,
    source_type: str,
    source_id: str,
    chunk_index: int,
    content: str,
    occurred_at: datetime,
) -> dict:
    return {
        "user_id": user_id,
        "source_type": source_type,
        "source_id": source_id,
        "chunk_index": chunk_index,
        "content": content,
        "content_hash": content_hash(content),
        "occurred_at": occurred_at,
        "audio_id": None,
        "language": None,
        "recorded_at": None,
        "reminded_at": None,
        "is_done": None,
        "important": None,
        "period": None,
        "period_start": None,
        "model": None,
    }


def transcript_full(user_id, audio_id, text, language, recorded_at, created_at) -> dict:
    doc = _base(user_id, SOURCE_TRANSCRIPT, str(audio_id), WHOLE_DOCUMENT, text, recorded_at or created_at)
    doc.update(audio_id=audio_id, language=language, recorded_at=recorded_at)
    return doc


def transcript_chunk(user_id, audio_id, chunk_index, chunk_text, language, recorded_at, created_at) -> dict:
    doc = _base(user_id, SOURCE_TRANSCRIPT, str(audio_id), chunk_index, chunk_text, recorded_at or created_at)
    doc.update(audio_id=audio_id, language=language, recorded_at=recorded_at)
    return doc


def todo(user_id, audio_id, todo_id, text, recorded_at, created_at, is_done) -> dict:
    doc = _base(user_id, SOURCE_TODO, str(todo_id), WHOLE_DOCUMENT, text, recorded_at or created_at)
    doc.update(audio_id=audio_id, recorded_at=recorded_at, is_done=is_done)
    return doc


def reminder(user_id, audio_id, reminder_id, text, remind_at, created_at, important) -> dict:
    doc = _base(user_id, SOURCE_REMINDER, str(reminder_id), WHOLE_DOCUMENT, text, created_at)
    doc.update(audio_id=audio_id, reminded_at=remind_at, important=important)
    return doc


def insight(user_id, audio_id, insight_id, text, created_at) -> dict:
    doc = _base(user_id, SOURCE_INSIGHT, str(insight_id), WHOLE_DOCUMENT, text, created_at)
    doc.update(audio_id=audio_id)
    return doc


def summary(user_id, period, period_start, text, model) -> dict:
    source_id = f"{period}:{period_start.isoformat()}"
    occurred = datetime.combine(period_start, time.min, tzinfo=timezone.utc)
    doc = _base(user_id, SOURCE_SUMMARY, source_id, WHOLE_DOCUMENT, text, occurred)
    doc.update(period=period, period_start=period_start, model=model)
    return doc


def summary_source_id(period: str, period_start: date) -> str:
    return f"{period}:{period_start.isoformat()}"
