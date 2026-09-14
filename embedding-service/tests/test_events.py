import json
import unittest

from app.events import EmbeddingJobEvent, InvalidEvent


def make_event(audio_id="11111111-1111-1111-1111-111111111111",
               user_id="22222222-2222-2222-2222-222222222222"):
    return {
        "schema_version": 2,
        "event_type": "EMBEDDING_REQUESTED",
        "event_id": "event-1",
        "data": {
            "audio_id": audio_id,
            "user_id": user_id,
            "text": "hello",
            "language": "en",
        },
    }


class TestEmbeddingJobEvent(unittest.TestCase):
    def test_accepts_valid_uuids(self):
        event = EmbeddingJobEvent.from_raw(json.dumps(make_event()).encode())
        self.assertEqual(event.audio_id, "11111111-1111-1111-1111-111111111111")
        self.assertEqual(event.user_id, "22222222-2222-2222-2222-222222222222")

    def test_rejects_malformed_audio_id(self):
        raw = json.dumps(make_event(audio_id="not-a-uuid")).encode()
        with self.assertRaises(InvalidEvent):
            EmbeddingJobEvent.from_raw(raw)

    def test_rejects_malformed_user_id(self):
        raw = json.dumps(make_event(user_id="")).encode()
        with self.assertRaises(InvalidEvent):
            EmbeddingJobEvent.from_raw(raw)


if __name__ == "__main__":
    unittest.main()
