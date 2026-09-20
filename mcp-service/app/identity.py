"""Best-effort Kratos Admin lookup for the account's display name.

The MCP holds no Kratos session token at tool time (auth is a cp_mcp_ key or
OAuth token resolved to a user id), so identity traits are read through the
private admin API, exactly as ingestion's deletion worker does. Any failure
yields None so whoami never fails on Kratos.
"""

import json
import logging
import urllib.error
import urllib.request
import uuid

log = logging.getLogger(__name__)

_TIMEOUT_SECONDS = 5


def fetch_name(admin_url: str, user_id: str, timeout: int = _TIMEOUT_SECONDS) -> str | None:
    if not admin_url or not user_id:
        return None
    try:
        uuid.UUID(user_id)
    except (ValueError, TypeError):
        return None
    request = urllib.request.Request(
        admin_url.rstrip("/") + "/admin/identities/" + user_id, method="GET"
    )
    request.add_header("Accept", "application/json")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            identity = json.loads(response.read() or b"{}")
    except (urllib.error.URLError, TimeoutError, ValueError) as exc:
        log.warning("kratos identity lookup failed: %s", exc)
        return None
    name = (identity.get("traits") or {}).get("name")
    return name if isinstance(name, str) and name.strip() else None
