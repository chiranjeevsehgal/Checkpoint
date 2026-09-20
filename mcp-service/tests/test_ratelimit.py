import time
import unittest

from app.ratelimit import RateLimiter


class RateLimiterTest(unittest.TestCase):
    def test_allows_burst_then_denies(self):
        limiter = RateLimiter(per_minute=60, burst=3)
        self.assertTrue(limiter.allow("a")[0])
        self.assertTrue(limiter.allow("a")[0])
        self.assertTrue(limiter.allow("a")[0])
        allowed, retry_after = limiter.allow("a")
        self.assertFalse(allowed)
        self.assertGreater(retry_after, 0)

    def test_keys_are_independent(self):
        limiter = RateLimiter(per_minute=60, burst=1)
        self.assertTrue(limiter.allow("a")[0])
        self.assertTrue(limiter.allow("b")[0])
        self.assertFalse(limiter.allow("a")[0])

    def test_tokens_refill(self):
        limiter = RateLimiter(per_minute=6000, burst=1)
        self.assertTrue(limiter.allow("a")[0])
        self.assertFalse(limiter.allow("a")[0])
        time.sleep(0.02)
        self.assertTrue(limiter.allow("a")[0])

    def test_rejects_bad_config(self):
        with self.assertRaises(ValueError):
            RateLimiter(per_minute=0, burst=1)
        with self.assertRaises(ValueError):
            RateLimiter(per_minute=1, burst=0)


if __name__ == "__main__":
    unittest.main()
