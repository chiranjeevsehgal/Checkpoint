"""Direct upload to ingestion-service. Stdlib only; blocking calls run in an executor."""

import asyncio
import hashlib
import json
import time
import urllib.error
import urllib.request

from . import config as cfg


def http_json(method: str, url: str, body: dict | None, headers: dict,
              timeout: float) -> tuple[int, dict | bytes]:
    raw = json.dumps(body).encode() if body is not None else None
    req_headers = dict(headers)
    if body is not None:
        req_headers.setdefault("Content-Type", "application/json")
    req = urllib.request.Request(url, data=raw, headers=req_headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            payload = resp.read()
            status = resp.status
    except urllib.error.HTTPError as e:
        try:
            err_body = e.read().decode(errors="ignore")[:500]
        except Exception:
            err_body = ""
        raise RuntimeError(f"{method} {url} -> HTTP {e.code}: {err_body}")
    ctype = ""
    try:
        ctype = resp.headers.get("Content-Type", "")
    except Exception:
        pass
    if "json" in ctype.lower() or (payload[:1] in (b"{", b"[")):
        try:
            return status, json.loads(payload.decode())
        except Exception:
            return status, payload
    return status, payload


def http_put_bytes(url: str, data: bytes, content_type: str, timeout: float) -> int:
    req = urllib.request.Request(
        url, data=data,
        headers={"Content-Type": content_type, "Content-Length": str(len(data))},
        method="PUT",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status
    except urllib.error.HTTPError as e:
        try:
            err_body = e.read().decode(errors="ignore")[:500]
        except Exception:
            err_body = ""
        raise RuntimeError(f"PUT presigned -> HTTP {e.code}: {err_body}")


class IngestionUploader:
    """3-step upload (create, PUT to MinIO, complete) + queue-wait poll to SUBMITTED."""

    def __init__(self, base_url: str = cfg.INGEST_BASE_URL,
                 user_id: str = cfg.INGEST_USER_ID,
                 timeout: float = cfg.INGEST_TIMEOUT_S,
                 poll_enabled: bool = cfg.INGEST_POLL_ENABLED_DEFAULT,
                 poll_timeout: float = cfg.INGEST_POLL_TIMEOUT_S,
                 poll_interval: float = cfg.INGEST_POLL_INTERVAL_S):
        self.base_url = (base_url or cfg.INGEST_BASE_URL).rstrip("/")
        self.user_id = user_id or cfg.INGEST_USER_ID
        self.timeout = timeout
        self.poll_enabled = poll_enabled
        self.poll_timeout = poll_timeout
        self.poll_interval = poll_interval if poll_interval > 0 else 1.0

    def _auth(self, extra: dict | None = None) -> dict:
        headers = {"Authorization": f"Bearer {self.user_id}"}
        if extra:
            headers.update(extra)
        return headers

    def get_status_sync(self, upload_id: str) -> str:
        status, body = http_json(
            "GET", f"{self.base_url}/v1/uploads/{upload_id}",
            None, self._auth(), self.timeout)
        if status != 200 or not isinstance(body, dict) or "status" not in body:
            raise RuntimeError(f"get status HTTP {status}: {str(body)[:300]}")
        return str(body.get("status", ""))

    def _wait_submitted(self, upload_id: str) -> tuple[str, float]:
        start = time.monotonic()
        last = "READY"
        deadline = start + max(0.0, self.poll_timeout)
        while time.monotonic() < deadline:
            time.sleep(self.poll_interval)
            try:
                last = self.get_status_sync(upload_id) or last
            except Exception:
                continue
            if last == "SUBMITTED":
                break
            if last not in ("READY", "SUBMITTED", "UPLOADING"):
                break
        return last, time.monotonic() - start

    def upload_sync(self, data: bytes, filename: str,
                    content_type: str = "audio/ogg",
                    idempotency_key: str | None = None) -> tuple[str, str]:
        size = len(data)
        if size > cfg.INGEST_MAX_BYTES:
            raise RuntimeError(f"too-large: {size} > {cfg.INGEST_MAX_BYTES}")
        headers = self._auth()
        if idempotency_key:
            headers["Idempotency-Key"] = idempotency_key[:128]
        status, body = http_json(
            "POST", f"{self.base_url}/v1/uploads",
            {"filename": filename, "content_type": content_type, "size_bytes": size},
            headers, self.timeout)
        if not isinstance(body, dict) or "upload_id" not in body or "upload" not in body:
            raise RuntimeError(f"create: unexpected response HTTP {status}: {str(body)[:300]}")
        upload_id = body["upload_id"]
        put_url = body["upload"]["url"]
        put_status = http_put_bytes(put_url, data, content_type, timeout=max(self.timeout, 30))
        if put_status not in (200, 201, 204):
            raise RuntimeError(f"PUT presigned -> HTTP {put_status}")
        checksum = hashlib.sha256(data).hexdigest()
        c_status, c_body = http_json(
            "POST", f"{self.base_url}/v1/uploads/{upload_id}/complete",
            {"size_bytes": size, "checksum_sha256": checksum},
            self._auth(), self.timeout)
        c_status_str = c_body.get("status", "") if isinstance(c_body, dict) else ""
        if c_status != 200 or c_status_str not in ("READY", "SUBMITTED"):
            raise RuntimeError(f"complete HTTP {c_status}: {str(c_body)[:300]}")
        if c_status_str == "SUBMITTED" or not self.poll_enabled:
            return upload_id, c_status_str
        final, _waited = self._wait_submitted(upload_id)
        return upload_id, final

    async def upload_async(self, data: bytes, filename: str,
                           content_type: str = "audio/ogg",
                           idempotency_key: str | None = None) -> tuple[str, str]:
        loop = asyncio.get_event_loop()
        return await loop.run_in_executor(
            None, self.upload_sync, data, filename, content_type, idempotency_key)
