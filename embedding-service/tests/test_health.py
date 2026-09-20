import json
import time
import unittest
import urllib.request

from app.health import HealthServer


class HealthServerTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = HealthServer("127.0.0.1", 18085)
        cls.server.start()
        deadline = time.time() + 3
        while time.time() < deadline:
            try:
                urllib.request.urlopen("http://127.0.0.1:18085/health/live", timeout=1)
                break
            except OSError:
                time.sleep(0.05)
        else:
            raise AssertionError("health server never came up")

    @classmethod
    def tearDownClass(cls):
        cls.server.stop()

    def _get(self, path):
        with urllib.request.urlopen(f"http://127.0.0.1:18085{path}", timeout=2) as resp:
            return resp.status, resp.read().decode()

    def test_live(self):
        code, body = self._get("/health/live")
        self.assertEqual(code, 200)
        self.assertEqual(json.loads(body), {"status": "ok"})

    def test_ready_after_set_ready(self):
        self.server.set_ready()
        code, _ = self._get("/health/ready")
        self.assertEqual(code, 200)

    def test_metrics_exposes_counters(self):
        self.server.inc("test_events_total")
        self.server.inc("test_events_total")
        code, body = self._get("/metrics")
        self.assertEqual(code, 200)
        self.assertIn("test_events_total 2", body)

    def test_unknown_path_404(self):
        try:
            self._get("/nope")
            self.fail("expected HTTPError")
        except Exception as exc:
            self.assertIn("404", str(exc))


if __name__ == "__main__":
    unittest.main()
