"""Pure response shaping for the MCP tools.

Free of mcp/auth/DB imports so the envelope and formatting stay unit-testable
without the MCP or ML dependencies, mirroring timeutil.py and documents.py.
"""

from . import timeutil


def envelope(payload: dict, zone) -> dict:
    response = dict(payload)
    response["now"] = timeutil.dual(timeutil.utc_now(), zone)
    if zone is not None:
        response["timezone"] = str(zone)
    return response


def error(code: str, message: str, zone=None) -> dict:
    return envelope({"error": message, "code": code}, zone)


def with_empty_note(payload: dict, message: str) -> dict:
    if payload.get("count") == 0:
        payload["message"] = message
    return payload


def format_row(row: dict, zone) -> dict:
    return {
        "type": row["source_type"],
        "text": row["content"],
        "source_id": row["source_id"],
        "language": row["language"],
        "chunk_index": row["chunk_index"],
        "important": row["important"],
        "occurred_at": timeutil.dual(row["occurred_at"], zone),
        "recorded_at": timeutil.dual(row["recorded_at"], zone),
        "remind_at": timeutil.dual(row["reminded_at"], zone),
        "is_done": row["is_done"],
        "period": row["period"],
        "period_start": row["period_start"].isoformat() if row["period_start"] else None,
        "audio_id": str(row["audio_id"]) if row["audio_id"] else None,
    }


def format_document(document, zone) -> dict:
    meta = document.meta or {}
    return {
        "type": meta.get("source_type"),
        "text": document.content,
        "source_id": meta.get("source_id"),
        "language": meta.get("language"),
        "chunk_index": meta.get("chunk_index"),
        "important": meta.get("important"),
        "score": round(document.score, 4) if document.score is not None else None,
        "occurred_at": timeutil.dual(timeutil.loads(meta.get("occurred_at")), zone),
        "recorded_at": timeutil.dual(timeutil.loads(meta.get("recorded_at")), zone),
        "remind_at": timeutil.dual(timeutil.loads(meta.get("reminded_at")), zone),
        "is_done": meta.get("is_done"),
        "period": meta.get("period"),
        "period_start": meta.get("period_start"),
        "audio_id": meta.get("audio_id"),
    }
