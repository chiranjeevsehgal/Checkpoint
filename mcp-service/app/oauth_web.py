"""Login and consent pages for the MCP OAuth flow.

The MCP SDK serves the standard OAuth routes; this module adds the two
human-facing steps. Login emails a one-time code through Kratos (native login
API, `code` method), then a short-lived signed cookie carries the user into
consent. Markup and styling live in oauth_pages.
"""

import asyncio
import html
import json
import secrets
import time
import urllib.error
import urllib.request
import uuid
from datetime import datetime, timedelta, timezone
from urllib.parse import urlparse, urlunparse

from mcp.server.auth.provider import construct_redirect_uri
from starlette.responses import RedirectResponse, Response

from .hashing import hash_secret
from .oauth import CODE_TTL_SECONDS, CheckpointOAuthProvider
from .oauth_pages import (
    CODE_BODY,
    CONSENT_BODY,
    LOGIN_BODY,
    error_block,
    error_response,
    render,
)
from .oauth_session import csrf_token, sign_session, verify_csrf, verify_session
from .oauth_store import OAuthStore
from .ratelimit import RateLimiter

SESSION_COOKIE = "cp_mcp_oauth"
_KRATOS_TIMEOUT_SECONDS = 10
_KRATOS_MAX_ATTEMPTS = 3
_KRATOS_RETRY_BACKOFFS = (0.5, 1.0)
_LOGIN_SEND_PER_MINUTE = 3
_LOGIN_SEND_BURST = 3


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
    # Retry only network errors (no response received, so no double-submit):
    # an HTTPError already carries the server's answer.
    for attempt in range(1, _KRATOS_MAX_ATTEMPTS + 1):
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
            if attempt < _KRATOS_MAX_ATTEMPTS:
                time.sleep(_KRATOS_RETRY_BACKOFFS[attempt - 1])
    return 0, {}


def _verified_user_id(kratos_url: str, session_token: str) -> str | None:
    status, session = _kratos_call(kratos_url.rstrip("/") + "/sessions/whoami", "GET", token=session_token)
    if status != 200 or not session.get("active"):
        return None
    addresses = session.get("identity", {}).get("verifiable_addresses") or []
    if not any(a.get("via") == "email" and a.get("verified") for a in addresses):
        return None
    return session.get("identity", {}).get("id")


def _is_uuid(value: object) -> bool:
    try:
        uuid.UUID(str(value))
    except (ValueError, TypeError):
        return False
    return True


def _message_texts(payload: dict) -> list[str]:
    ui = payload.get("ui") or {}
    texts = [m["text"] for m in (ui.get("messages") or [])
             if isinstance(m, dict) and isinstance(m.get("text"), str)]
    for node in ui.get("nodes") or []:
        texts += [m["text"] for m in (node.get("messages") or [])
                  if isinstance(m, dict) and isinstance(m.get("text"), str)]
    error = payload.get("error") or {}
    texts += [error[key] for key in ("message", "reason") if isinstance(error.get(key), str)]
    return texts


def _kratos_error(payload: dict, fallback: str) -> str:
    texts = _message_texts(payload)
    return texts[0] if texts else fallback


def _session_token(payload: dict) -> str | None:
    token = payload.get("session_token")
    if isinstance(token, str) and token:
        return token
    # API clients may receive the token through continue_with instead (kratos.yml).
    for entry in payload.get("continue_with") or []:
        if entry.get("action") == "set_ory_session_token" and entry.get("ory_session_token"):
            return entry["ory_session_token"]
    return None


def kratos_login_start(kratos_url: str, email: str) -> tuple[str | None, str | None]:
    """Email a one-time sign-in code. Returns (kratos_flow_id, error)."""
    status, flow = _kratos_call(kratos_url.rstrip("/") + "/self-service/login/api", "GET")
    if status != 200 or "ui" not in flow:
        return None, "Sign-in is temporarily unavailable. Try again shortly."
    action = _resolve_kratos_url(kratos_url, flow["ui"]["action"])
    status, result = _kratos_call(action, "POST", {"method": "code", "identifier": email})
    # Kratos answers 400 for API/SPA clients once the code is sent; the body is
    # the updated flow whose state is "email_sent".
    if result.get("state") == "email_sent" and _is_uuid(result.get("id")):
        return str(result["id"]), None
    if status == 0:
        return None, "Sign-in is temporarily unavailable. Try again shortly."
    # Generic on purpose: Kratos' own message would confirm the account exists.
    return None, "We couldn't send a code to that address."


def kratos_login_finish(kratos_url: str, flow_id: str, email: str,
                        code: str) -> tuple[str | None, str | None]:
    """Verify the emailed code. Returns (user_id, error)."""
    if not _is_uuid(flow_id):
        return None, "This sign-in attempt expired. Start again."
    action = _resolve_kratos_url(kratos_url, f"/self-service/login/api?flow={flow_id}")
    status, result = _kratos_call(action, "POST",
                                  {"method": "code", "identifier": email, "code": code})
    token = _session_token(result) if status == 200 else None
    if not token:
        return None, _kratos_error(result, "The code is incorrect or has expired.")
    user_id = _verified_user_id(kratos_url, token)
    if not user_id:
        return None, "Verify your email in the Checkpoint app before connecting."
    return user_id, None


def register_oauth_routes(server, provider: CheckpointOAuthProvider, store: OAuthStore,
                          secret: str, kratos_url: str, public_url: str) -> None:
    cookie_secure = public_url.startswith("https://")
    login_send_limiter = RateLimiter(_LOGIN_SEND_PER_MINUTE, _LOGIN_SEND_BURST)

    @server.custom_route("/login", methods=["GET", "POST"])
    async def login(request) -> Response:
        request_id = (request.query_params.get("req") or await _form_value(request, "req")).strip()
        auth_request = await asyncio.to_thread(store.load_auth_request, request_id) if request_id else None
        if auth_request is None:
            return render("Sign in", LOGIN_BODY, req="",
                          error=error_block("This sign-in link has expired."))
        if request.method == "GET":
            user_id = verify_session(secret, request.cookies.get(SESSION_COOKIE))
            if user_id:
                return RedirectResponse(f"/consent?req={request_id}", status_code=303)
            return render("Sign in", LOGIN_BODY, req=html.escape(request_id), error="")
        email = (await _form_value(request, "email")).strip()
        flow_id = (await _form_value(request, "flow")).strip()
        if flow_id:
            code = (await _form_value(request, "code")).strip()
            user_id, error = await asyncio.to_thread(
                kratos_login_finish, kratos_url, flow_id, email, code)
            if error or not user_id:
                return render("Check your email", CODE_BODY, req=html.escape(request_id),
                              flow=html.escape(flow_id), email=html.escape(email),
                              error=error_block(error))
        elif not email:
            return render("Sign in", LOGIN_BODY, req=html.escape(request_id),
                          error=error_block("Enter your email address."))
        else:
            allowed, _ = login_send_limiter.allow(email.lower())
            if not allowed:
                return render("Sign in", LOGIN_BODY, req=html.escape(request_id),
                              error=error_block("Too many code requests. Try again shortly."))
            flow_id, error = await asyncio.to_thread(kratos_login_start, kratos_url, email)
            if error or not flow_id:
                return render("Sign in", LOGIN_BODY, req=html.escape(request_id),
                              error=error_block(error))
            return render("Check your email", CODE_BODY, req=html.escape(request_id),
                          flow=html.escape(flow_id), email=html.escape(email), error="")
        response = RedirectResponse(f"/consent?req={request_id}", status_code=303)
        response.set_cookie(SESSION_COOKIE, sign_session(secret, user_id), max_age=600,
                            httponly=True, secure=cookie_secure, samesite="lax", path="/")
        return response

    @server.custom_route("/consent", methods=["GET", "POST"])
    async def consent(request) -> Response:
        request_id = (request.query_params.get("req") or await _form_value(request, "req")).strip()
        user_id = verify_session(secret, request.cookies.get(SESSION_COOKIE))
        if not user_id:
            return RedirectResponse(f"/login?req={request_id}", status_code=303)
        auth_request = await asyncio.to_thread(store.load_auth_request, request_id) if request_id else None
        if auth_request is None:
            return error_response("This authorization request has expired.")
        if request.method == "GET":
            client = await _client_name(provider, auth_request.client_id)
            return render("Authorize", CONSENT_BODY, client=html.escape(client),
                          scopes=html.escape(auth_request.scopes), req=html.escape(request_id),
                          csrf=csrf_token(secret, request_id))
        csrf = await _form_value(request, "csrf")
        if not verify_csrf(secret, request_id, csrf):
            return error_response("Invalid consent submission. Start again from the client.")
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
