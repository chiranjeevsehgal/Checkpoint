import psycopg

_UPSERT = """
    INSERT INTO embeddings (user_id, audio_id, chunk_index, chunk_text, language, model, embedding)
    VALUES (%(user_id)s, %(audio_id)s, %(chunk_index)s, %(chunk_text)s, %(language)s, %(model)s, %(embedding)s)
    ON CONFLICT (user_id, audio_id, chunk_index) DO UPDATE SET
        chunk_text = EXCLUDED.chunk_text,
        language = EXCLUDED.language,
        model = EXCLUDED.model,
        embedding = EXCLUDED.embedding
"""

_DELETE_STALE = """
    DELETE FROM embeddings
    WHERE user_id = %(user_id)s AND audio_id = %(audio_id)s AND chunk_index >= %(chunk_count)s
"""


class Store:
    """pgvector writer. The upsert makes Kafka redeliveries idempotent
    (no commit on crash => same event processed twice is a no-op), and the
    stale-chunk delete keeps the row set consistent with the current chunking
    if the event is ever re-sent with different chunk settings.
    """

    def __init__(self, dsn: str) -> None:
        self._dsn = dsn
        self._conn: psycopg.Connection | None = None

    def connect(self) -> None:
        self._conn = psycopg.connect(self._dsn)

    def close(self) -> None:
        if self._conn is not None:
            self._conn.close()
            self._conn = None

    def reset(self) -> None:
        self.close()
        self.connect()

    def is_user_deleting(self, user_id: str) -> bool:
        """True when a deletion tombstone exists. A missing table means
        downstream runs against a separate database; nothing to gate on.

        The read runs in its own committed transaction: psycopg3 opens an
        implicit transaction on the first statement, and leaving one open would
        turn save()'s transaction into a savepoint that never commits."""
        if self._conn is None:
            self.connect()
        assert self._conn is not None
        try:
            with self._conn.transaction():
                with self._conn.cursor() as cur:
                    cur.execute(
                        "SELECT EXISTS (SELECT 1 FROM account_deletions WHERE user_id = %s::uuid)",
                        (user_id,),
                    )
                    row = cur.fetchone()
            return bool(row and row[0])
        except psycopg.errors.UndefinedTable:
            self._conn.rollback()
            return False

    def save(
        self,
        user_id: str,
        audio_id: str,
        language: str,
        model: str,
        chunks: list[str],
        vectors: list[list[float]],
    ) -> None:
        if self._conn is None:
            self.connect()
        assert self._conn is not None
        with self._conn.transaction():
            with self._conn.cursor() as cur:
                rows = [
                    {
                        "user_id": user_id,
                        "audio_id": audio_id,
                        "chunk_index": i,
                        "chunk_text": chunk,
                        "language": language,
                        "model": model,
                        "embedding": vector,
                    }
                    for i, (chunk, vector) in enumerate(zip(chunks, vectors))
                ]
                if rows:
                    cur.executemany(_UPSERT, rows)
                cur.execute(
                    _DELETE_STALE,
                    {"user_id": user_id, "audio_id": audio_id, "chunk_count": len(chunks)},
                )
