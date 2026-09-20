import unittest
from unittest.mock import patch

from app import retrieval
from app.search import scoped


class FakeDocument:
    def __init__(self, doc_id, user_id, score=None):
        self.id = doc_id
        self.meta = {"user_id": user_id}
        self.score = score


class FakeIndex:
    def __init__(self, documents):
        self.documents = documents
        self.requested_top_k = []

    def search(self, vector, filters, top_k):
        self.requested_top_k.append(top_k)
        return list(self.documents)


class FakeEmbedder:
    def embed(self, texts):
        return [[0.0]] * len(texts)


def owned(doc_id, score=None):
    return FakeDocument(doc_id, "user-1", score)


def foreign(doc_id):
    return FakeDocument(doc_id, "user-2")


def lexical_row(doc_id):
    return {
        "id": doc_id,
        "user_id": "user-1",
        "source_type": "todo",
        "source_id": f"src-{doc_id}",
        "audio_id": None,
        "chunk_index": -1,
        "content": f"content {doc_id}",
        "language": "en",
        "occurred_at": None,
        "recorded_at": None,
        "reminded_at": None,
        "is_done": False,
        "important": False,
        "period": None,
        "period_start": None,
        "model": "test",
        "embedding": None,
    }


class OverFetchTest(unittest.TestCase):
    def test_over_fetches_before_scoped_filter(self):
        docs = [owned("a"), foreign("x"), owned("b"), foreign("y"), foreign("z"),
                owned("c"), owned("d"), owned("e"), owned("f"), owned("g")]
        index = FakeIndex(docs)
        results = retrieval.retrieve(index, FakeEmbedder(), "user-1", "q", {}, 5)
        self.assertEqual(index.requested_top_k, [10])
        self.assertEqual(len(results), 5)
        self.assertTrue(all(d.meta["user_id"] == "user-1" for d in results))

    def test_fetch_is_capped(self):
        index = FakeIndex([])
        retrieval.retrieve(index, FakeEmbedder(), "user-1", "q", {}, 200)
        self.assertEqual(index.requested_top_k, [retrieval.MAX_FETCH])


class MinScoreTest(unittest.TestCase):
    def test_min_score_applies_to_dense_before_fusion(self):
        docs = [owned("a", 0.9), owned("b", 0.1)]
        index = FakeIndex(docs)
        results = retrieval.retrieve(index, FakeEmbedder(), "user-1", "q", {}, 10, min_score=0.5)
        self.assertEqual([d.id for d in results], ["a"])


class FuseTest(unittest.TestCase):
    def test_shared_hit_ranks_first(self):
        dense = [owned("a"), owned("b")]
        rows = [lexical_row("b"), lexical_row("c")]
        with patch("app.search.row_to_document",
                   side_effect=lambda row, emb: FakeDocument(str(row["id"]), "user-1")):
            fused = retrieval.fuse(dense, rows, 10)
        self.assertEqual([d.id for d in fused], ["b", "a", "c"])

    def test_limit_is_respected(self):
        dense = [owned("a"), owned("b")]
        rows = [lexical_row("c")]
        with patch("app.search.row_to_document",
                   side_effect=lambda row, emb: FakeDocument(str(row["id"]), "user-1")):
            fused = retrieval.fuse(dense, rows, 2)
        self.assertEqual(len(fused), 2)


class HybridFallbackTest(unittest.TestCase):
    def test_lexical_failure_serves_dense_only(self):
        index = FakeIndex([owned("a")])

        class BrokenStore:
            def lexical_search(self, user_id, query, limit):
                raise RuntimeError("tsvector down")

        results = retrieval.retrieve(
            index, FakeEmbedder(), "user-1", "q", {}, 10,
            store=BrokenStore(), hybrid_enabled=True)
        self.assertEqual([d.id for d in results], ["a"])

    def test_disabled_hybrid_never_touches_store(self):
        index = FakeIndex([owned("a")])

        class ExplodingStore:
            def lexical_search(self, user_id, query, limit):
                raise AssertionError("must not be called")

        results = retrieval.retrieve(
            index, FakeEmbedder(), "user-1", "q", {}, 10,
            store=ExplodingStore(), hybrid_enabled=False)
        self.assertEqual([d.id for d in results], ["a"])

    def test_scoped_still_applies(self):
        self.assertEqual(scoped([foreign("x")], "user-1"), [])


if __name__ == "__main__":
    unittest.main()
