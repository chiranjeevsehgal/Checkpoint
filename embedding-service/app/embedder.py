from sentence_transformers import SentenceTransformer

EMBEDDING_DIM = 1024

class Embedder:
    """Embeddings are L2-normalized on encode so
    cosine similarity in pgvector (`<=>`) is the right distance operator.
    """

    def __init__(self, model_name: str) -> None:
        self._model_name = model_name
        self._model = SentenceTransformer(model_name)
        self._model.eval()

    @property
    def model_name(self) -> str:
        return self._model_name

    @property
    def tokenizer(self):
        return self._model.tokenizer

    def embed(self, texts: list[str]) -> list[list[float]]:
        vectors = self._model.encode(
            texts,
            normalize_embeddings=True,
            show_progress_bar=False,
        )
        return vectors.tolist()
