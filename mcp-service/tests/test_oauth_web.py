import unittest
from types import SimpleNamespace
from unittest import mock

try:
    from starlette.applications import Starlette
    from starlette.routing import Route
    from starlette.testclient import TestClient

    from app import oauth_web
except ModuleNotFoundError:  # pragma: no cover - mcp/starlette deps absent
    oauth_web = None

USER_ID = "00000000-0000-0000-0000-000000000001"
FLOW_ID = "11111111-2222-3333-4444-555555555555"
REQ_ID = "22222222-3333-4444-5555-666666666666"
KRATOS = "http://kratos:4433"
PUBLIC_URL = "http://localhost:1417"
SECRET = "test-secret"
EMAIL = "asha@example.com"


@unittest.skipUnless(oauth_web, "mcp package not installed")
class HelperTest(unittest.TestCase):
    def test_message_texts_reads_ui_nodes_and_error(self):
        payload = {
            "ui": {"messages": [{"text": "top"}], "nodes": [{"messages": [{"text": "node"}]}]},
            "error": {"message": "boom", "reason": "why"},
        }
        self.assertEqual(oauth_web._message_texts(payload), ["top", "node", "boom", "why"])

    def test_kratos_error_uses_fallback_when_empty(self):
        self.assertEqual(oauth_web._kratos_error({}, "fallback"), "fallback")

    def test_session_token_prefers_top_level(self):
        self.assertEqual(oauth_web._session_token({"session_token": "s"}), "s")

    def test_session_token_reads_continue_with(self):
        payload = {"continue_with": [{"action": "set_ory_session_token", "ory_session_token": "t"}]}
        self.assertEqual(oauth_web._session_token(payload), "t")

    def test_session_token_none_when_missing(self):
        self.assertIsNone(oauth_web._session_token({}))

    def test_is_uuid(self):
        self.assertTrue(oauth_web._is_uuid(FLOW_ID))
        self.assertFalse(oauth_web._is_uuid("not-a-uuid"))


def kratos_stub(send_response=None, verify_response=None):
    """_kratos_call stub covering the GET-flow / POST-identifier / POST-code / whoami calls."""
    send_response = send_response or (400, {"id": FLOW_ID, "state": "sent_email"})

    def call(url, method, body=None, token=None):
        if url.endswith("/sessions/whoami"):
            return 200, {"active": True, "identity": {"id": USER_ID, "verifiable_addresses": [
                {"via": "email", "verified": True}]}}
        if body and body.get("code"):
            return verify_response if verify_response else (400, {})
        if method == "GET":
            return 200, {"ui": {"action": f"/self-service/login/api?flow={FLOW_ID}"}}
        return send_response

    return call


@unittest.skipUnless(oauth_web, "mcp package not installed")
class KratosLoginTest(unittest.TestCase):
    def test_flow_state_matches_kratos(self):
        # Verified against Kratos v26.2.0 selfservice/flow/state.go (StateEmailSent).
        self.assertEqual(oauth_web._KRATOS_STATE_SENT_EMAIL, "sent_email")

    def test_submit_path_matches_kratos(self):
        # Verified against Kratos v26.2.0 selfservice/flow/login/handler.go
        # (RouteSubmitFlow); /self-service/login/api only accepts GET.
        self.assertEqual(oauth_web._KRATOS_SUBMIT_LOGIN_PATH, "/self-service/login")

    def test_finish_posts_to_the_submit_route(self):
        seen = []

        def call(url, method, body=None, token=None):
            seen.append(url)
            if url.endswith("/sessions/whoami"):
                return 200, {"active": True, "identity": {"id": USER_ID, "verifiable_addresses": [
                    {"via": "email", "verified": True}]}}
            return 200, {"session_token": "sess"}

        with mock.patch.object(oauth_web, "_kratos_call", side_effect=call):
            user_id, error = oauth_web.kratos_login_finish(KRATOS, FLOW_ID, EMAIL, "123456")
        self.assertEqual(user_id, USER_ID)
        self.assertIsNone(error)
        self.assertEqual(seen[0], f"{KRATOS}{oauth_web._KRATOS_SUBMIT_LOGIN_PATH}?flow={FLOW_ID}")

    def test_start_returns_flow_id_when_code_sent(self):
        with mock.patch.object(oauth_web, "_kratos_call", side_effect=kratos_stub()):
            flow_id, error = oauth_web.kratos_login_start(KRATOS, EMAIL)
        self.assertEqual(flow_id, FLOW_ID)
        self.assertIsNone(error)

    def test_start_hides_account_existence_and_logs_reason(self):
        enumeration = "account does not exist or has not setup sign in with code"
        stub = kratos_stub(send_response=(400, {"ui": {"messages": [{"text": enumeration}]}}))
        with mock.patch.object(oauth_web, "_kratos_call", side_effect=stub), \
                self.assertLogs("app.oauth_web", level="WARNING") as captured:
            flow_id, error = oauth_web.kratos_login_start(KRATOS, EMAIL)
        self.assertIsNone(flow_id)
        self.assertIn("We couldn't send a code to that address.", error)
        self.assertNotIn("account does not exist", error)
        self.assertEqual(captured.records[0].reason, enumeration)

    def test_start_reports_outage_when_flow_fetch_fails(self):
        with mock.patch.object(oauth_web, "_kratos_call", return_value=(0, {})):
            flow_id, error = oauth_web.kratos_login_start(KRATOS, EMAIL)
        self.assertIsNone(flow_id)
        self.assertEqual(error, "Sign-in is temporarily unavailable. Try again shortly.")

    def test_finish_rejects_malformed_flow_id_without_calling_kratos(self):
        with mock.patch.object(oauth_web, "_kratos_call") as kratos_call:
            user_id, error = oauth_web.kratos_login_finish(KRATOS, "bad-flow", EMAIL, "123456")
        self.assertIsNone(user_id)
        self.assertIn("expired", error)
        kratos_call.assert_not_called()

    def test_finish_returns_verified_user(self):
        stub = kratos_stub(verify_response=(200, {"session_token": "sess"}))
        with mock.patch.object(oauth_web, "_kratos_call", side_effect=stub):
            user_id, error = oauth_web.kratos_login_finish(KRATOS, FLOW_ID, EMAIL, "123456")
        self.assertEqual(user_id, USER_ID)
        self.assertIsNone(error)

    def test_finish_surfaces_kratos_code_error(self):
        message = "The login code is invalid or has already been used"
        stub = kratos_stub(verify_response=(400, {"ui": {"messages": [{"text": message}]}}))
        with mock.patch.object(oauth_web, "_kratos_call", side_effect=stub):
            user_id, error = oauth_web.kratos_login_finish(KRATOS, FLOW_ID, EMAIL, "000000")
        self.assertIsNone(user_id)
        self.assertEqual(error, message)


class FakeStore:
    def __init__(self, request_id=REQ_ID):
        self._request_id = request_id

    def load_auth_request(self, request_id):
        return SimpleNamespace(id=request_id) if request_id == self._request_id else None


class FakeServer:
    def __init__(self):
        self.routes = []

    def custom_route(self, path, methods):
        def register(handler):
            self.routes.append((path, methods, handler))
            return handler

        return register


def build_client():
    server = FakeServer()
    oauth_web.register_oauth_routes(server, None, FakeStore(), SECRET, KRATOS, PUBLIC_URL)
    routes = [Route(path, handler, methods=list(methods)) for path, methods, handler in server.routes]
    return TestClient(Starlette(routes=routes), follow_redirects=False)


@unittest.skipUnless(oauth_web, "mcp package not installed")
class LoginRouteTest(unittest.TestCase):
    def test_expired_link_is_reported(self):
        response = build_client().get("/login?req=unknown-request")
        self.assertEqual(response.status_code, 200)
        self.assertIn("This sign-in link has expired.", response.text)

    def test_send_code_renders_code_form(self):
        with mock.patch.object(oauth_web, "_kratos_call", side_effect=kratos_stub()):
            response = build_client().post("/login", data={"req": REQ_ID, "email": EMAIL})
        self.assertEqual(response.status_code, 200)
        self.assertIn("We sent a one-time code", response.text)
        self.assertIn(FLOW_ID, response.text)

    def test_send_code_without_email_prompts(self):
        response = build_client().post("/login", data={"req": REQ_ID})
        self.assertIn("Enter your email address.", response.text)

    def test_unknown_account_shows_generic_error(self):
        enumeration = "account does not exist or has not setup sign in with code"
        stub = kratos_stub(send_response=(400, {"ui": {"messages": [{"text": enumeration}]}}))
        with mock.patch.object(oauth_web, "_kratos_call", side_effect=stub):
            response = build_client().post("/login", data={"req": REQ_ID, "email": EMAIL})
        self.assertIn("send a code to that address.", response.text)
        self.assertNotIn("account does not exist", response.text)

    def test_repeated_sends_are_rate_limited(self):
        client = build_client()
        with mock.patch.object(oauth_web, "_kratos_call", side_effect=kratos_stub()):
            for _ in range(oauth_web._LOGIN_SEND_BURST):
                self.assertIn("We sent a one-time code",
                              client.post("/login", data={"req": REQ_ID, "email": EMAIL}).text)
            response = client.post("/login", data={"req": REQ_ID, "email": EMAIL})
        self.assertIn("Too many code requests.", response.text)

    def test_verify_code_sets_cookie_and_redirects_to_consent(self):
        stub = kratos_stub(verify_response=(200, {"session_token": "sess"}))
        with mock.patch.object(oauth_web, "_kratos_call", side_effect=stub):
            response = build_client().post(
                "/login", data={"req": REQ_ID, "flow": FLOW_ID, "email": EMAIL, "code": "123456"})
        self.assertEqual(response.status_code, 303)
        self.assertEqual(response.headers["location"], f"/consent?req={REQ_ID}")
        self.assertEqual(oauth_web.verify_session(SECRET, response.cookies.get(oauth_web.SESSION_COOKIE)),
                         USER_ID)

    def test_wrong_code_rerenders_code_form_with_message(self):
        message = "The login code is invalid or has already been used"
        stub = kratos_stub(verify_response=(400, {"ui": {"messages": [{"text": message}]}}))
        with mock.patch.object(oauth_web, "_kratos_call", side_effect=stub):
            response = build_client().post(
                "/login", data={"req": REQ_ID, "flow": FLOW_ID, "email": EMAIL, "code": "000000"})
        self.assertEqual(response.status_code, 200)
        self.assertIn(message, response.text)
        self.assertIn(FLOW_ID, response.text)


if __name__ == "__main__":
    unittest.main()
