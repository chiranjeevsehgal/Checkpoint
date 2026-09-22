import os
import unittest
import uuid
from datetime import datetime, timezone

from app.indexer import SOURCE_WATERMARK, Indexer

try:
    import psycopg
    from pgvector.psycopg import register_vector
    from psycopg.rows import dict_row
except ModuleNotFoundError:  # pragma: no cover - dependencies absent
    psycopg = None
    register_vector = None
    dict_row = None

DSN = os.getenv("TEST_DATABASE_URL")
DIM = 1024


class FakeEmbedder:
    def embed(self, texts):
        return [[0.0] * DIM for _ in texts]


class FakeIndex:
    def __init__(self):
        self.upserted = []
        self.removed = []

    def upsert(self, documents):
        self.upserted.extend(documents)

    def remove(self, ids):
        self.removed.extend(ids)

    def rebuild(self, documents):
        pass


class DocumentForTest(unittest.TestCase):
    """The reconcile row mapper must reuse the documents.* builders."""

    def setUp(self):
        self.indexer = Indexer("dsn", None, None)

    def test_todo_uses_recorded_at_and_is_done(self):
        recorded = datetime(2026, 1, 1, tzinfo=timezone.utc)
        created = datetime(2026, 2, 1, tzinfo=timezone.utc)
        doc = self.indexer._document_for({
            "source_type": "todo", "user_id": "u", "audio_id": "a", "source_id": "7",
            "content": "buy milk", "recorded_at": recorded, "created_at": created,
            "is_done": True, "remind_at": None, "important": None,
        })
        self.assertEqual(doc["occurred_at"], recorded)
        self.assertTrue(doc["is_done"])

    def test_reminder_uses_created_at_and_remind_at(self):
        created = datetime(2026, 2, 1, tzinfo=timezone.utc)
        remind = datetime(2026, 3, 1, tzinfo=timezone.utc)
        doc = self.indexer._document_for({
            "source_type": "reminder", "user_id": "u", "audio_id": "a", "source_id": "9",
            "content": "call bank", "recorded_at": None, "created_at": created,
            "is_done": None, "remind_at": remind, "important": True,
        })
        self.assertEqual(doc["occurred_at"], created)
        self.assertEqual(doc["reminded_at"], remind)
        self.assertTrue(doc["important"])

    def test_insight_uses_created_at(self):
        created = datetime(2026, 2, 1, tzinfo=timezone.utc)
        doc = self.indexer._document_for({
            "source_type": "insight", "user_id": "u", "audio_id": "a", "source_id": "3",
            "content": "an idea", "recorded_at": None, "created_at": created,
            "is_done": None, "remind_at": None, "important": None,
        })
        self.assertEqual(doc["occurred_at"], created)
        self.assertEqual(doc["content"], "an idea")


@unittest.skipUnless(DSN and psycopg, "TEST_DATABASE_URL/psycopg not available")
class IndexerEmbeddingsTest(unittest.TestCase):
    """Chunks that arrive after the transcript must still be indexed."""

    def setUp(self):
        self.user_id = str(uuid.uuid4())
        self.audio_id = str(uuid.uuid4())
        self.conn = psycopg.connect(DSN, autocommit=True)
        register_vector(self.conn)
        self.index = FakeIndex()
        self.indexer = Indexer(DSN, FakeEmbedder(), self.index)
        self.indexer.connect()
        self.original_watermark = self.indexer._watermark(SOURCE_WATERMARK)
        self.indexer._set_watermark(SOURCE_WATERMARK, self.indexer._now())

    def tearDown(self):
        with self.conn.cursor() as cur:
            cur.execute("DELETE FROM search_documents WHERE user_id = %s::uuid", (self.user_id,))
            cur.execute("DELETE FROM embeddings WHERE user_id = %s::uuid", (self.user_id,))
            cur.execute("DELETE FROM transcripts WHERE user_id = %s::uuid", (self.user_id,))
        self.indexer._set_watermark(SOURCE_WATERMARK, self.original_watermark)
        self.indexer.close()
        self.conn.close()

    def _insert_transcript(self):
        with self.conn.cursor() as cur:
            cur.execute(
                "INSERT INTO transcripts (audio_id, user_id, text, created_at) "
                "VALUES (%s::uuid, %s::uuid, %s, NOW())",
                (self.audio_id, self.user_id, "hello world"),
            )

    def _insert_embedding(self):
        with self.conn.cursor() as cur:
            cur.execute(
                "INSERT INTO embeddings "
                "(user_id, audio_id, chunk_index, chunk_text, model, embedding) "
                "VALUES (%s::uuid, %s::uuid, 0, %s, 'test', %s)",
                (self.user_id, self.audio_id, "hello", [0.0] * DIM),
            )

    def _chunk_count(self):
        with self.conn.cursor() as cur:
            cur.execute(
                "SELECT count(*) FROM search_documents "
                "WHERE audio_id = %s::uuid AND chunk_index >= 0",
                (self.audio_id,),
            )
            return cur.fetchone()[0]

    def test_embeddings_arriving_later_are_indexed(self):
        self._insert_transcript()
        self.indexer._sync_sources(self.indexer._now())
        self.assertEqual(self._chunk_count(), 0)

        self._insert_embedding()
        self.indexer._sync_sources(self.indexer._now())

        self.assertEqual(self._chunk_count(), 1)
        chunk_types = [doc.meta["source_type"] for doc in self.index.upserted]
        self.assertIn("transcript", chunk_types)


@unittest.skipUnless(DSN and psycopg, "TEST_DATABASE_URL/psycopg not available")
class IndexerReconcileTest(unittest.TestCase):
    """In-place edits and deletes of structured items must be reconciled."""

    def setUp(self):
        self.user_id = str(uuid.uuid4())
        self.audio_id = str(uuid.uuid4())
        self.conn = psycopg.connect(DSN, autocommit=True)
        register_vector(self.conn)
        self.index = FakeIndex()
        self.indexer = Indexer(DSN, FakeEmbedder(), self.index)
        self.indexer.connect()
        self.original_watermark = self.indexer._watermark(SOURCE_WATERMARK)
        self.indexer._set_watermark(SOURCE_WATERMARK, self.indexer._now())

    def tearDown(self):
        with self.conn.cursor() as cur:
            cur.execute("DELETE FROM search_documents WHERE user_id = %s::uuid", (self.user_id,))
            cur.execute("DELETE FROM todos WHERE user_id = %s::uuid", (self.user_id,))
            cur.execute("DELETE FROM reminders WHERE user_id = %s", (self.user_id,))
            cur.execute("DELETE FROM insights WHERE user_id = %s", (self.user_id,))
        self.indexer._set_watermark(SOURCE_WATERMARK, self.original_watermark)
        self.indexer.close()
        self.conn.close()

    def _insert_todo(self, text="buy milk"):
        with self.conn.cursor() as cur:
            cur.execute(
                "INSERT INTO todos (user_id, audio_id, text, model) "
                "VALUES (%s::uuid, %s::uuid, %s, 'test') RETURNING id",
                (self.user_id, self.audio_id, text),
            )
            return cur.fetchone()[0]

    def _insert_reminder(self, text, remind_at=None):
        with self.conn.cursor() as cur:
            cur.execute(
                "INSERT INTO reminders (user_id, audio_id, text, model, remind_at) "
                "VALUES (%s, %s, %s, 'test', %s) RETURNING id",
                (self.user_id, self.audio_id, text, remind_at),
            )
            return cur.fetchone()[0]

    def _index_sources(self):
        self.indexer._sync_sources(self.indexer._now())
        self.indexer._embed_pending()

    def _doc_row(self, source_type, source_id):
        with self.conn.cursor(row_factory=dict_row) as cur:
            cur.execute(
                "SELECT id, content, is_done, reminded_at, (embedding IS NOT NULL) AS embedded "
                "FROM search_documents WHERE user_id = %s::uuid AND source_type = %s "
                "AND source_id = %s AND chunk_index = -1",
                (self.user_id, source_type, str(source_id)),
            )
            return cur.fetchone()

    def test_ticked_todo_is_reconciled(self):
        todo_id = self._insert_todo()
        self._index_sources()
        self.assertFalse(self._doc_row("todo", todo_id)["is_done"])

        with self.conn.cursor() as cur:
            cur.execute("UPDATE todos SET is_done = TRUE WHERE id = %s", (todo_id,))

        self.indexer._reconcile_structured()
        self.assertTrue(self._doc_row("todo", todo_id)["is_done"])
        self.assertFalse(self._doc_row("todo", todo_id)["embedded"])

        self.indexer._embed_pending()
        self.assertTrue(self._doc_row("todo", todo_id)["embedded"])
        todo_docs = [d for d in self.index.upserted if d.meta["source_id"] == str(todo_id)]
        self.assertTrue(todo_docs and todo_docs[-1].meta["is_done"])

    def test_deleted_todo_is_removed(self):
        todo_id = self._insert_todo()
        self._index_sources()
        doc_id = self._doc_row("todo", todo_id)["id"]

        with self.conn.cursor() as cur:
            cur.execute("DELETE FROM todos WHERE id = %s", (todo_id,))

        self.indexer._reconcile_structured()

        self.assertIsNone(self._doc_row("todo", todo_id))
        self.assertIn(doc_id, self.index.removed)

    def test_edited_reminder_is_reconciled(self):
        remind = datetime(2026, 3, 1, tzinfo=timezone.utc)
        reminder_id = self._insert_reminder("call bank", remind)
        self._index_sources()
        self.assertEqual(self._doc_row("reminder", reminder_id)["content"], "call bank")

        moved = datetime(2026, 4, 1, tzinfo=timezone.utc)
        with self.conn.cursor() as cur:
            cur.execute(
                "UPDATE reminders SET text = %s, remind_at = %s WHERE id = %s",
                ("call mom", moved, reminder_id),
            )

        self.indexer._reconcile_structured()

        row = self._doc_row("reminder", reminder_id)
        self.assertEqual(row["content"], "call mom")
        self.assertEqual(row["reminded_at"], moved)
        self.assertFalse(row["embedded"])


if __name__ == "__main__":
    unittest.main()
