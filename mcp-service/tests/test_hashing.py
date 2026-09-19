import hashlib
import unittest

from app import hashing


class HashingTest(unittest.TestCase):
    def test_hash_secret_matches_sha256(self):
        self.assertEqual(hashing.hash_secret("cp_mcp_abc"), hashlib.sha256(b"cp_mcp_abc").digest())

    def test_parse_bearer_extracts_key(self):
        self.assertEqual(hashing.parse_bearer("Bearer cp_mcp_abc"), "cp_mcp_abc")

    def test_parse_bearer_rejects_foreign_tokens(self):
        self.assertIsNone(hashing.parse_bearer("Bearer not-a-key"))
        self.assertIsNone(hashing.parse_bearer("cp_mcp_abc"))
        self.assertIsNone(hashing.parse_bearer(None))


if __name__ == "__main__":
    unittest.main()
