import asyncio
import unittest
from unittest import mock

try:
    from app import oauth as oauth_module
    from app.oauth import CheckpointOAuthProvider
except ModuleNotFoundError:  # pragma: no cover - mcp/ML deps absent
    oauth_module = None
    CheckpointOAuthProvider = None


class FakeStore:
    def load_token(self, token_hash, kind):
        return None

    def get_client_metadata(self, client_id):
        return None


@unittest.skipUnless(CheckpointOAuthProvider, "mcp package not installed")
class CheckpointOAuthProviderTest(unittest.TestCase):
    def provider(self):
        return CheckpointOAuthProvider(FakeStore(), "https://mcp.example", "dsn", {"claude.ai"}, 3600, 2592000)

    def test_redirect_host_allowlist(self):
        provider = self.provider()
        self.assertTrue(provider.redirect_allowed("https://claude.ai/api/mcp/auth_callback"))
        self.assertTrue(provider.redirect_allowed("http://127.0.0.1:3118/callback"))
        self.assertFalse(provider.redirect_allowed("https://evil.example/callback"))

    def test_key_token_resolves_to_user(self):
        provider = self.provider()
        with mock.patch.object(oauth_module, "resolve_key_user", return_value="user-9"):
            token = asyncio.run(provider.load_access_token("cp_mcp_abc"))
        self.assertIsNotNone(token)
        self.assertEqual(token.client_id, "user-9")

    def test_unknown_token_is_rejected(self):
        provider = self.provider()
        self.assertIsNone(asyncio.run(provider.load_access_token("random-value")))


if __name__ == "__main__":
    unittest.main()
