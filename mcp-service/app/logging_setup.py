"""JSON logging for the service entrypoints.

Configured once at startup so every record is one JSON line on stdout, ready
for the log collector. Callers attach structured context with the standard
`extra=` argument:

    log.info("embedded audio", extra={"audio_id": audio_id, "chunks": len(chunks)})
"""

import json
import logging
import os
import sys
from datetime import datetime, timezone

_STANDARD_ATTRS = frozenset(
    logging.LogRecord("", 0, "", 0, "", (), None).__dict__
) | {"message", "asctime"}


class JsonFormatter(logging.Formatter):
    def __init__(self, service: str) -> None:
        super().__init__()
        self._service = service

    def format(self, record: logging.LogRecord) -> str:
        payload = {
            "time": datetime.fromtimestamp(record.created, tz=timezone.utc).isoformat(timespec="milliseconds"),
            "level": record.levelname,
            "service": self._service,
            "logger": record.name,
            "message": record.getMessage(),
        }
        payload.update({k: v for k, v in record.__dict__.items() if k not in _STANDARD_ATTRS})
        if record.exc_info:
            payload["exception"] = self.formatException(record.exc_info)
        return json.dumps(payload, default=str, ensure_ascii=False)


def setup(service: str) -> None:
    """Send every logger's records to stdout as JSON at the LOG_LEVEL level."""
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JsonFormatter(service))
    root = logging.getLogger()
    root.handlers[:] = [handler]
    root.setLevel(getattr(logging, os.getenv("LOG_LEVEL", "INFO").upper(), logging.INFO))
