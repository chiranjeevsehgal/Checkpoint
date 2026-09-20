import unittest
from datetime import date, datetime, timezone
from zoneinfo import ZoneInfo

from app import presentation

ZONE = ZoneInfo("Asia/Kolkata")


class FakeDocument:
    def __init__(self, score=None, content="text", **meta):
        self.content = content
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

    def test_as_of_included_when_zone_known(self):
        as_of = datetime(2026, 9, 20, 6, 30, tzinfo=timezone.utc)
        got = presentation.envelope({"count": 1}, ZONE, as_of)
        self.assertEqual(got["as_of"]["utc"], "2026-09-20T06:30:00+00:00")
        self.assertEqual(got["as_of"]["local"], "2026-09-20T12:00:00+05:30")

    def test_as_of_omitted_without_zone(self):
        as_of = datetime(2026, 9, 20, 6, 30, tzinfo=timezone.utc)
        self.assertNotIn("as_of", presentation.envelope({"count": 1}, None, as_of))


class WindowTextTest(unittest.TestCase):
    def test_no_cap_returns_everything(self):
        self.assertEqual(presentation.window_text("abcdef", 0, None), ("abcdef", False))

    def test_cap_truncates_and_flags(self):
        self.assertEqual(presentation.window_text("abcdef", 0, 3), ("abc", True))

    def test_offset_skips_prefix(self):
        self.assertEqual(presentation.window_text("abcdef", 4, None), ("ef", False))

    def test_negative_offset_clamps_to_zero(self):
        self.assertEqual(presentation.window_text("abcdef", -5, None), ("abcdef", False))


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


class ProjectTest(unittest.TestCase):
    def test_detailed_keeps_everything(self):
        row = {"source_id": "s", "text": "t"}
        self.assertEqual(presentation.project(row), row)

    def test_concise_drops_technical_ids(self):
        row = {"source_id": "s", "audio_id": "a", "language": "eng", "chunk_index": 2,
               "important": True, "score": 0.5, "text": "t", "is_done": False}
        self.assertEqual(presentation.project(row, concise=True), {"text": "t", "is_done": False})


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

    def test_truncates_long_text(self):
        got = presentation.format_row(self.row(content="x" * 20), ZONE, max_chars=5)
        self.assertEqual(got["text"], "xxxxx")
        self.assertTrue(got["truncated"])

    def test_short_text_is_not_truncated(self):
        self.assertFalse(presentation.format_row(self.row(), ZONE, max_chars=100)["truncated"])

    def test_concise_drops_ids_but_keeps_content(self):
        got = presentation.format_row(self.row(audio_id="a"), ZONE, concise=True)
        self.assertNotIn("source_id", got)
        self.assertNotIn("audio_id", got)
        self.assertNotIn("chunk_index", got)
        self.assertEqual(got["text"], "buy milk")
        self.assertFalse(got["is_done"])


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

    def test_truncates_long_text(self):
        document = FakeDocument(content="y" * 20, source_type="transcript")
        got = presentation.format_document(document, ZONE, max_chars=5)
        self.assertEqual(got["text"], "yyyyy")
        self.assertTrue(got["truncated"])

    def test_concise_drops_score_and_ids(self):
        document = FakeDocument(score=0.9, source_type="transcript", source_id="audio-1")
        got = presentation.format_document(document, ZONE, concise=True)
        self.assertNotIn("score", got)
        self.assertNotIn("source_id", got)


if __name__ == "__main__":
    unittest.main()
