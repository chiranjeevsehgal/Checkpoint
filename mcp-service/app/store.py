"""Read-only access to search_documents and user_settings for the MCP tools.

Connects as the checkpoint_mcp role and sets app.user_id per transaction, so
RLS guarantees a query can only ever see the authenticated account's rows.
"""

import threading
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime

try:
    import psycopg
    from psycopg.rows import dict_row
except ModuleNotFoundError:  # pragma: no cover - exercised only without psycopg
    psycopg = None
    dict_row = None

_SELECT_COLUMNS = (
    "id, source_type, source_id, audio_id, chunk_index, content, language, "
    "occurred_at, recorded_at, reminded_at, is_done, important, period, period_start, model"
)


@dataclass
class DocumentQuery:
    source_types: tuple[str, ...] | None = None
    start: datetime | None = None
    end: datetime | None = None
    is_done: bool | None = None
    reminded_from: datetime | None = None
    reminded_to: datetime | None = None
    period: str | None = None
    audio_id: str | None = None
    chunk_index: int | None = None
    order: str = "occurred_at DESC"
    limit: int = 50


class ReadStore:
    def __init__(self, dsn: str) -> None:
        self._dsn = dsn
        self._conn = None
        self._lock = threading.Lock()

    def connect(self) -> None:
        if psycopg is None:
            raise RuntimeError("psycopg is not installed")
        self._conn = psycopg.connect(self._dsn, autocommit=True)

    def close(self) -> None:
        if self._conn is not None:
            self._conn.close()
            self._conn = None

    @contextmanager
    def _user_cursor(self, user_id: str):
        with self._lock:
            with self._conn.transaction():
                with self._conn.cursor(row_factory=dict_row) as cur:
                    cur.execute("SELECT set_config('app.user_id', %s, true)", (user_id,))
                    yield cur

    def get_timezone(self, user_id: str) -> str | None:
        with self._user_cursor(user_id) as cur:
            cur.execute("SELECT timezone FROM user_settings WHERE user_id = %s", (user_id,))
            row = cur.fetchone()
        return row["timezone"] if row else None

    def fetch(self, user_id: str, query: DocumentQuery) -> list[dict]:
        clauses = ["user_id = %(user_id)s"]
        params: dict = {"user_id": user_id, "limit": query.limit}

        if query.source_types:
            params["source_types"] = list(query.source_types)
            clauses.append("source_type = ANY(%(source_types)s)")
        if query.start is not None:
            params["start"] = query.start
            clauses.append("occurred_at >= %(start)s")
        if query.end is not None:
            params["end"] = query.end
            clauses.append("occurred_at <= %(end)s")
        if query.is_done is not None:
            params["is_done"] = query.is_done
            clauses.append("is_done = %(is_done)s")
        if query.reminded_from is not None:
            params["reminded_from"] = query.reminded_from
            clauses.append("reminded_at >= %(reminded_from)s")
        if query.reminded_to is not None:
            params["reminded_to"] = query.reminded_to
            clauses.append("reminded_at <= %(reminded_to)s")
        if query.period is not None:
            params["period"] = query.period
            clauses.append("period = %(period)s")
        if query.audio_id is not None:
            params["audio_id"] = query.audio_id
            clauses.append("audio_id = %(audio_id)s::uuid")
        if query.chunk_index is not None:
            params["chunk_index"] = query.chunk_index
            clauses.append("chunk_index = %(chunk_index)s")

        statement = (
            f"SELECT {_SELECT_COLUMNS} FROM search_documents "
            f"WHERE {' AND '.join(clauses)} "
            f"ORDER BY {query.order} LIMIT %(limit)s"
        )
        with self._user_cursor(user_id) as cur:
            cur.execute(statement, params)
            return cur.fetchall()
