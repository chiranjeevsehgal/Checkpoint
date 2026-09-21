import json
import unittest

from app.retry import build_retry_event


class BuildRetryEventTest(unittest.TestCase):
    def test_envelope_and_original_payload(self):
        original = b'{"schema_version":2,"event_id":"e1","data":{"user_id":"u1"}}'
        event = build_retry_event(
            "embedding-service", "embedding.jobs.v1", "process", "PROCESSING_ERROR", "boom", original
        )

        self.assertEqual(event["schema_version"], 2)
        self.assertEqual(event["event_type"], "RETRY_REQUESTED")
        self.assertTrue(event["event_id"])
        self.assertEqual(event["data"]["source_service"], "embedding-service")
        self.assertEqual(event["data"]["source_topic"], "embedding.jobs.v1")
        self.assertEqual(event["data"]["stage"], "process")
        self.assertEqual(event["data"]["error_code"], "PROCESSING_ERROR")
        self.assertEqual(event["data"]["error_message"], "boom")
        self.assertEqual(event["data"]["original_event"]["event_id"], "e1")

    def test_unparseable_payload_preserved_as_text(self):
        event = build_retry_event(
            "embedding-service", "embedding.jobs.v1", "process", "PROCESSING_ERROR", "boom", b"not-json"
        )
        self.assertEqual(event["data"]["original_event"], "not-json")


if __name__ == "__main__":
    unittest.main()
