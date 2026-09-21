"""Per-key token-bucket rate limiter (stdlib only).

Bounds how fast one account can call the expensive tools, so a leaked
access key cannot hammer the shared in-process index. Buckets live in
memory: the service is a documented single worker, so no cross-process
state is needed.
"""

import logging
import threading
import time

log = logging.getLogger(__name__)

_MAX_KEYS = 10000


class RateLimiter:
    def __init__(self, per_minute: int, burst: int) -> None:
        if per_minute <= 0 or burst <= 0:
            raise ValueError("rate and burst must be positive")
        self._interval = 60.0 / per_minute
        self._burst = burst
        self._buckets: dict[str, list] = {}
        self._lock = threading.Lock()

    def allow(self, key: str) -> tuple[bool, float]:
        """Take one token for key. Returns (allowed, retry_after_seconds)."""
        now = time.monotonic()
        with self._lock:
            tokens, updated = self._buckets.get(key, (float(self._burst), now))
            tokens = min(float(self._burst), tokens + (now - updated) / self._interval)
            if tokens >= 1.0:
                self._buckets[key] = [tokens - 1.0, now]
                return True, 0.0
            retry_after = (1.0 - tokens) * self._interval
            self._buckets[key] = [tokens, updated]
            if len(self._buckets) > _MAX_KEYS:
                self._sweep(now)
            return False, retry_after

    def _sweep(self, now: float) -> None:
        cutoff = now - self._interval * self._burst
        stale = [key for key, (_, updated) in self._buckets.items() if updated < cutoff]
        for key in stale:
            del self._buckets[key]
        if len(self._buckets) > _MAX_KEYS:
            log.warning("rate limiter table full, dropping oldest entries")
            for key in list(self._buckets)[: len(self._buckets) - _MAX_KEYS]:
                del self._buckets[key]
