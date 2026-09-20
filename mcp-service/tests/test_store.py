import os
import unittest

from app.store import DocumentQuery, ReadStore

try:
    import psycopg
except ModuleNotFoundError:  # pragma: no cover - dependencies absent
    psycopg = None

DSN = os.getenv("TEST_DATABASE_URL")


class DocumentQueryTest(unittest.TestCase):
    def test_defaults(self):
        query = DocumentQuery()
        self.assertIsNone(query.source_types)
        self.assertEqual(query.limit, 50)
        self.assertEqual(query.order, "occurred_at DESC")
        self.assertEqual(query.offset, 0)

    def test_fields_are_settable(self):
        query = DocumentQuery(source_types=("todo",), is_done=False, limit=5, offset=10)
        self.assertEqual(query.source_types, ("todo",))
        self.assertFalse(query.is_done)
        self.assertEqual(query.limit, 5)
        self.assertEqual(query.offset, 10)


@unittest.skipUnless(DSN and psycopg and ReadStore, "TEST_DATABASE_URL/psycopg not available")
class ReadStoreIntegrationTest(unittest.TestCase):
    def setUp(self):
        self.store = ReadStore(DSN)
        self.store.connect()

    def tearDown(self):
        self.store.close()

    def test_unknown_user_sees_no_rows(self):
        rows = self.store.fetch("00000000-0000-0000-0000-000000000000", DocumentQuery(limit=5))
        self.assertEqual(rows, [])

    def test_unknown_user_has_no_timezone(self):
        self.assertIsNone(self.store.get_timezone("00000000-0000-0000-0000-000000000000"))


if __name__ == "__main__":
    unittest.main()
