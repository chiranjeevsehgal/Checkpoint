import os
import unittest
import uuid

from app.store import DocumentQuery, ReadStore

try:
    import psycopg
except ModuleNotFoundError:  # pragma: no cover - dependencies absent
    psycopg = None

WRITER_DSN = os.getenv("TEST_DATABASE_URL")
MCP_DSN = os.getenv("TEST_MCP_DATABASE_URL")


@unittest.skipUnless(
    WRITER_DSN and MCP_DSN and psycopg,
    "TEST_DATABASE_URL/TEST_MCP_DATABASE_URL/psycopg not available",
)
class TenantIsolationTest(unittest.TestCase):
    """ReadStore connects as checkpoint_mcp; RLS must hide other accounts."""

    def setUp(self):
        self.user_a = str(uuid.uuid4())
        self.user_b = str(uuid.uuid4())
        self.writer = psycopg.connect(WRITER_DSN, autocommit=True)
        with self.writer.cursor() as cur:
            for user_id in (self.user_a, self.user_b):
                cur.execute(
                    "INSERT INTO search_documents "
                    "(user_id, source_type, source_id, chunk_index, content, content_hash, occurred_at) "
                    "VALUES (%s::uuid, 'todo', %s, -1, %s, %s, NOW())",
                    (user_id, str(uuid.uuid4()), f"secret-{user_id}", "hash"),
                )
        self.store = ReadStore(MCP_DSN)
        self.store.connect()

    def tearDown(self):
        self.store.close()
        with self.writer.cursor() as cur:
            cur.execute(
                "DELETE FROM search_documents WHERE user_id = ANY(%s::uuid[])",
                ([self.user_a, self.user_b],),
            )
        self.writer.close()

    def test_user_a_cannot_read_user_b(self):
        rows = self.store.fetch(self.user_a, DocumentQuery(limit=10))
        contents = [row["content"] for row in rows]
        self.assertIn(f"secret-{self.user_a}", contents)
        self.assertNotIn(f"secret-{self.user_b}", contents)


if __name__ == "__main__":
    unittest.main()
