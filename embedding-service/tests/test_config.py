import os
import unittest
from unittest.mock import patch

from app.config import load


class RetryBackoffsTest(unittest.TestCase):
    def _load(self, env):
        base = {"DATABASE_URL": "postgres://localhost/db"}
        with patch.dict(os.environ, {**base, **env}, clear=True):
            return load()

    def test_defaults(self):
        cfg = self._load({})
        self.assertEqual(cfg.retry_backoffs, (1, 5, 15, 60))
        self.assertEqual(cfg.batch_max, 8)
        self.assertEqual(cfg.metrics_port, 9085)

    def test_env_override(self):
        cfg = self._load({"EMBEDDING_RETRY_BACKOFFS": "2,10"})
        self.assertEqual(cfg.retry_backoffs, (2, 10))

    def test_rejects_garbage(self):
        with self.assertRaises(ValueError):
            self._load({"EMBEDDING_RETRY_BACKOFFS": "soon, later"})

    def test_rejects_empty(self):
        with self.assertRaises(ValueError):
            self._load({"EMBEDDING_RETRY_BACKOFFS": " , "})


if __name__ == "__main__":
    unittest.main()
