"""In-process TurboVec semantic index and the Haystack search wrapper.

One global index holds every account's embedded documents; each query passes
a meta.user_id filter that TurboVec resolves to an allowlist before scoring,
so a search can never return another account's rows.
"""

from datetime import datetime, timezone

try:
    from haystack import Document
    from haystack.document_stores.types import DuplicatePolicy
    from turbovec.haystack import TurboQuantDocumentStore
except ModuleNotFoundError:  # pragma: no cover - only without the ML extras
    Document = None
    DuplicatePolicy = None
    TurboQuantDocumentStore = None


def _iso(value: datetime | None) -> str | None:
    if value is None:
        return None
    if value.tzinfo is None:
        value = value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc).isoformat()


def row_to_document(row: dict, embedding) -> "Document":
    meta = {
        "user_id": str(row["user_id"]),
        "source_type": row["source_type"],
        "occurred_at": _iso(row["occurred_at"]),
        "recorded_at": _iso(row["recorded_at"]),
        "reminded_at": _iso(row["reminded_at"]),
        "audio_id": str(row["audio_id"]) if row["audio_id"] else None,
        "is_done": row["is_done"],
        "period": row["period"],
        "period_start": row["period_start"].isoformat() if row["period_start"] else None,
        "model": row["model"],
    }
    return Document(id=str(row["id"]), content=row["content"], embedding=embedding, meta=meta)


def build_filters(
    user_id: str,
    source_types: tuple[str, ...] | None = None,
    start: datetime | None = None,
    end: datetime | None = None,
) -> dict:
    conditions = [{"field": "meta.user_id", "operator": "==", "value": user_id}]
    if source_types:
        conditions.append({"field": "meta.source_type", "operator": "in", "value": list(source_types)})
    if start is not None:
        conditions.append({"field": "meta.occurred_at", "operator": ">=", "value": _iso(start)})
    if end is not None:
        conditions.append({"field": "meta.occurred_at", "operator": "<=", "value": _iso(end)})
    if len(conditions) == 1:
        return conditions[0]
    return {"operator": "AND", "conditions": conditions}


class DocumentIndex:
    def __init__(self, dim: int, bit_width: int) -> None:
        self._store = TurboQuantDocumentStore(
            dim=dim,
            bit_width=bit_width,
            embedding_similarity_function="cosine",
        )

    def rebuild(self, documents: list) -> None:
        self._store.delete_all_documents()
        if documents:
            self._store.write_documents(documents, policy=DuplicatePolicy.OVERWRITE)

    def upsert(self, documents: list) -> None:
        if documents:
            self._store.write_documents(documents, policy=DuplicatePolicy.OVERWRITE)

    def remove(self, ids: list[int]) -> None:
        if ids:
            self._store.delete_documents([str(i) for i in ids])

    def search(self, query_embedding: list[float], filters: dict, top_k: int) -> list:
        return self._store.embedding_retrieval(
            query_embedding=query_embedding,
            top_k=top_k,
            filters=filters,
            scale_score=True,
        )
