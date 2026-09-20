import unittest

from app.batching import collect_batch, parse_valid, split_vectors
from app.events import EmbeddingJobEvent, InvalidEvent


class FakeMessage:
    def __init__(self, value, error=None, partition=0, offset=0):
        self._value = value
        self._error = error
        self._partition = partition
        self._offset = offset

    def value(self):
        return self._value

    def error(self):
        return self._error

    def partition(self):
        return self._partition

    def offset(self):
        return self._offset


class FakeKafka:
    def __init__(self, messages):
        self._messages = list(messages)

    def poll(self, timeout):
        if not self._messages:
            return None
        return self._messages.pop(0)


VALID = (
    b'{"schema_version": 2, "event_id": "e1", "event_type": "EMBEDDING_REQUESTED", '
    b'"occurred_at": "2026-09-20T00:00:00.000Z", "data": {'
    b'"audio_id": "00000000-0000-0000-0000-000000000001", '
    b'"user_id": "00000000-0000-0000-0000-000000000002", '
    b'"text": "hello world", "language": "en"}}'
)


class CollectBatchTest(unittest.TestCase):
    def test_empty_poll_returns_empty(self):
        self.assertEqual(collect_batch(FakeKafka([]), 8), [])

    def test_drains_up_to_batch_max(self):
        messages = [FakeMessage(VALID) for _ in range(5)]
        batch = collect_batch(FakeKafka(messages), 3)
        self.assertEqual(len(batch), 3)

    def test_stops_when_queue_empties(self):
        messages = [FakeMessage(VALID) for _ in range(2)]
        batch = collect_batch(FakeKafka(messages), 8)
        self.assertEqual(len(batch), 2)


class SplitVectorsTest(unittest.TestCase):
    def test_splits_back_per_event(self):
        grouped = split_vectors([2, 1], [[1.0], [2.0], [3.0]])
        self.assertEqual(grouped, [[[1.0], [2.0]], [[3.0]]])

    def test_mismatched_count_is_invalid(self):
        with self.assertRaises(InvalidEvent):
            split_vectors([2], [[1.0]])


class ParseValidTest(unittest.TestCase):
    def test_splits_valid_and_invalid(self):
        valid, invalid = parse_valid([FakeMessage(VALID), FakeMessage(b"nope")])
        self.assertEqual(len(valid), 1)
        self.assertIsInstance(valid[0][1], EmbeddingJobEvent)
        self.assertEqual(len(invalid), 1)


if __name__ == "__main__":
    unittest.main()
