"""Postgres persistence for the MCP OAuth authorization server.

Connects as the BYPASSRLS worker role because tokens and clients are looked up
by hash before any account is known. Secrets are stored only as SHA-256 hashes.
The connection is guarded by a lock and shared, matching store.ReadStore.
"""

import threading
import uuid
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime, timezone

try:
    import psycopg
    from psycopg.rows import dict_row
except ModuleNotFoundError:  # pragma: no cover - exercised only without psycopg
    psycopg = None
    dict_row = None


@dataclass
class AuthRequest:
    id: str
    client_id: str
    redirect_uri: str
    redirect_uri_provided_explicitly: bool
    scopes: str
    code_challenge: str
    state: str | None
    resource: str | None


@dataclass
class StoredCode:
    user_id: str
    client_id: str
    redirect_uri: str
    scopes: str
    code_challenge: str
    resource: str | None
    expires_at: datetime


@dataclass
class StoredToken:
    user_id: str
    client_id: str
    scopes: str
    resource: str | None
    expires_at: datetime


class OAuthStore:
    def __init__(self, dsn: str) -> None:
        self._dsn = dsn
        self._conn = None
        self._lock = threading.Lock()

    def connect(self) -> None:
        if psycopg is None:
            raise RuntimeError("psycopg is not installed")
        self._conn = psycopg.connect(self._dsn, autocommit=True)

    def close(self) -> None:
        if self._conn is not None:
            self._conn.close()
            self._conn = None

    @contextmanager
    def _cursor(self):
        with self._lock, self._conn.cursor(row_factory=dict_row) as cur:
            yield cur

    def save_client(self, client_id: str, metadata_json: str) -> None:
        with self._cursor() as cur:
            cur.execute(
                "INSERT INTO oauth_clients (client_id, metadata) VALUES (%s, %s::jsonb) "
                "ON CONFLICT (client_id) DO UPDATE SET metadata = EXCLUDED.metadata",
                (client_id, metadata_json),
            )

    def get_client_metadata(self, client_id: str) -> str | None:
        with self._cursor() as cur:
            cur.execute("SELECT metadata::text FROM oauth_clients WHERE client_id = %s", (client_id,))
            row = cur.fetchone()
        return row["metadata"] if row else None

    def create_auth_request(
        self,
        client_id: str,
        redirect_uri: str,
        redirect_uri_provided_explicitly: bool,
        scopes: str,
        code_challenge: str,
        state: str | None,
        resource: str | None,
        expires_at: datetime,
    ) -> str:
        request_id = str(uuid.uuid4())
        with self._cursor() as cur:
            cur.execute(
                "INSERT INTO oauth_auth_requests "
                "(id, client_id, redirect_uri, redirect_uri_provided_explicitly, scopes, "
                " code_challenge, state, resource, expires_at) "
                "VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)",
                (request_id, client_id, redirect_uri, redirect_uri_provided_explicitly,
                 scopes, code_challenge, state, resource, expires_at),
            )
        return request_id

    def load_auth_request(self, request_id: str) -> AuthRequest | None:
        with self._cursor() as cur:
            cur.execute(
                "SELECT id::text, client_id, redirect_uri, redirect_uri_provided_explicitly, "
                "scopes, code_challenge, state, resource FROM oauth_auth_requests "
                "WHERE id = %s AND expires_at > NOW()",
                (request_id,),
            )
            row = cur.fetchone()
        if not row:
            return None
        return AuthRequest(
            id=row["id"],
            client_id=row["client_id"],
            redirect_uri=row["redirect_uri"],
            redirect_uri_provided_explicitly=row["redirect_uri_provided_explicitly"],
            scopes=row["scopes"],
            code_challenge=row["code_challenge"],
            state=row["state"],
            resource=row["resource"],
        )

    def delete_auth_request(self, request_id: str) -> None:
        with self._cursor() as cur:
            cur.execute("DELETE FROM oauth_auth_requests WHERE id = %s", (request_id,))

    def save_code(
        self, code_hash: bytes, client_id: str, user_id: str, redirect_uri: str,
        scopes: str, code_challenge: str, resource: str | None, expires_at: datetime,
    ) -> None:
        with self._cursor() as cur:
            cur.execute(
                "INSERT INTO oauth_authorization_codes "
                "(code_hash, client_id, user_id, redirect_uri, scopes, code_challenge, "
                " resource, expires_at) VALUES (%s, %s, %s, %s, %s, %s, %s, %s)",
                (code_hash, client_id, user_id, redirect_uri, scopes, code_challenge,
                 resource, expires_at),
            )

    def load_code(self, code_hash: bytes) -> StoredCode | None:
        with self._cursor() as cur:
            cur.execute(
                "SELECT user_id::text, client_id, redirect_uri, scopes, code_challenge, "
                "resource, expires_at FROM oauth_authorization_codes "
                "WHERE code_hash = %s AND consumed_at IS NULL AND expires_at > NOW()",
                (code_hash,),
            )
            row = cur.fetchone()
        if not row:
            return None
        return StoredCode(
            user_id=row["user_id"], client_id=row["client_id"], redirect_uri=row["redirect_uri"],
            scopes=row["scopes"], code_challenge=row["code_challenge"], resource=row["resource"],
            expires_at=row["expires_at"],
        )

    def consume_code(self, code_hash: bytes) -> bool:
        with self._cursor() as cur:
            cur.execute(
                "UPDATE oauth_authorization_codes SET consumed_at = NOW() "
                "WHERE code_hash = %s AND consumed_at IS NULL",
                (code_hash,),
            )
            return cur.rowcount > 0

    def save_token_pair(
        self, family_id: str, client_id: str, user_id: str, scopes: str, resource: str | None,
        access_hash: bytes, access_expires: datetime, refresh_hash: bytes, refresh_expires: datetime,
    ) -> None:
        with self._cursor() as cur:
            cur.execute(
                "INSERT INTO oauth_tokens "
                "(token_hash, kind, family_id, user_id, client_id, scopes, resource, expires_at) "
                "VALUES (%s, 'access', %s, %s, %s, %s, %s, %s), "
                "       (%s, 'refresh', %s, %s, %s, %s, %s, %s)",
                (access_hash, family_id, user_id, client_id, scopes, resource, access_expires,
                 refresh_hash, family_id, user_id, client_id, scopes, resource, refresh_expires),
            )

    def load_token(self, token_hash: bytes, kind: str) -> StoredToken | None:
        with self._cursor() as cur:
            cur.execute(
                "SELECT user_id::text, client_id, scopes, resource, expires_at FROM oauth_tokens "
                "WHERE token_hash = %s AND kind = %s AND revoked_at IS NULL AND expires_at > NOW()",
                (token_hash, kind),
            )
            row = cur.fetchone()
        if not row:
            return None
        return StoredToken(
            user_id=row["user_id"], client_id=row["client_id"], scopes=row["scopes"],
            resource=row["resource"], expires_at=row["expires_at"],
        )

    def revoke_family_by_token(self, token_hash: bytes) -> None:
        with self._cursor() as cur:
            cur.execute(
                "UPDATE oauth_tokens SET revoked_at = NOW() WHERE revoked_at IS NULL AND family_id IN "
                "(SELECT family_id FROM oauth_tokens WHERE token_hash = %s)",
                (token_hash,),
            )
