"""OAuth 2.1 authorization server for the MCP endpoint.

The MCP SDK mounts the standard OAuth routes (`_auth_server_provider`) and calls
these methods; login/consent pages live in oauth_web. Persistence is in
OAuthStore and user authentication is handled during the web flow.
"""

import asyncio
import secrets
import uuid
from datetime import datetime, timedelta, timezone
from urllib.parse import urlparse

from mcp.server.auth.provider import (
    AccessToken,
    AuthorizationCode,
    AuthorizationParams,
    AuthorizeError,
    OAuthAuthorizationServerProvider,
    OAuthToken,
    RefreshToken,
    RegistrationError,
    TokenError,
)
from mcp.shared.auth import OAuthClientInformationFull

from .auth import SCOPE, resolve_key_user
from .hashing import KEY_PREFIX, hash_secret
from .oauth_store import OAuthStore, StoredCode

ACCESS_PREFIX = "cp_oauth_"
REFRESH_PREFIX = "cp_ort_"
AUTH_REQUEST_TTL_SECONDS = 600
CODE_TTL_SECONDS = 300


def _now() -> datetime:
    return datetime.now(timezone.utc)


class CheckpointOAuthProvider(OAuthAuthorizationServerProvider):
    def __init__(self, store: OAuthStore, public_url: str, key_dsn: str,
                 allowed_redirect_hosts: set[str], access_ttl_seconds: int,
                 refresh_ttl_seconds: int) -> None:
        self._store = store
        self._public_url = public_url.rstrip("/")
        self._key_dsn = key_dsn
        self._allowed_hosts = allowed_redirect_hosts
        self._access_ttl = access_ttl_seconds
        self._refresh_ttl = refresh_ttl_seconds

    def redirect_allowed(self, redirect_uri: str) -> bool:
        host = (urlparse(redirect_uri).hostname or "").lower()
        return host in self._allowed_hosts or host in ("localhost", "127.0.0.1")

    # --- client registration -------------------------------------------------

    async def get_client(self, client_id: str) -> OAuthClientInformationFull | None:
        metadata = await asyncio.to_thread(self._store.get_client_metadata, client_id)
        if metadata is None:
            return None
        return OAuthClientInformationFull.model_validate_json(metadata)

    async def register_client(self, client_info: OAuthClientInformationFull) -> None:
        for uri in client_info.redirect_uris or []:
            if not self.redirect_allowed(str(uri)):
                raise RegistrationError("invalid_redirect_uri", f"redirect_uri host not allowed: {uri}")
        await asyncio.to_thread(self._store.save_client, client_info.client_id, client_info.model_dump_json())

    # --- authorization -------------------------------------------------------

    async def authorize(self, client: OAuthClientInformationFull, params: AuthorizationParams) -> str:
        if not self.redirect_allowed(str(params.redirect_uri)):
            raise AuthorizeError("invalid_request", "redirect_uri host not allowed")
        scopes = " ".join(params.scopes) if params.scopes else SCOPE
        request_id = await asyncio.to_thread(
            self._store.create_auth_request,
            client.client_id,
            str(params.redirect_uri),
            params.redirect_uri_provided_explicitly,
            scopes,
            params.code_challenge,
            params.state,
            params.resource,
            _now() + timedelta(seconds=AUTH_REQUEST_TTL_SECONDS),
        )
        return f"{self._public_url}/login?req={request_id}"

    async def load_authorization_code(
        self, client: OAuthClientInformationFull, authorization_code: str
    ) -> AuthorizationCode | None:
        stored = await asyncio.to_thread(self._store.load_code, hash_secret(authorization_code))
        if stored is None or stored.client_id != client.client_id:
            return None
        return _to_authorization_code(stored, authorization_code)

    async def exchange_authorization_code(
        self, client: OAuthClientInformationFull, authorization_code: AuthorizationCode
    ) -> OAuthToken:
        code_hash = hash_secret(authorization_code.code)
        if not await asyncio.to_thread(self._store.consume_code, code_hash):
            raise TokenError("invalid_grant", "authorization code already used")
        return await self._issue_tokens(
            client.client_id, authorization_code.subject or "", authorization_code.scopes,
            authorization_code.resource,
        )

    # --- refresh -------------------------------------------------------------

    async def load_refresh_token(
        self, client: OAuthClientInformationFull, refresh_token: str
    ) -> RefreshToken | None:
        stored = await asyncio.to_thread(self._store.load_token, hash_secret(refresh_token), "refresh")
        if stored is None or stored.client_id != client.client_id:
            return None
        return RefreshToken(
            token=refresh_token, client_id=stored.client_id, scopes=stored.scopes.split(),
            expires_at=int(stored.expires_at.timestamp()), resource=stored.resource,
            subject=stored.user_id,
        )

    async def exchange_refresh_token(
        self, client: OAuthClientInformationFull, refresh_token: RefreshToken, scopes: list[str]
    ) -> OAuthToken:
        await asyncio.to_thread(self._store.revoke_family_by_token, hash_secret(refresh_token.token))
        return await self._issue_tokens(
            client.client_id, refresh_token.subject or "", refresh_token.scopes, refresh_token.resource,
        )

    async def revoke_token(self, token: AccessToken | RefreshToken) -> None:
        await asyncio.to_thread(self._store.revoke_family_by_token, hash_secret(token.token))

    # --- resource server -----------------------------------------------------

    async def load_access_token(self, token: str) -> AccessToken | None:
        if token.startswith(KEY_PREFIX):
            user_id = await asyncio.to_thread(resolve_key_user, self._key_dsn, token)
            if not user_id:
                return None
            return AccessToken(token=token, client_id=user_id, scopes=[SCOPE])
        if not token.startswith(ACCESS_PREFIX):
            return None
        stored = await asyncio.to_thread(self._store.load_token, hash_secret(token), "access")
        if stored is None:
            return None
        return AccessToken(
            token=token, client_id=stored.user_id, scopes=stored.scopes.split(),
            expires_at=int(stored.expires_at.timestamp()), resource=stored.resource,
            subject=stored.user_id,
        )

    # --- helpers -------------------------------------------------------------

    async def _issue_tokens(self, client_id: str, user_id: str, scopes: list[str],
                            resource: str | None) -> OAuthToken:
        if not user_id:
            raise TokenError("invalid_grant", "missing token subject")
        access = ACCESS_PREFIX + secrets.token_urlsafe(32)
        refresh = REFRESH_PREFIX + secrets.token_urlsafe(32)
        scope_text = " ".join(scopes) if scopes else SCOPE
        await asyncio.to_thread(
            self._store.save_token_pair,
            str(uuid.uuid4()),
            client_id, user_id, scope_text, resource,
            hash_secret(access), _now() + timedelta(seconds=self._access_ttl),
            hash_secret(refresh), _now() + timedelta(seconds=self._refresh_ttl),
        )
        return OAuthToken(
            access_token=access, expires_in=self._access_ttl, scope=scope_text,
            refresh_token=refresh,
        )


def _to_authorization_code(stored: StoredCode, code: str) -> AuthorizationCode:
    return AuthorizationCode(
        code=code, scopes=stored.scopes.split(), expires_at=stored.expires_at.timestamp(),
        client_id=stored.client_id, code_challenge=stored.code_challenge,
        redirect_uri=stored.redirect_uri, redirect_uri_provided_explicitly=True,
        resource=stored.resource, subject=stored.user_id,
    )
