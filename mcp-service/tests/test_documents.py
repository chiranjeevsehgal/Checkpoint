import unittest
from datetime import date, datetime, timezone

from app import documents


class DocumentsTest(unittest.TestCase):
    def test_todo_prefers_recorded_at(self):
        recorded = datetime(2026, 1, 1, tzinfo=timezone.utc)
        created = datetime(2026, 2, 1, tzinfo=timezone.utc)
        doc = documents.todo("u", "a", 1, "buy milk", recorded, created, False)
        self.assertEqual(doc["occurred_at"], recorded)
        self.assertFalse(doc["is_done"])
        self.assertEqual(doc["source_type"], documents.SOURCE_TODO)

    def test_todo_falls_back_to_created_at(self):
        created = datetime(2026, 2, 1, tzinfo=timezone.utc)
        doc = documents.todo("u", "a", 1, "x", None, created, False)
        self.assertEqual(doc["occurred_at"], created)

    def test_summary_source_id_and_occurred(self):
        doc = documents.summary("u", "daily", date(2026, 9, 20), "recap", "m")
        self.assertEqual(doc["source_id"], "daily:2026-09-20")
        self.assertEqual(doc["occurred_at"], datetime(2026, 9, 20, 0, 0, tzinfo=timezone.utc))
        self.assertEqual(doc["period_start"], date(2026, 9, 20))

    def test_transcript_full_is_whole_document(self):
        created = datetime(2026, 2, 1, tzinfo=timezone.utc)
        doc = documents.transcript_full("u", "a", "hello", "eng", None, created)
        self.assertEqual(doc["chunk_index"], documents.WHOLE_DOCUMENT)
        self.assertIsNone(doc["recorded_at"])

    def test_content_hash_changes_with_content(self):
        self.assertNotEqual(documents.content_hash("a"), documents.content_hash("b"))


if __name__ == "__main__":
    unittest.main()
