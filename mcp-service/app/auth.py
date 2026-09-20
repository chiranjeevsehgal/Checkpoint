"""Bearer access-key verification for the MCP endpoint.

Every request presents `Authorization: Bearer cp_mcp_...`; the verifier
hashes it and resolves the owning user through the SECURITY DEFINER
mcp_resolve_key function. Tools read that user id from the request context.
"""

import asyncio
import logging

from mcp.server.auth.middleware.auth_context import get_access_token
from mcp.server.auth.provider import AccessToken, TokenVerifier

try:
    import psycopg
except ModuleNotFoundError:  # pragma: no cover - only without the ML extras
    psycopg = None

from .hashing import KEY_PREFIX, hash_secret

log = logging.getLogger(__name__)

SCOPE = "checkpoint"


def current_user_id() -> str | None:
    token = get_access_token()
    return token.client_id if token else None


def resolve_key_user(dsn: str, key: str) -> str | None:
    """Resolve a presented cp_mcp_ key to its owning user id, or None."""
    if psycopg is None:
        return None
    try:
        with psycopg.connect(dsn) as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT mcp_resolve_key(%s)::text", (hash_secret(key),))
                row = cur.fetchone()
        return row[0] if row and row[0] else None
    except Exception as exc:  # noqa: BLE001 - never turn a DB blip into a 500
        log.warning("mcp key resolution failed: %s", exc)
        return None


class KeyTokenVerifier(TokenVerifier):
    def __init__(self, dsn: str) -> None:
        self._dsn = dsn

    async def verify_token(self, token: str) -> AccessToken | None:
        if not token or not token.startswith(KEY_PREFIX):
            return None
        user_id = await asyncio.to_thread(resolve_key_user, self._dsn, token)
        if user_id is None:
            return None
        return AccessToken(token=token, client_id=user_id, scopes=[SCOPE])
