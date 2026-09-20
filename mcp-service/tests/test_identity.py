import json
import unittest
import urllib.error
from unittest import mock

from app import identity

USER_ID = "00000000-0000-0000-0000-000000000001"


class FakeResponse:
    def __init__(self, payload=None, raw=None):
        self._raw = raw if raw is not None else json.dumps(payload or {}).encode()

    def read(self):
        return self._raw

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


class FetchNameTest(unittest.TestCase):
    def fetch(self, response=None, error=None, user_id=USER_ID, admin_url="http://kratos:4434"):
        with mock.patch.object(identity.urllib.request, "urlopen") as urlopen:
            if error is not None:
                urlopen.side_effect = error
            else:
                urlopen.return_value = response
            return identity.fetch_name(admin_url, user_id), urlopen

    def test_returns_trait_name(self):
        got, _ = self.fetch(FakeResponse({"traits": {"name": "Asha"}}))
        self.assertEqual(got, "Asha")

    def test_empty_admin_url_short_circuits(self):
        got, urlopen = self.fetch(admin_url="")
        self.assertIsNone(got)
        urlopen.assert_not_called()

    def test_invalid_user_id_short_circuits(self):
        got, urlopen = self.fetch(user_id="not-a-uuid")
        self.assertIsNone(got)
        urlopen.assert_not_called()

    def test_blank_name_is_none(self):
        got, _ = self.fetch(FakeResponse({"traits": {"name": "   "}}))
        self.assertIsNone(got)

    def test_missing_traits_is_none(self):
        got, _ = self.fetch(FakeResponse({}))
        self.assertIsNone(got)

    def test_network_error_is_none(self):
        got, _ = self.fetch(error=urllib.error.URLError("down"))
        self.assertIsNone(got)

    def test_malformed_json_is_none(self):
        got, _ = self.fetch(FakeResponse(raw=b"not json"))
        self.assertIsNone(got)


if __name__ == "__main__":
    unittest.main()
