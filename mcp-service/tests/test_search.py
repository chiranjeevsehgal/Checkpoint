import unittest

from app.search import scoped


class FakeDocument:
    def __init__(self, user_id):
        self.meta = {"user_id": user_id}


class ScopedTest(unittest.TestCase):
    def test_keeps_only_the_requested_user(self):
        results = [FakeDocument("a"), FakeDocument("b"), FakeDocument("a")]
        self.assertEqual(scoped(results, "a"), [results[0], results[2]])

    def test_drops_documents_without_an_owner(self):
        self.assertEqual(scoped([FakeDocument(None)], "a"), [])

    def test_empty_results_stay_empty(self):
        self.assertEqual(scoped([], "a"), [])


if __name__ == "__main__":
    unittest.main()
