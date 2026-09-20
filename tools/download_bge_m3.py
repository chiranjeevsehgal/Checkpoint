"""Download bge-m3 into the local HF cache with plain-text progress.

Shared by the embedding-service and mcp-service images (single copy; both
Dockerfiles COPY it from tools/). docker build has no tty, so huggingface's
default progress bar renders nothing there and a 2.3 GB fetch looks like a
hung step. This prints one line per file plus a rolling progress line every
15 seconds.
"""

import fnmatch
import sys
import threading

from huggingface_hub import HfApi, hf_hub_download
from tqdm import tqdm

REPO_ID = "BAAI/bge-m3"
ALLOW_PATTERNS = [
    "*.json",
    "*.txt",
    "*.model",
    "*.bin",
    "*.safetensors",
    "1_Pooling/*",
    "2_Normalize/*",
]
REPORT_INTERVAL_SECONDS = 15

_progress: dict[str, tuple[int, int | None]] = {}
_current: dict[str, str | None] = {"file": None}


class _Recording(tqdm):
    def update(self, n=1):
        result = super().update(n)
        _progress[_current["file"] or str(self.desc)] = (self.n, self.total)
        return result

    def display(self, msg=None, pos=None):
        pass


def _reporter(stop: threading.Event) -> None:
    while not stop.wait(REPORT_INTERVAL_SECONDS):
        parts = []
        for name, (done, total) in _progress.items():
            if total:
                parts.append(f"{name}: {done / 2**30:.2f}/{total / 2**30:.2f} GiB ({100 * done / total:.0f}%)")
            else:
                parts.append(f"{name}: {done / 2**20:.0f} MiB")
        if parts:
            print(f"[model] downloading: {'; '.join(parts)}", flush=True)


def _wanted(filenames: list[str]) -> list[str]:
    return [f for f in filenames if any(fnmatch.fnmatch(f, p) for p in ALLOW_PATTERNS)]


def _fetch(filename: str) -> str:
    _current["file"] = filename
    try:
        return hf_hub_download(REPO_ID, filename, tqdm_class=_Recording)
    except TypeError:
        return hf_hub_download(REPO_ID, filename)


def main() -> int:
    api = HfApi()
    filenames = _wanted(api.list_repo_files(REPO_ID))
    if not filenames:
        print(f"[model] ERROR: no files in {REPO_ID} matched the allow patterns", flush=True)
        return 1
    print(f"[model] fetching {len(filenames)} files from {REPO_ID}", flush=True)

    stop = threading.Event()
    reporter = threading.Thread(target=_reporter, args=(stop,), daemon=True)
    reporter.start()
    try:
        for filename in filenames:
            print(f"[model] {filename}: start", flush=True)
            path = _fetch(filename)
            _progress.pop(filename, None)
            print(f"[model] {filename}: done -> {path}", flush=True)
    finally:
        stop.set()
    print("[model] all files cached; model is baked into the image", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
