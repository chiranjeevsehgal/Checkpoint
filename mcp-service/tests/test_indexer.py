import os
import unittest
import uuid

from app.indexer import SOURCE_WATERMARK, Indexer

try:
    import psycopg
    from pgvector.psycopg import register_vector
except ModuleNotFoundError:  # pragma: no cover - dependencies absent
    psycopg = None
    register_vector = None

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


if __name__ == "__main__":
    unittest.main()
