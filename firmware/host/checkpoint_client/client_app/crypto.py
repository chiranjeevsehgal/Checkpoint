"""AES-128-CCM helpers. Mirrors crypto.cpp exactly."""

import hashlib
import hmac
import struct

from cryptography.hazmat.primitives.ciphers.aead import AESCCM

from . import config as cfg


def hkdf_sha256(salt: bytes, ikm: bytes, info: bytes, length: int) -> bytes:
    """RFC 5869 HKDF-SHA256, single-block outputs only. Mirrors crypto.cpp."""
    prk = hmac.new(salt, ikm, hashlib.sha256).digest()
    return hmac.new(prk, info + b"\x01", hashlib.sha256).digest()[:length]


def derive_file_key(session_key: bytes, session_id: int, file_uid: int) -> bytes:
    info = b"checkpoint-file-v1" + struct.pack("<IQ", session_id, file_uid)
    return hkdf_sha256(b"", session_key, info, cfg.CRYPTO_KEY_BYTES)


def build_nonce(session_id: int, file_uid: int, seq: int) -> bytes:
    msg = (b"checkpoint-nonce-v1"
           + struct.pack("<IQH", session_id, file_uid, seq))
    return hashlib.sha256(msg).digest()[:cfg.CRYPTO_NONCE_BYTES]


def decrypt_fragment(key: bytes, session_id: int, file_id: int, seq: int,
                     frag_len: int, ciphertext_and_tag: bytes) -> bytes | None:
    nonce = build_nonce(session_id, file_id, seq)
    aad = struct.pack("<BBHH", cfg.PROTO_VER, cfg.PKT_DATA, seq, frag_len)
    try:
        return AESCCM(key, tag_length=cfg.CRYPTO_TAG_BYTES).decrypt(
            nonce, ciphertext_and_tag, aad)
    except Exception as e:
        print(f"  [!] Decrypt failed for seq={seq}: {e}")
        return None
