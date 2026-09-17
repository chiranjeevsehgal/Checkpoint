import os
import unittest
import uuid

try:
    import psycopg

    from app.store import Store
except ImportError:  # psycopg is only present in the worker image / CI
    psycopg = None
    Store = None

DSN = os.getenv("TEST_DATABASE_URL")


@unittest.skipUnless(DSN and psycopg and Store, "TEST_DATABASE_URL/psycopg not available")
class StoreCommitTest(unittest.TestCase):
    def setUp(self):
        self.user_id = str(uuid.uuid4())
        self.audio_id = str(uuid.uuid4())
        self.store = Store(DSN)
        self.store.connect()

    def tearDown(self):
        with psycopg.connect(DSN) as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "DELETE FROM embeddings WHERE user_id = %s AND audio_id = %s",
                    (self.user_id, self.audio_id),
                )
        self.store.close()

    def _count_from_new_connection(self) -> int:
        with psycopg.connect(DSN) as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "SELECT count(*) FROM embeddings WHERE user_id = %s AND audio_id = %s",
                    (self.user_id, self.audio_id),
                )
                return cur.fetchone()[0]

    def test_save_commits_after_tombstone_check(self):
        # is_user_deleting must not leave an implicit transaction open: save()
        # would then run as a savepoint that never commits.
        self.assertFalse(self.store.is_user_deleting(self.user_id))
        self.store.save(
            user_id=self.user_id,
            audio_id=self.audio_id,
            language="en",
            model="test",
            chunks=["hello"],
            vectors=[[0.1] * 1024],
        )
        self.assertEqual(self._count_from_new_connection(), 1)

        # Durable across a reconnect, proving it was committed, not local.
        self.store.reset()
        self.assertEqual(self._count_from_new_connection(), 1)


if __name__ == "__main__":
    unittest.main()
