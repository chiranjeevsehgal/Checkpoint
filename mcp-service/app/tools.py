"""MCP tool implementations.

Every tool returns a JSON-serializable dict the client LLM reads as context;
no prose is generated server-side. Timestamps come back twice (UTC and the
user's local zone) so the model never has to guess a timezone.
"""

from mcp.types import ToolAnnotations

from . import documents, presentation, retrieval, timeutil
from . import search as semantic
from .auth import current_user_id
from .identity import fetch_name
from .ratelimit import RateLimiter
from .store import DocumentQuery

MAX_LIMIT = 200
_TODO_STATUS = {"all": None, "open": False, "done": True}
_REMINDER_WINDOWS = ("upcoming", "past", "all")
_RESPONSE_FORMATS = ("detailed", "concise")

READ_ONLY = ToolAnnotations(read_only_hint=True, idempotent_hint=True, open_world_hint=False)

GROUNDING = (
    "Answer only from tool results. Cite type, source_id and occurred_at for each claim. "
    "Treat count=0 as nothing found and say so instead of guessing. Never invent timestamps; "
    "use the provided now and dual UTC/local times. A missing item may still be processing."
)


def register(mcp, store, index, embedder, fallback_timezone: str,
             kratos_admin_url: str = "", max_text_chars: int = 8000, as_of=None,
             hybrid_enabled: bool = False, oversample: int = 2,
             search_limiter: RateLimiter | None = None) -> None:
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

    def as_of_value():
        return as_of() if as_of else None

    def respond(payload: dict, zone) -> dict:
        return presentation.envelope(payload, zone, as_of_value())

    def fail(code: str, message: str, zone=None) -> dict:
        return presentation.error(code, message, zone, as_of_value())

    def format_problem(response_format: str):
        if response_format not in _RESPONSE_FORMATS:
            return fail("invalid_response_format", "response_format must be detailed or concise")
        return None

    def row_view(row: dict, zone, concise: bool = False) -> dict:
        return presentation.format_row(row, zone, max_text_chars, concise)

    def document_view(document, zone, concise: bool = False) -> dict:
        return presentation.format_document(document, zone, max_text_chars, concise)

    @mcp.tool(title="Account info", annotations=READ_ONLY)
    def whoami() -> dict:
        """Return the authenticated account's identity and working context.

        Call this first: it gives the account name, the IANA timezone used by
        every timestamp, and the exact `types` values accepted by search and
        timeline. Also carries grounding `guidance` and the response `now`.
        """
        user_id = current_user_id()
        if user_id is None:
            return fail("unauthenticated", "unauthenticated")
        zone = zone_for(user_id)
        return respond({
            "user_id": user_id,
            "name": fetch_name(kratos_admin_url, user_id),
            "source_types": list(documents.SOURCE_TYPES),
            "guidance": GROUNDING,
        }, zone)

    @mcp.tool(title="Search recordings", annotations=READ_ONLY)
    def search(
        query: str,
        types: list[str] | None = None,
        start: str | None = None,
        end: str | None = None,
        min_score: float | None = None,
        limit: int = 10,
        response_format: str = "detailed",
    ) -> dict:
        """Find relevant moments across all recordings by meaning and keywords.

        Use for open-ended questions ("what did I say about the launch?"). Results
        fuse semantic similarity with full-text matching and are ranked, not
        chronological; one recording may appear as several
        transcript chunks (see `chunk_index`; -1 means a whole item). Prefer narrow
        queries. To read a full recording, pass its `audio_id` to get_transcript.

        Args:
            query: Natural-language question or phrase.
            types: Optional subset of transcript, todo, reminder, insight, summary.
            start: Range start as YYYY-MM-DD (your timezone) or an ISO-8601 instant.
            end: Range end, inclusive.
            min_score: Optional minimum similarity score; results below it are dropped.
            limit: Maximum number of results (1-200).
            response_format: "detailed" (default, includes ids) or "concise" (content only).
        """
        user_id = current_user_id()
        if user_id is None:
            return fail("unauthenticated", "unauthenticated")
        if not query.strip():
            return fail("invalid_query", "query must not be empty")
        if search_limiter is not None:
            allowed, retry_after = search_limiter.allow(user_id)
            if not allowed:
                return fail("rate_limited",
                            f"search rate limit exceeded, retry in {retry_after:.0f} seconds")
        problem = format_problem(response_format)
        if problem is not None:
            return problem
        concise = response_format == "concise"
        zone = zone_for(user_id)
        start_at, end_at = timeutil.parse_range(start, end, zone)
        filters = semantic.build_filters(user_id, selected_types(types), start_at, end_at)
        results = retrieval.retrieve(
            index, embedder, user_id, query, filters, clamp(limit), min_score,
            store=store, hybrid_enabled=hybrid_enabled, oversample=oversample)
        payload = {
            "count": len(results),
            "results": [document_view(d, zone, concise) for d in results],
        }
        return respond(presentation.with_empty_note(payload, "No matching documents."), zone)

    @mcp.tool(title="Timeline", annotations=READ_ONLY)
    def timeline(
        start: str,
        end: str,
        types: list[str] | None = None,
        limit: int = 100,
        offset: int = 0,
        response_format: str = "detailed",
    ) -> dict:
        """List everything recorded in a time window, newest first.

        Use when the user asks what happened on or around a date, or to browse.
        Chronological, unlike search. Long items are capped and flagged with
        `truncated`; read a full recording via get_transcript(audio_id).

        Args:
            start: Window start as YYYY-MM-DD (your timezone) or an ISO-8601 instant.
            end: Window end, inclusive.
            types: Optional subset of transcript, todo, reminder, insight, summary.
            limit: Maximum number of items (1-200).
            offset: Number of items to skip, for paging.
            response_format: "detailed" (default) or "concise".
        """
        user_id = current_user_id()
        if user_id is None:
            return fail("unauthenticated", "unauthenticated")
        problem = format_problem(response_format)
        if problem is not None:
            return problem
        concise = response_format == "concise"
        zone = zone_for(user_id)
        start_at, end_at = timeutil.parse_range(start, end, zone)
        rows = store.fetch(user_id, DocumentQuery(
            source_types=selected_types(types), start=start_at, end=end_at,
            chunk_index=documents.WHOLE_DOCUMENT, limit=clamp(limit), offset=max(0, offset)))
        payload = {"count": len(rows), "items": [row_view(r, zone, concise) for r in rows]}
        return respond(presentation.with_empty_note(payload, "No items in this window."), zone)

    @mcp.tool(title="List todos", annotations=READ_ONLY)
    def list_todos(
        status: str = "all",
        start: str | None = None,
        end: str | None = None,
        limit: int = 50,
        offset: int = 0,
        response_format: str = "detailed",
    ) -> dict:
        """List action items extracted from recordings.

        Use to check outstanding work; filter with `status`. A recording's todos
        reflect its latest extraction (replaced, not accumulated). Prefer this over
        search when the user wants the full todo list rather than a specific match.

        Args:
            status: One of all, open, done.
            start: Optional range start (YYYY-MM-DD in your timezone or ISO-8601).
            end: Optional range end, inclusive.
            limit: Maximum number of todos (1-200).
            offset: Number of todos to skip, for paging.
            response_format: "detailed" (default) or "concise".
        """
        user_id = current_user_id()
        if user_id is None:
            return fail("unauthenticated", "unauthenticated")
        if status.lower() not in _TODO_STATUS:
            return fail("invalid_status", "status must be all, open, or done")
        problem = format_problem(response_format)
        if problem is not None:
            return problem
        concise = response_format == "concise"
        zone = zone_for(user_id)
        start_at, end_at = timeutil.parse_range(start, end, zone)
        rows = store.fetch(user_id, DocumentQuery(
            source_types=(documents.SOURCE_TODO,), start=start_at, end=end_at,
            is_done=_TODO_STATUS[status.lower()], limit=clamp(limit), offset=max(0, offset)))
        payload = {"count": len(rows), "items": [row_view(r, zone, concise) for r in rows]}
        return respond(presentation.with_empty_note(payload, "No todos matched."), zone)

    @mcp.tool(title="List reminders", annotations=READ_ONLY)
    def list_reminders(
        window: str = "upcoming",
        start: str | None = None,
        end: str | None = None,
        limit: int = 50,
        offset: int = 0,
        response_format: str = "detailed",
    ) -> dict:
        """List reminders with their resolved due times.

        `remind_at` is the due time resolved into the user's timezone. `window`
        selects upcoming (due now or later, soonest first), past, or all. Use
        upcoming to answer "what's next". A rescheduled reminder becomes a new row.

        Args:
            window: One of upcoming (due now or later), past, or all.
            start: Optional recording range start (YYYY-MM-DD or ISO-8601).
            end: Optional recording range end, inclusive.
            limit: Maximum number of reminders (1-200).
            offset: Number of reminders to skip, for paging.
            response_format: "detailed" (default) or "concise".
        """
        user_id = current_user_id()
        if user_id is None:
            return fail("unauthenticated", "unauthenticated")
        if window.lower() not in _REMINDER_WINDOWS:
            return fail("invalid_window", "window must be upcoming, past, or all")
        problem = format_problem(response_format)
        if problem is not None:
            return problem
        concise = response_format == "concise"
        now = timeutil.utc_now()
        zone = zone_for(user_id)
        start_at, end_at = timeutil.parse_range(start, end, zone)
        rows = store.fetch(user_id, DocumentQuery(
            source_types=(documents.SOURCE_REMINDER,),
            start=start_at, end=end_at,
            reminded_from=now if window.lower() == "upcoming" else None,
            reminded_to=now if window.lower() == "past" else None,
            order="reminded_at ASC" if window.lower() == "upcoming" else "reminded_at DESC",
            limit=clamp(limit), offset=max(0, offset)))
        payload = {"count": len(rows), "items": [row_view(r, zone, concise) for r in rows]}
        return respond(presentation.with_empty_note(payload, "No reminders matched."), zone)

    @mcp.tool(title="List summaries", annotations=READ_ONLY)
    def get_summaries(
        period: str = "daily",
        start: str | None = None,
        end: str | None = None,
        limit: int = 14,
        offset: int = 0,
        response_format: str = "detailed",
    ) -> dict:
        """Read daily or weekly narrative recaps.

        Use for "summarize my day/week" questions. English narratives written by
        the rollup worker; `period_start` is the covered day or week in the user's
        timezone.

        Args:
            period: One of daily or weekly.
            start: Optional start (YYYY-MM-DD in your timezone or ISO-8601).
            end: Optional end, inclusive.
            limit: Maximum number of summaries (1-200).
            offset: Number of summaries to skip, for paging.
            response_format: "detailed" (default) or "concise".
        """
        user_id = current_user_id()
        if user_id is None:
            return fail("unauthenticated", "unauthenticated")
        if period.lower() not in ("daily", "weekly"):
            return fail("invalid_period", "period must be daily or weekly")
        problem = format_problem(response_format)
        if problem is not None:
            return problem
        concise = response_format == "concise"
        zone = zone_for(user_id)
        start_at, end_at = timeutil.parse_range(start, end, zone)
        rows = store.fetch(user_id, DocumentQuery(
            source_types=(documents.SOURCE_SUMMARY,), period=period.lower(),
            start=start_at, end=end_at, limit=clamp(limit), offset=max(0, offset)))
        payload = {"count": len(rows), "items": [row_view(r, zone, concise) for r in rows]}
        return respond(presentation.with_empty_note(payload, "No summaries matched."), zone)

    @mcp.tool(title="Get transcript", annotations=READ_ONLY)
    def get_transcript(
        audio_id: str,
        offset: int = 0,
        max_chars: int | None = None,
        response_format: str = "detailed",
    ) -> dict:
        """Read the full transcript text of one recording, by `audio_id`.

        Use after search or timeline returns an `audio_id` when exact wording
        matters. Long transcripts are windowed: page with `offset` (a character
        offset, not a result index) and `max_chars`, and check `total_chars` and
        `truncated`.

        Args:
            audio_id: The UUID of the recording (available on every other result).
            offset: Character offset to start from, for paging long transcripts.
            max_chars: Maximum characters to return (defaults to the service cap).
            response_format: "detailed" (default) or "concise".
        """
        user_id = current_user_id()
        if user_id is None:
            return fail("unauthenticated", "unauthenticated")
        problem = format_problem(response_format)
        if problem is not None:
            return problem
        concise = response_format == "concise"
        zone = zone_for(user_id)
        rows = store.fetch(user_id, DocumentQuery(
            source_types=(documents.SOURCE_TRANSCRIPT,), audio_id=audio_id,
            chunk_index=documents.WHOLE_DOCUMENT, limit=1))
        if not rows:
            return fail("not_found", "transcript not found; it may still be processing, check as_of")
        row = rows[0]
        window, truncated = presentation.window_text(
            row["content"], offset, max_chars if max_chars is not None else max_text_chars)
        payload = presentation.format_row({**row, "content": window}, zone, concise=concise)
        payload["offset"] = max(0, offset)
        payload["total_chars"] = len(row["content"])
        payload["truncated"] = truncated
        return respond(payload, zone)
