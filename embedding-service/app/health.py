"""Loopback health endpoints for the embedding worker.

Serves GET /health/live, GET /health/ready and GET /metrics (Prometheus
counter exposition, same text format as the Go workers) on a background
thread so Kafka polling never blocks on scrapes.
"""

import logging
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

log = logging.getLogger("embedding")


class HealthServer:
    def __init__(self, addr: str, port: int) -> None:
        self._addr = addr
        self._port = port
        self._ready = False
        self._counts: dict[str, int] = {}
        self._lock = threading.Lock()
        self._server: ThreadingHTTPServer | None = None

    def set_ready(self) -> None:
        self._ready = True

    def inc(self, name: str) -> None:
        with self._lock:
            self._counts[name] = self._counts.get(name, 0) + 1

    def start(self) -> None:
        server = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args) -> None:  # keep stdout for JSON logs
                pass

            def _send(self, code: int, body: bytes, content_type: str) -> None:
                self.send_response(code)
                self.send_header("Content-Type", content_type)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def do_GET(self) -> None:
                if self.path == "/health/live":
                    self._send(200, b'{"status":"ok"}', "application/json")
                elif self.path == "/health/ready":
                    if server._ready:
                        self._send(200, b'{"status":"ok"}', "application/json")
                    else:
                        self._send(503, b'{"status":"starting"}', "application/json")
                elif self.path == "/metrics":
                    with server._lock:
                        lines = [f"# TYPE {name} counter\n{name} {count}"
                                 for name, count in sorted(server._counts.items())]
                    self._send(200, ("\n".join(lines) + "\n").encode(),
                               "text/plain; version=0.0.4")
                else:
                    self._send(404, b"not found", "text/plain")

        try:
            self._server = ThreadingHTTPServer((self._addr, self._port), Handler)
        except OSError as exc:
            log.warning("health server failed; continuing without metrics",
                        extra={"error": str(exc)})
            return
        thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        thread.start()

    def stop(self) -> None:
        if self._server is not None:
            self._server.shutdown()
            self._server = None
