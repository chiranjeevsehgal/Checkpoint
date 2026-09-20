"""Signed session and CSRF tokens for the MCP OAuth login/consent pages.

Pure helpers with no framework imports so they stay unit-testable without the
ML extras. The session cookie carries the authenticated Checkpoint user id and
an expiry; the CSRF token binds a consent submission to its request id.
"""

import hashlib
import hmac
import time

SESSION_TTL_SECONDS = 600


def _sign(secret: str, message: str) -> str:
    return hmac.new(secret.encode("utf-8"), message.encode("utf-8"), hashlib.sha256).hexdigest()


def sign_session(secret: str, user_id: str, ttl_seconds: int = SESSION_TTL_SECONDS) -> str:
    expires_at = int(time.time()) + ttl_seconds
    message = f"{user_id}.{expires_at}"
    return f"{message}.{_sign(secret, message)}"


def verify_session(secret: str, value: str | None) -> str | None:
    if not value:
        return None
    # Value shape: <user_id>.<exp>.<sig>; a uuid never contains dots.
    parts = value.split(".")
    if len(parts) != 3:
        return None
    user_id, expires_at_text, signature = parts
    if not hmac.compare_digest(_sign(secret, f"{user_id}.{expires_at_text}"), signature):
        return None
    try:
        expires_at = int(expires_at_text)
    except ValueError:
        return None
    if expires_at < int(time.time()):
        return None
    return user_id


def csrf_token(secret: str, request_id: str) -> str:
    return _sign(secret, f"consent:{request_id}")


def verify_csrf(secret: str, request_id: str, token: str | None) -> bool:
    if not token:
        return False
    return hmac.compare_digest(csrf_token(secret, request_id), token)
