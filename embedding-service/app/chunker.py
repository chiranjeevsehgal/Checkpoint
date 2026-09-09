def token_windows(total: int, max_tokens: int, overlap_tokens: int) -> list[tuple[int, int]]:
    """Pure [start, end) ranges over `total` tokens: windows of at most
    max_tokens, each advancing by max_tokens - overlap_tokens. Shared with
    nothing — testable without a real tokenizer.
    """
    if max_tokens <= 0:
        raise ValueError("max_tokens must be positive")
    if overlap_tokens < 0 or overlap_tokens >= max_tokens:
        raise ValueError("overlap_tokens must be in [0, max_tokens)")
    if total <= 0:
        return []
    if total <= max_tokens:
        return [(0, total)]

    step = max_tokens - overlap_tokens
    windows: list[tuple[int, int]] = []
    start = 0
    while True:
        end = min(start + max_tokens, total)
        windows.append((start, end))
        if end >= total:
            return windows
        start += step


class TokenChunker:
    """Splits text into <=max_tokens chunks measured with the model's own
    tokenizer, overlapping by overlap_tokens so content cut near a seam
    stays retrievable from both sides.

    Token-exact (not a chars-per-token estimate): the same units the model
    encodes, so a chunk can never silently exceed the window, and a subword
    tokenizer means chunks never split words mid-token either.
    """

    def __init__(self, tokenizer, max_tokens: int, overlap_tokens: int) -> None:
        # Validate once here so config errors surface at startup, not mid-loop.
        token_windows(1, max_tokens, overlap_tokens)
        self._tokenizer = tokenizer
        self._max_tokens = max_tokens
        self._overlap_tokens = overlap_tokens

    def chunk(self, text: str) -> list[str]:
        text = text.strip()
        if not text:
            return []
        ids = self._tokenizer.encode(text, add_special_tokens=False)
        chunks = []
        for start, end in token_windows(len(ids), self._max_tokens, self._overlap_tokens):
            chunk = self._tokenizer.decode(ids[start:end]).strip()
            if chunk:
                chunks.append(chunk)
        return chunks
