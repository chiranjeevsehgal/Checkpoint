"""Retrieval entry point shared by the search tool and the eval harness.

Fuses dense vector search with a Postgres full-text (tsvector) lexical
channel via reciprocal rank fusion, so exact terms (names, numbers) that
embeddings blur still surface. The vector store filter plus the scoped()
post-filter keep every result inside the authenticated account; callers never
depend on the ranking strategy.
"""

import logging

from . import search as semantic

log = logging.getLogger(__name__)

OVERSAMPLE = 2
MAX_FETCH = 200
RRF_K = 60


def fuse(dense: list, lexical_rows: list, limit: int) -> list:
    """Reciprocal rank fusion over dense Documents and lexical row dicts."""
    by_id: dict = {}
    combined: dict = {}
    for rank, doc in enumerate(dense, 1):
        key = str(doc.id)
        by_id[key] = doc
        combined[key] = combined.get(key, 0.0) + 1.0 / (RRF_K + rank)
    for rank, row in enumerate(lexical_rows, 1):
        key = str(row["id"])
        if key not in by_id:
            by_id[key] = semantic.row_to_document(row, semantic.plain_embedding(row.get("embedding")))
        combined[key] = combined.get(key, 0.0) + 1.0 / (RRF_K + rank)
    ordered = sorted(combined, key=lambda key: combined[key], reverse=True)
    return [by_id[key] for key in ordered[:limit]]


def retrieve(index, embedder, user_id, query, filters, limit, min_score=None,
             store=None, hybrid_enabled=False, oversample=OVERSAMPLE):
    fetch = min(max(int(limit), 1) * max(int(oversample), 1), MAX_FETCH)
    vector = embedder.embed([query])[0]
    # Over-fetch: scoped() and min_score only shrink the list, so fetch extra
    # and slice to the requested limit at the end.
    dense = semantic.scoped(index.search(vector, filters, fetch), user_id)
    if min_score is not None:
        dense = [d for d in dense if d.score is not None and d.score >= min_score]
    lexical_rows: list = []
    if hybrid_enabled and store is not None:
        try:
            lexical_rows = store.lexical_search(user_id, query, fetch)
        except Exception as exc:  # noqa: BLE001 - lexical is best-effort; dense still serves
            log.warning("lexical search failed, serving dense only", extra={"error": str(exc)})
    if lexical_rows:
        return fuse(dense, lexical_rows, limit)
    return dense[:limit]
