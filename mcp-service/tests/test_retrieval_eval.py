"""Retrieval quality harness: reports hit@k / MRR and asserts grounding.

The corpus pairs each expected document with many near-neighbour distractors
that share every token except the distinguishing one, so the metric can show a
dense miss. Quality thresholds are deliberately not asserted; the numbers are
recorded before and after the hybrid change. Run:

    TEST_DATABASE_URL=postgres://... python -m unittest tests.test_retrieval_eval -v
"""

import os
import unittest
import uuid
from datetime import datetime, timezone

from app import documents, retrieval, search

try:
    import psycopg
    from pgvector import Vector
    from pgvector.psycopg import register_vector
    from psycopg.rows import dict_row
    import sentence_transformers  # noqa: F401
    import turbovec  # noqa: F401
except ModuleNotFoundError:  # pragma: no cover - ML/DB deps absent
    psycopg = None

DSN = os.getenv("TEST_DATABASE_URL")
TOP_K = 5

PEOPLE = ("Priya", "Marco", "Lena", "Sam", "Aiko", "Tomas", "Nadia", "Omar")
CODENAMES = ("Helios", "Titan", "Nimbus", "Vertex", "Onyx", "Zephyr", "Atlas", "Orion")
PROFESSIONS = ("doctor", "dentist", "vet", "optician", "therapist")
DAYS = ("Monday", "Tuesday", "Wednesday", "Thursday", "Friday")
TIMES = ("morning", "afternoon", "evening", "night")


def _build_corpus():
    docs = [
        ("todo", "todo-halcyon", "Email Priya about the Project Halcyon kickoff"),
        ("todo", "todo-invoice", "Pay invoice 8842 before Friday"),
        ("transcript", "audio-1", "We reviewed the quarterly roadmap and delayed the launch"),
        ("reminder", "reminder-1", "Call the dentist on Tuesday"),
        ("insight", "insight-1", "The team works best in the morning"),
        ("summary", "daily:2026-09-19", "You spent the morning planning the product launch"),
    ]
    for i, codename in enumerate(CODENAMES):
        person = PEOPLE[i % len(PEOPLE)]
        docs.append(("todo", f"todo-project-{i}", f"Email {person} about the Project {codename} kickoff"))
    for number in range(1000, 1060):
        docs.append(("todo", f"todo-invoice-{number}", f"Pay invoice {number} before Friday"))
    for profession in PROFESSIONS:
        for day in DAYS:
            if (profession, day) == ("dentist", "Tuesday"):
                continue
            docs.append(("reminder", f"reminder-{profession}-{day.lower()}", f"Call the {profession} on {day}"))
    for time_of_day in TIMES:
        if time_of_day == "morning":
            continue
        docs.append(("insight", f"insight-team-{time_of_day}", f"The team works best in the {time_of_day}"))
    for i, topic in enumerate(("annual", "product", "engineering", "hiring", "platform")):
        docs.append(("transcript", f"audio-roadmap-{i}", f"We reviewed the {topic} roadmap and delayed the launch"))
    return tuple(docs)


CORPUS = _build_corpus()

QUERIES = (
    ("Project Halcyon kickoff", "todo-halcyon"),
    ("invoice 8842", "todo-invoice"),
    ("dentist on Tuesday", "reminder-1"),
    ("when does the team work best in the morning", "insight-1"),
    ("quarterly roadmap launch", "audio-1"),
)

_INSERT = """
    INSERT INTO search_documents
        (user_id, source_type, source_id, chunk_index, content, content_hash, occurred_at, embedding)
    VALUES
        (%(user_id)s::uuid, %(source_type)s, %(source_id)s, %(chunk_index)s,
         %(content)s, %(content_hash)s, %(occurred_at)s, %(embedding)s)
"""

_SELECT = """
    SELECT id, user_id, source_type, source_id, audio_id, chunk_index, content, language,
           occurred_at, recorded_at, reminded_at, is_done, important, period, period_start,
           model, embedding
    FROM search_documents WHERE user_id = %s::uuid
"""


@unittest.skipUnless(DSN and psycopg, "TEST_DATABASE_URL/ML deps not available")
class RetrievalEvalTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        from app.embedder import EMBEDDING_DIM, Embedder

        cls.user_id = str(uuid.uuid4())
        cls.embedder = Embedder("BAAI/bge-m3")
        cls.conn = psycopg.connect(DSN, autocommit=True)
        register_vector(cls.conn)

        rows = [
            {
                "user_id": cls.user_id,
                "source_type": source_type,
                "source_id": source_id,
                "chunk_index": documents.WHOLE_DOCUMENT,
                "content": content,
                "content_hash": documents.content_hash(content),
                "occurred_at": datetime.now(timezone.utc),
            }
            for source_type, source_id, content in CORPUS
        ]
        vectors = cls.embedder.embed([row["content"] for row in rows])

        with cls.conn.cursor(row_factory=dict_row) as cur:
            for row, vector in zip(rows, vectors):
                cur.execute(_INSERT, {**row, "embedding": Vector(vector)})
            cur.execute(_SELECT, (cls.user_id,))
            stored = cur.fetchall()

        cls.index = search.DocumentIndex(dim=EMBEDDING_DIM, bit_width=4)
        cls.index.rebuild([
            search.row_to_document(row, row["embedding"].to_list()) for row in stored
        ])
        cls.source_ids = {source_id for _, source_id, _ in CORPUS}

    @classmethod
    def tearDownClass(cls):
        with cls.conn.cursor() as cur:
            cur.execute("DELETE FROM search_documents WHERE user_id = %s::uuid", (cls.user_id,))
        cls.conn.close()

    def test_reports_metrics_and_stays_grounded(self):
        ranks = []
        for query, expected in QUERIES:
            results = retrieval.retrieve(
                self.index, self.embedder, self.user_id, query,
                search.build_filters(self.user_id), TOP_K)
            source_ids = [doc.meta["source_id"] for doc in results]
            self.assertTrue(set(source_ids) <= self.source_ids, f"ungrounded results for {query!r}")
            ranks.append(source_ids.index(expected) + 1 if expected in source_ids else 0)

        hits = sum(1 for rank in ranks if rank) / len(ranks)
        mrr = sum(1 / rank for rank in ranks if rank) / len(ranks)
        print(f"\n[retrieval-eval] corpus={len(CORPUS)} hit@{TOP_K}={hits:.2f} mrr={mrr:.2f} ranks={ranks}")


if __name__ == "__main__":
    unittest.main()
