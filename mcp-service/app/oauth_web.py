"""Login and consent pages for the MCP OAuth flow.

The MCP SDK serves the standard OAuth routes; this module adds the two
human-facing steps. Login drives the same Kratos API flow the mobile app uses,
then a short-lived signed cookie carries the user into consent.
"""

import asyncio
import html
import json
import secrets
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone
from urllib.parse import urlparse, urlunparse

from mcp.server.auth.provider import construct_redirect_uri
from starlette.responses import HTMLResponse, RedirectResponse, Response

from .hashing import hash_secret
from .oauth import CODE_TTL_SECONDS, CheckpointOAuthProvider
from .oauth_session import csrf_token, sign_session, verify_csrf, verify_session
from .oauth_store import OAuthStore

SESSION_COOKIE = "cp_mcp_oauth"
_KRATOS_TIMEOUT_SECONDS = 10

_LOGIN_HTML = """<!doctype html><html><head><meta charset="utf-8">
<title>Checkpoint sign in</title></head><body>
<h1>Sign in to Checkpoint</h1>
{error}
<form method="post" action="/login">
<input type="hidden" name="req" value="{req}">
<p><label>Email <input type="email" name="email" autocomplete="username" required></label></p>
<p><label>Password <input type="password" name="password" autocomplete="current-password" required></label></p>
<p><button type="submit">Sign in</button></p>
</form></body></html>"""

_CONSENT_HTML = """<!doctype html><html><head><meta charset="utf-8">
<title>Authorize {client}</title></head><body>
<h1>Authorize {client}</h1>
<p>{client} is requesting access to your Checkpoint recordings.</p>
<p>Scope: <strong>{scopes}</strong></p>
<form method="post" action="/consent">
<input type="hidden" name="req" value="{req}">
<input type="hidden" name="csrf" value="{csrf}">
<p><button type="submit" name="decision" value="allow">Allow</button>
<button type="submit" name="decision" value="deny">Deny</button></p>
</form></body></html>"""


def _error_block(message: str | None) -> str:
    return f'<p role="alert">{html.escape(message)}</p>' if message else ""


def _render(template: str, **values: str) -> HTMLResponse:
    return HTMLResponse(template.format(**values))


async def _form_value(request, name: str) -> str:
    form = await request.form()
    value = form.get(name)
    return value if isinstance(value, str) else ""


def _resolve_kratos_url(base: str, action: str) -> str:
    if not action.startswith(("http://", "https://")):
        return base.rstrip("/") + action
    target = urlparse(action)
    return urlunparse(target._replace(scheme=urlparse(base).scheme, netloc=urlparse(base).netloc))


def _kratos_call(url: str, method: str, body: dict | None = None,
                 token: str | None = None) -> tuple[int, dict]:
    data = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(url, data=data, method=method)
    request.add_header("Accept", "application/json")
    if data is not None:
        request.add_header("Content-Type", "application/json")
    if token:
        request.add_header("X-Session-Token", token)
    try:
        with urllib.request.urlopen(request, timeout=_KRATOS_TIMEOUT_SECONDS) as response:
            return response.status, json.loads(response.read() or b"{}")
    except urllib.error.HTTPError as exc:
        try:
            payload = json.loads(exc.read() or b"{}")
        except ValueError:
            payload = {}
        return exc.code, payload
    except (urllib.error.URLError, TimeoutError):
        return 0, {}


def _verified_user_id(kratos_url: str, session_token: str) -> str | None:
    status, session = _kratos_call(kratos_url.rstrip("/") + "/sessions/whoami", "GET", token=session_token)
    if status != 200 or not session.get("active"):
        return None
    addresses = session.get("identity", {}).get("verifiable_addresses") or []
    if not any(a.get("via") == "email" and a.get("verified") for a in addresses):
        return None
    return session.get("identity", {}).get("id")


def kratos_login(kratos_url: str, email: str, password: str) -> tuple[str | None, str | None]:
    """Return (user_id, error). Drives the Kratos native login API flow."""
    status, flow = _kratos_call(kratos_url.rstrip("/") + "/self-service/login/api", "GET")
    if status != 200 or "ui" not in flow:
        return None, "Sign-in is temporarily unavailable. Try again shortly."
    action = _resolve_kratos_url(kratos_url, flow["ui"]["action"])
    status, result = _kratos_call(action, "POST",
                                  {"method": "password", "identifier": email, "password": password})
    token = result.get("session_token")
    if status != 200 or not token:
        return None, "Email or password is incorrect."
    user_id = _verified_user_id(kratos_url, token)
    if not user_id:
        return None, "Verify your email in the Checkpoint app before connecting."
    return user_id, None


def register_oauth_routes(server, provider: CheckpointOAuthProvider, store: OAuthStore,
                          secret: str, kratos_url: str) -> None:
    @server.custom_route("/login", methods=["GET", "POST"])
    async def login(request) -> Response:
        request_id = request.query_params.get("req") or await _form_value(request, "req")
        auth_request = await asyncio.to_thread(store.load_auth_request, request_id)
        if auth_request is None:
            return _render(_LOGIN_HTML, req="", error=_error_block("This sign-in link has expired."))
        if request.method == "GET":
            user_id = verify_session(secret, request.cookies.get(SESSION_COOKIE))
            if user_id:
                return RedirectResponse(f"/consent?req={request_id}", status_code=303)
            return _render(_LOGIN_HTML, req=html.escape(request_id), error="")
        email = await _form_value(request, "email")
        password = await _form_value(request, "password")
        user_id, error = await asyncio.to_thread(kratos_login, kratos_url, email, password)
        if error or not user_id:
            return _render(_LOGIN_HTML, req=html.escape(request_id), error=_error_block(error))
        response = RedirectResponse(f"/consent?req={request_id}", status_code=303)
        response.set_cookie(SESSION_COOKIE, sign_session(secret, user_id), max_age=600,
                            httponly=True, secure=True, samesite="lax", path="/")
        return response

    @server.custom_route("/consent", methods=["GET", "POST"])
    async def consent(request) -> Response:
        request_id = request.query_params.get("req") or await _form_value(request, "req")
        user_id = verify_session(secret, request.cookies.get(SESSION_COOKIE))
        if not user_id:
            return RedirectResponse(f"/login?req={request_id}", status_code=303)
        auth_request = await asyncio.to_thread(store.load_auth_request, request_id)
        if auth_request is None:
            return _error("This authorization request has expired.")
        if request.method == "GET":
            client = await _client_name(provider, auth_request.client_id)
            return _render(_CONSENT_HTML, client=html.escape(client), scopes=html.escape(auth_request.scopes),
                           req=html.escape(request_id), csrf=csrf_token(secret, request_id))
        csrf = await _form_value(request, "csrf")
        if not verify_csrf(secret, request_id, csrf):
            return _error("Invalid consent submission. Start again from the client.")
        if await _form_value(request, "decision") != "allow":
            return RedirectResponse(
                construct_redirect_uri(auth_request.redirect_uri, error="access_denied",
                                       state=auth_request.state),
                status_code=303,
            )
        code = secrets.token_urlsafe(32)
        await asyncio.to_thread(
            store.save_code, hash_secret(code), auth_request.client_id, user_id,
            auth_request.redirect_uri, auth_request.scopes, auth_request.code_challenge,
            auth_request.resource, datetime.now(timezone.utc) + timedelta(seconds=CODE_TTL_SECONDS),
        )
        await asyncio.to_thread(store.delete_auth_request, request_id)
        return RedirectResponse(
            construct_redirect_uri(auth_request.redirect_uri, code=code, state=auth_request.state),
            status_code=303,
        )


async def _client_name(provider: CheckpointOAuthProvider, client_id: str) -> str:
    client = await provider.get_client(client_id)
    return client.client_name if client and client.client_name else "An AI assistant"


def _error(message: str) -> HTMLResponse:
    return HTMLResponse(f"<!doctype html><html><body><p>{html.escape(message)}</p></body></html>", status_code=400)
