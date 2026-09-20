import unittest
from datetime import date, datetime, timezone
from zoneinfo import ZoneInfo

from app import presentation

ZONE = ZoneInfo("Asia/Kolkata")


class FakeDocument:
    def __init__(self, score=None, **meta):
        self.content = "text"
        self.score = score
        self.meta = meta


class EnvelopeTest(unittest.TestCase):
    def test_adds_now_and_timezone(self):
        got = presentation.envelope({"count": 1}, ZONE)
        self.assertEqual(got["timezone"], "Asia/Kolkata")
        self.assertTrue(got["now"]["utc"].endswith("+00:00"))
        self.assertTrue(got["now"]["local"].endswith("+05:30"))

    def test_omits_timezone_when_unknown(self):
        self.assertNotIn("timezone", presentation.envelope({"count": 1}, None))

    def test_does_not_mutate_payload(self):
        payload = {"count": 1}
        presentation.envelope(payload, ZONE)
        self.assertEqual(payload, {"count": 1})


class ErrorTest(unittest.TestCase):
    def test_error_carries_code_and_now(self):
        got = presentation.error("invalid_query", "query must not be empty")
        self.assertEqual(got["error"], "query must not be empty")
        self.assertEqual(got["code"], "invalid_query")
        self.assertIn("now", got)
        self.assertNotIn("timezone", got)


class EmptyNoteTest(unittest.TestCase):
    def test_adds_message_only_when_empty(self):
        self.assertEqual(presentation.with_empty_note({"count": 0}, "nothing")["message"], "nothing")
        self.assertNotIn("message", presentation.with_empty_note({"count": 2}, "nothing"))


class FormatRowTest(unittest.TestCase):
    def row(self, **overrides):
        base = {
            "source_type": "todo",
            "content": "buy milk",
            "source_id": "7",
            "language": "eng",
            "chunk_index": -1,
            "important": None,
            "occurred_at": datetime(2026, 9, 20, 6, 30, tzinfo=timezone.utc),
            "recorded_at": None,
            "reminded_at": None,
            "is_done": False,
            "period": None,
            "period_start": None,
            "audio_id": None,
        }
        base.update(overrides)
        return base

    def test_exposes_provenance_fields(self):
        got = presentation.format_row(self.row(), ZONE)
        self.assertEqual(got["source_id"], "7")
        self.assertEqual(got["language"], "eng")
        self.assertEqual(got["chunk_index"], -1)
        self.assertFalse(got["is_done"])

    def test_renders_dual_times_and_period(self):
        got = presentation.format_row(self.row(period_start=date(2026, 9, 20)), ZONE)
        self.assertEqual(got["occurred_at"]["local"], "2026-09-20T12:00:00+05:30")
        self.assertEqual(got["period_start"], "2026-09-20")

    def test_absent_audio_id_is_none(self):
        self.assertIsNone(presentation.format_row(self.row(), ZONE)["audio_id"])


class FormatDocumentTest(unittest.TestCase):
    def test_reads_meta_provenance_and_score(self):
        document = FakeDocument(
            score=0.82314,
            source_type="transcript",
            source_id="audio-1",
            language="eng",
            chunk_index=2,
            occurred_at="2026-09-20T06:30:00+00:00",
        )
        got = presentation.format_document(document, ZONE)
        self.assertEqual(got["source_id"], "audio-1")
        self.assertEqual(got["chunk_index"], 2)
        self.assertEqual(got["score"], 0.8231)
        self.assertEqual(got["occurred_at"]["local"], "2026-09-20T12:00:00+05:30")


if __name__ == "__main__":
    unittest.main()
