import unittest

from app import oauth_session


class OAuthSessionTest(unittest.TestCase):
    secret = "test-secret"

    def test_round_trip_returns_user(self):
        value = oauth_session.sign_session(self.secret, "user-1")
        self.assertEqual(oauth_session.verify_session(self.secret, value), "user-1")

    def test_wrong_secret_is_rejected(self):
        value = oauth_session.sign_session(self.secret, "user-1")
        self.assertIsNone(oauth_session.verify_session("other-secret", value))

    def test_expired_session_is_rejected(self):
        value = oauth_session.sign_session(self.secret, "user-1", ttl_seconds=-1)
        self.assertIsNone(oauth_session.verify_session(self.secret, value))

    def test_malformed_values_are_rejected(self):
        self.assertIsNone(oauth_session.verify_session(self.secret, "a.b"))
        self.assertIsNone(oauth_session.verify_session(self.secret, None))

    def test_csrf_binds_to_request(self):
        token = oauth_session.csrf_token(self.secret, "req-1")
        self.assertTrue(oauth_session.verify_csrf(self.secret, "req-1", token))
        self.assertFalse(oauth_session.verify_csrf(self.secret, "req-2", token))
        self.assertFalse(oauth_session.verify_csrf(self.secret, "req-1", None))


if __name__ == "__main__":
    unittest.main()
