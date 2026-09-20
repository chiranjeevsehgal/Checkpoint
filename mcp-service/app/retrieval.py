"""Retrieval entry point shared by the search tool and the eval harness.

Dense vector search today; the lexical channel and rank fusion land here so
callers never depend on the ranking strategy.
"""

from . import search as semantic


def retrieve(index, embedder, user_id, query, filters, limit, min_score=None):
    vector = embedder.embed([query])[0]
    results = semantic.scoped(index.search(vector, filters, limit), user_id)
    if min_score is not None:
        results = [d for d in results if d.score is not None and d.score >= min_score]
    return results
