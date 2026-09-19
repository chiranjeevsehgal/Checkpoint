"""MCP tool implementations.

Every tool returns a JSON-serializable dict the client LLM reads as context;
no prose is generated server-side. Timestamps come back twice (UTC and the
user's local zone) so the model never has to guess a timezone.
"""

from datetime import datetime, timezone

from . import documents, search, timeutil
from .auth import current_user_id
from .store import DocumentQuery

MAX_LIMIT = 200
_TODO_STATUS = {"all": None, "open": False, "done": True}
_REMINDER_WINDOWS = ("upcoming", "past", "all")


def register(mcp, store, index, embedder, fallback_timezone: str) -> None:
    def clamp(limit: int) -> int:
        return max(1, min(limit, MAX_LIMIT))

    def zone_for(user_id: str):
        return timeutil.resolve_zone(store.get_timezone(user_id), fallback_timezone)

    def selected_types(types: list[str] | None):
        if not types:
            return None
        requested = {t.strip().lower() for t in types}
        chosen = tuple(t for t in documents.SOURCE_TYPES if t in requested)
        return chosen or None

    def format_row(row: dict, zone) -> dict:
        return {
            "type": row["source_type"],
            "text": row["content"],
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
            "score": round(document.score, 4) if document.score is not None else None,
            "occurred_at": timeutil.dual(timeutil.loads(meta.get("occurred_at")), zone),
            "recorded_at": timeutil.dual(timeutil.loads(meta.get("recorded_at")), zone),
            "remind_at": timeutil.dual(timeutil.loads(meta.get("reminded_at")), zone),
            "is_done": meta.get("is_done"),
            "period": meta.get("period"),
            "period_start": meta.get("period_start"),
            "audio_id": meta.get("audio_id"),
        }

    @mcp.tool()
    def whoami() -> dict:
        """Report the authenticated account, its IANA timezone, and the searchable types."""
        user_id = current_user_id()
        if user_id is None:
            return {"error": "unauthenticated"}
        zone = zone_for(user_id)
        return {"user_id": user_id, "timezone": str(zone), "source_types": list(documents.SOURCE_TYPES)}

    @mcp.tool()
    def search(
        query: str,
        types: list[str] | None = None,
        start: str | None = None,
        end: str | None = None,
        limit: int = 10,
    ) -> dict:
        """Semantic search across transcripts, todos, reminders, insights and summaries.

        Args:
            query: Natural-language question or phrase.
            types: Optional subset of transcript, todo, reminder, insight, summary.
            start: Range start as YYYY-MM-DD (your timezone) or an ISO-8601 instant.
            end: Range end, inclusive.
            limit: Maximum number of results (1-200).
        """
        user_id = current_user_id()
        if user_id is None:
            return {"error": "unauthenticated"}
        if not query.strip():
            return {"error": "query must not be empty"}
        zone = zone_for(user_id)
        start_at, end_at = timeutil.parse_range(start, end, zone)
        filters = search.build_filters(user_id, selected_types(types), start_at, end_at)
        vector = embedder.embed([query])[0]
        results = index.search(vector, filters, clamp(limit))
        return {"count": len(results), "results": [format_document(d, zone) for d in results]}

    @mcp.tool()
    def timeline(
        start: str,
        end: str,
        types: list[str] | None = None,
        limit: int = 100,
    ) -> dict:
        """Everything recorded in a time window, newest first.

        Args:
            start: Window start as YYYY-MM-DD (your timezone) or an ISO-8601 instant.
            end: Window end, inclusive.
            types: Optional subset of transcript, todo, reminder, insight, summary.
            limit: Maximum number of items (1-200).
        """
        user_id = current_user_id()
        if user_id is None:
            return {"error": "unauthenticated"}
        zone = zone_for(user_id)
        start_at, end_at = timeutil.parse_range(start, end, zone)
        rows = store.fetch(user_id, DocumentQuery(
            source_types=selected_types(types), start=start_at, end=end_at, limit=clamp(limit)))
        return {"count": len(rows), "items": [format_row(r, zone) for r in rows]}

    @mcp.tool()
    def list_todos(
        status: str = "all",
        start: str | None = None,
        end: str | None = None,
        limit: int = 50,
    ) -> dict:
        """Todos extracted from recordings.

        Args:
            status: One of all, open, done.
            start: Optional range start (YYYY-MM-DD in your timezone or ISO-8601).
            end: Optional range end, inclusive.
            limit: Maximum number of todos (1-200).
        """
        user_id = current_user_id()
        if user_id is None:
            return {"error": "unauthenticated"}
        if status.lower() not in _TODO_STATUS:
            return {"error": "status must be all, open, or done"}
        zone = zone_for(user_id)
        start_at, end_at = timeutil.parse_range(start, end, zone)
        rows = store.fetch(user_id, DocumentQuery(
            source_types=(documents.SOURCE_TODO,), start=start_at, end=end_at,
            is_done=_TODO_STATUS[status.lower()], limit=clamp(limit)))
        return {"count": len(rows), "items": [format_row(r, zone) for r in rows]}

    @mcp.tool()
    def list_reminders(
        window: str = "upcoming",
        start: str | None = None,
        end: str | None = None,
        limit: int = 50,
    ) -> dict:
        """Reminders with their resolved due times.

        Args:
            window: One of upcoming (due now or later), past, or all.
            start: Optional recording range start (YYYY-MM-DD or ISO-8601).
            end: Optional recording range end, inclusive.
            limit: Maximum number of reminders (1-200).
        """
        user_id = current_user_id()
        if user_id is None:
            return {"error": "unauthenticated"}
        if window.lower() not in _REMINDER_WINDOWS:
            return {"error": "window must be upcoming, past, or all"}
        now = datetime.now(timezone.utc)
        zone = zone_for(user_id)
        start_at, end_at = timeutil.parse_range(start, end, zone)
        rows = store.fetch(user_id, DocumentQuery(
            source_types=(documents.SOURCE_REMINDER,),
            start=start_at, end=end_at,
            reminded_from=now if window.lower() == "upcoming" else None,
            reminded_to=now if window.lower() == "past" else None,
            order="reminded_at ASC" if window.lower() == "upcoming" else "reminded_at DESC",
            limit=clamp(limit)))
        return {"count": len(rows), "items": [format_row(r, zone) for r in rows]}

    @mcp.tool()
    def get_summaries(
        period: str = "daily",
        start: str | None = None,
        end: str | None = None,
        limit: int = 14,
    ) -> dict:
        """Daily or weekly narrative summaries.

        Args:
            period: One of daily or weekly.
            start: Optional start (YYYY-MM-DD in your timezone or ISO-8601).
            end: Optional end, inclusive.
            limit: Maximum number of summaries (1-200).
        """
        user_id = current_user_id()
        if user_id is None:
            return {"error": "unauthenticated"}
        if period.lower() not in ("daily", "weekly"):
            return {"error": "period must be daily or weekly"}
        zone = zone_for(user_id)
        start_at, end_at = timeutil.parse_range(start, end, zone)
        rows = store.fetch(user_id, DocumentQuery(
            source_types=(documents.SOURCE_SUMMARY,), period=period.lower(),
            start=start_at, end=end_at, limit=clamp(limit)))
        return {"count": len(rows), "items": [format_row(r, zone) for r in rows]}

    @mcp.tool()
    def get_transcript(audio_id: str) -> dict:
        """Full transcript text for one recording, looked up by its audio_id.

        Args:
            audio_id: The UUID of the recording (available on every other result).
        """
        user_id = current_user_id()
        if user_id is None:
            return {"error": "unauthenticated"}
        zone = zone_for(user_id)
        rows = store.fetch(user_id, DocumentQuery(
            source_types=(documents.SOURCE_TRANSCRIPT,), audio_id=audio_id,
            chunk_index=documents.WHOLE_DOCUMENT, limit=1))
        if not rows:
            return {"error": "transcript not found"}
        return format_row(rows[0], zone)
