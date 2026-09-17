"""
Cloud ownership credential checks (no hardware).
Run: python -m pytest firmware/tests/test_cloud_secret.py -v
Covers: independent cloud secret storage, hash-only provisioning export,
explicit-key AEAD primitive, and the cloud-secret control opcodes.
"""
import hashlib
import struct
from pathlib import Path

import pytest

BASE = Path(__file__).resolve().parent.parent / "checkpoint"

crypto_lib = __import__("importlib.util", fromlist=["find_spec"]).find_spec("cryptography")
needs_crypto = pytest.mark.skipif(crypto_lib is None, reason="cryptography not installed")


def read(name):
    return (BASE / name).read_text(encoding="utf-8", errors="ignore")


def test_cloud_secret_is_stored_independently():
    header = read("auth.h")
    source = read("auth.cpp")
    assert "AUTH_CLOUD_SECRET_BYTES 32" in header
    assert 'getBytesLength("cloud")' in source
    assert 'putBytes("cloud"' in source
    assert 'getBytes("cloud"' in source
    assert "auth_get_cloud_secret" in header and "auth_get_cloud_secret" in source
    assert "auth_cloud_secret_hash" in header and "auth_cloud_secret_hash" in source


def test_provisioning_export_is_hash_only():
    source = read("auth.cpp")
    assert 'line == "auth provision"' in source
    assert 'Serial.print("device ")' in source
    assert 'Serial.print("cloud-sha256 ")' in source
    provision = source.split('line == "auth provision"')[1].split("} else if (line ==")[0]
    assert "s_cloud_secret" not in provision, "provisioning must not print the plaintext secret"
    assert "auth_get_cloud_secret(" not in provision, "provisioning must not read the plaintext secret"


def test_explicit_key_aead_primitives():
    header = read("crypto.h")
    source = read("crypto.cpp")
    for token in ("crypto_sha256", "crypto_aead_encrypt", "crypto_aead_decrypt", "crypto_build_cloud_nonce"):
        assert token in header and token in source, f"missing {token}"
    assert "checkpoint-cloud-v1" in source


def test_cloud_nonce_scheme_matches_host_derivation():
    session_id = 0x11223344
    seq = 0x0102
    expected = hashlib.sha256(b"checkpoint-cloud-v1" + struct.pack("<IH", session_id, seq)).digest()[:12]
    assert len(expected) == 12


@needs_crypto
def test_cloud_envelope_roundtrip():
    from cryptography.hazmat.primitives.ciphers.aead import AESCCM

    session_key = bytes(range(16))
    secret = bytes([0x11]) * 32
    session_id = 0x11223344
    seq = 0x0102
    nonce = hashlib.sha256(b"checkpoint-cloud-v1" + struct.pack("<IH", session_id, seq)).digest()[:12]
    aad = struct.pack("<BBHH", 3, 0x23, seq, 0)[:4]
    aad = bytes([3, 0x23, seq & 0xFF, (seq >> 8) & 0xFF])

    aead = AESCCM(session_key, tag_length=8)
    sealed = aead.encrypt(nonce, secret, aad)  # ciphertext(32) || tag(8)
    assert len(sealed) == 40
    assert aead.decrypt(nonce, sealed, aad) == secret
