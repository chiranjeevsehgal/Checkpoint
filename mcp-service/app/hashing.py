import hashlib

KEY_PREFIX = "cp_mcp_"


def hash_secret(secret: str) -> bytes:
    """SHA-256 of an MCP access key; matches ingestion's storage format."""
    return hashlib.sha256(secret.encode("utf-8")).digest()


def parse_bearer(authorization: str | None) -> str | None:
    """Return the token from a `Bearer <token>` header, or None."""
    if not authorization:
        return None
    scheme, _, token = authorization.partition(" ")
    if scheme.lower() != "bearer":
        return None
    token = token.strip()
    if not token or not token.startswith(KEY_PREFIX):
        return None
    return token
