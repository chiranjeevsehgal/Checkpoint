"""Keeps search_documents and the in-process TurboVec index in sync.

Runs as checkpoint_worker (BYPASSRLS) so it can index every account; the MCP
itself only reads as checkpoint_mcp under RLS. Sources are replace-shaped
(extraction deletes and reinserts per audio), so a changed audio is rebuilt
wholesale, which also drops rows that no longer exist.
"""

import logging
from datetime import datetime, timezone

try:
    import psycopg
    from pgvector import Vector
    from pgvector.psycopg import register_vector
    from psycopg.rows import dict_row
except ModuleNotFoundError:  # pragma: no cover - only without the ML extras
    psycopg = None
    Vector = None
    register_vector = None
    dict_row = None

from . import documents, search

log = logging.getLogger(__name__)


def _plain_embedding(value):
    """pgvector may hand back a Vector, ndarray or list; normalise to a list."""
    if value is None or isinstance(value, list):
        return value
    if hasattr(value, "to_list"):
        return value.to_list()
    return list(value)


SOURCE_WATERMARK = "sources"
SUMMARY_WATERMARK = "summaries"
EPOCH = datetime(1970, 1, 1, tzinfo=timezone.utc)

_ROW_COLUMNS = (
    "id, user_id, source_type, source_id, audio_id, chunk_index, content, language, "
    "occurred_at, recorded_at, reminded_at, is_done, important, period, period_start, model"
)

_INSERT = """
    INSERT INTO search_documents (
        user_id, source_type, source_id, audio_id, chunk_index, content, language,
        occurred_at, recorded_at, reminded_at, is_done, important, period, period_start,
        model, content_hash, embedding, indexed_at
    ) VALUES (
        %(user_id)s::uuid, %(source_type)s, %(source_id)s, %(audio_id)s::uuid, %(chunk_index)s, %(content)s, %(language)s,
        %(occurred_at)s, %(recorded_at)s, %(reminded_at)s, %(is_done)s, %(important)s, %(period)s, %(period_start)s,
        %(model)s, %(content_hash)s, %(embedding)s, %(indexed_at)s
    )
    ON CONFLICT (user_id, source_type, source_id, chunk_index) DO UPDATE SET
        content = EXCLUDED.content, content_hash = EXCLUDED.content_hash,
        embedding = EXCLUDED.embedding, indexed_at = EXCLUDED.indexed_at, updated_at = NOW()
    RETURNING id
"""

# reminders/insights keep audio_id as TEXT (created after the tenant-id
# hardening migration), so every branch casts to the canonical UUID type.
# embeddings is included because chunks land after the transcript; without
# it an audio synced before its embeddings would never get its chunk rows.
_CHANGED_AUDIO = """
    SELECT DISTINCT audio_id FROM (
        SELECT audio_id::uuid AS audio_id, created_at FROM transcripts
        UNION ALL SELECT audio_id::uuid, created_at FROM todos
        UNION ALL SELECT audio_id::uuid, created_at FROM reminders
        UNION ALL SELECT audio_id::uuid, created_at FROM insights
        UNION ALL SELECT audio_id::uuid, created_at FROM embeddings
    ) changed
    WHERE created_at > %s AND created_at <= %s AND audio_id IS NOT NULL
"""


class Indexer:
    def __init__(self, dsn: str, embedder, index: "search.DocumentIndex", batch_size: int = 32) -> None:
        self._dsn = dsn
        self._embedder = embedder
        self._index = index
        self._batch_size = batch_size
        self._conn = None
        self.last_synced_at = None

    def connect(self) -> None:
        if psycopg is None:
            raise RuntimeError("psycopg is not installed")
        self._conn = psycopg.connect(self._dsn, autocommit=True)
        register_vector(self._conn)

    def close(self) -> None:
        if self._conn is not None:
            self._conn.close()
            self._conn = None

    def reset(self) -> None:
        self.close()
        self.connect()

    def rebuild_index(self) -> None:
        with self._conn.cursor(row_factory=dict_row) as cur:
            cur.execute(
                f"SELECT {_ROW_COLUMNS}, embedding FROM search_documents WHERE embedding IS NOT NULL"
            )
            rows = cur.fetchall()
        self._index.rebuild([search.row_to_document(row, _plain_embedding(row.get("embedding"))) for row in rows])

    def run_once(self) -> None:
        cycle_now = self._now()
        self._sync_sources(cycle_now)
        self._sync_summaries(cycle_now)
        self._embed_pending()
        self.last_synced_at = cycle_now

    def _sync_sources(self, cycle_now: datetime) -> None:
        watermark = self._watermark(SOURCE_WATERMARK)
        with self._conn.cursor(row_factory=dict_row) as cur:
            cur.execute(_CHANGED_AUDIO, (watermark, cycle_now))
            audio_ids = [row["audio_id"] for row in cur.fetchall()]

        to_index: list = []
        removed: list[int] = []
        for audio_id in audio_ids:
            removed.extend(self._rebuild_audio(audio_id, to_index))
        self._set_watermark(SOURCE_WATERMARK, cycle_now)
        self._index.remove(removed)
        self._index.upsert(to_index)
        if audio_ids:
            log.info("indexed changed audio", extra={"count": len(audio_ids)})

    def _rebuild_audio(self, audio_id, to_index: list) -> list[int]:
        removed: list[int] = []
        with self._conn.transaction():
            with self._conn.cursor(row_factory=dict_row) as cur:
                cur.execute(
                    "DELETE FROM search_documents WHERE audio_id = %s "
                    "RETURNING id, (embedding IS NOT NULL) AS embedded",
                    (audio_id,),
                )
                removed = [row["id"] for row in cur.fetchall() if row["embedded"]]

                transcript = self._one(cur, "SELECT user_id, text, language, recorded_at, created_at "
                                            "FROM transcripts WHERE audio_id = %s", audio_id)
                if transcript is not None:
                    self._insert(cur, documents.transcript_full(
                        transcript["user_id"], audio_id, transcript["text"], transcript["language"],
                        transcript["recorded_at"], transcript["created_at"]), None, True, to_index)
                    for chunk in self._all(cur, "SELECT chunk_index, chunk_text, language, embedding "
                                                "FROM embeddings WHERE audio_id = %s ORDER BY chunk_index", audio_id):
                        self._insert(cur, documents.transcript_chunk(
                            transcript["user_id"], audio_id, chunk["chunk_index"], chunk["chunk_text"],
                            chunk["language"], transcript["recorded_at"], transcript["created_at"]),
                            chunk["embedding"], True, to_index)

                for row in self._all(cur, "SELECT id, user_id, text, recorded_at, created_at, is_done "
                                          "FROM todos WHERE audio_id = %s", audio_id):
                    self._insert(cur, documents.todo(
                        row["user_id"], audio_id, row["id"], row["text"], row["recorded_at"],
                        row["created_at"], row["is_done"]), None, False, to_index)
                for row in self._all(cur, "SELECT id, user_id, text, remind_at, created_at, important "
                                          "FROM reminders WHERE audio_id::uuid = %s", audio_id):
                    self._insert(cur, documents.reminder(
                        row["user_id"], audio_id, row["id"], row["text"], row["remind_at"],
                        row["created_at"], row["important"]), None, False, to_index)
                for row in self._all(cur, "SELECT id, user_id, text, created_at "
                                          "FROM insights WHERE audio_id::uuid = %s", audio_id):
                    self._insert(cur, documents.insight(
                        row["user_id"], audio_id, row["id"], row["text"], row["created_at"]),
                        None, False, to_index)
        return removed

    def _sync_summaries(self, cycle_now: datetime) -> None:
        watermark = self._watermark(SUMMARY_WATERMARK)
        with self._conn.cursor(row_factory=dict_row) as cur:
            cur.execute(
                "SELECT user_id, period, period_start, text, model FROM summaries "
                "WHERE updated_at > %s AND updated_at <= %s",
                (watermark, cycle_now),
            )
            summaries = cur.fetchall()

        to_index: list = []
        removed: list[int] = []
        for row in summaries:
            user_id = str(row["user_id"])
            source_id = documents.summary_source_id(row["period"], row["period_start"])
            with self._conn.transaction():
                with self._conn.cursor(row_factory=dict_row) as cur:
                    cur.execute(
                        "DELETE FROM search_documents WHERE user_id = %s::uuid "
                        "AND source_type = 'summary' AND source_id = %s "
                        "RETURNING id, (embedding IS NOT NULL) AS embedded",
                        (user_id, source_id),
                    )
                    removed.extend(r["id"] for r in cur.fetchall() if r["embedded"])
                    self._insert(cur, documents.summary(
                        user_id, row["period"], row["period_start"], row["text"], row["model"]),
                        None, False, to_index)
        self._set_watermark(SUMMARY_WATERMARK, cycle_now)
        self._index.remove(removed)
        self._index.upsert(to_index)

    def _embed_pending(self) -> None:
        while True:
            with self._conn.cursor(row_factory=dict_row) as cur:
                cur.execute(
                    f"SELECT {_ROW_COLUMNS} FROM search_documents "
                    "WHERE embedding IS NULL AND indexed_at IS NULL LIMIT %s",
                    (self._batch_size,),
                )
                rows = cur.fetchall()
            if not rows:
                return
            vectors = self._embedder.embed([row["content"] for row in rows])
            documents_to_index = []
            with self._conn.transaction():
                with self._conn.cursor() as cur:
                    for row, vector in zip(rows, vectors):
                        cur.execute(
                            "UPDATE search_documents SET embedding = %s, indexed_at = NOW(), updated_at = NOW() "
                            "WHERE id = %s",
                            (Vector(vector), row["id"]),
                        )
                        documents_to_index.append(search.row_to_document(row, vector))
            self._index.upsert(documents_to_index)

    def _insert(self, cur, doc: dict, embedding, indexed_now: bool, to_index: list) -> None:
        params = dict(doc)
        params["embedding"] = Vector(_plain_embedding(embedding)) if embedding is not None else None
        params["indexed_at"] = self._now() if indexed_now else None
        cur.execute(_INSERT, params)
        row_id = cur.fetchone()["id"]
        if indexed_now and embedding is not None:
            to_index.append(search.row_to_document({**doc, "id": row_id}, _plain_embedding(embedding)))

    def _watermark(self, name: str) -> datetime:
        with self._conn.cursor(row_factory=dict_row) as cur:
            cur.execute("SELECT watermark FROM mcp_index_state WHERE name = %s", (name,))
            row = cur.fetchone()
        return row["watermark"] if row else EPOCH

    def _set_watermark(self, name: str, value: datetime) -> None:
        with self._conn.cursor() as cur:
            cur.execute(
                "INSERT INTO mcp_index_state (name, watermark) VALUES (%s, %s) "
                "ON CONFLICT (name) DO UPDATE SET watermark = EXCLUDED.watermark, updated_at = NOW()",
                (name, value),
            )

    def _now(self) -> datetime:
        with self._conn.cursor() as cur:
            cur.execute("SELECT NOW()")
            return cur.fetchone()[0]

    @staticmethod
    def _one(cur, statement: str, audio_id):
        cur.execute(statement, (audio_id,))
        return cur.fetchone()

    @staticmethod
    def _all(cur, statement: str, audio_id):
        cur.execute(statement, (audio_id,))
        return cur.fetchall()
