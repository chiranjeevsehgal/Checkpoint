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


AUTH_DOM = b"checkpoint-auth-v3"
SERVER_DOM = b"checkpoint-server-finish-v3"
CLIENT_DOM = b"checkpoint-client-finish-v3"
SESSION_INFO = b"checkpoint-session-v3"
ENROLL_INFO = b"checkpoint-client-v3"


def build_transcript(device_id: bytes, client_id: bytes, session_id: int,
                     device_nonce: bytes, client_nonce: bytes, mode: int) -> bytes:
    return (AUTH_DOM + bytes(device_id) + bytes(client_id)
            + struct.pack("<I", session_id)
            + bytes(device_nonce) + bytes(client_nonce) + bytes([mode & 0xFF]))


def derive_session_key_v3(client_key: bytes, device_nonce: bytes, client_nonce: bytes,
                          device_id: bytes, client_id: bytes, session_id: int) -> bytes:
    salt = bytes(device_nonce) + bytes(client_nonce)
    info = SESSION_INFO + bytes(device_id) + bytes(client_id) + struct.pack("<I", session_id)
    return hkdf_sha256(salt, bytes(client_key), info, cfg.CRYPTO_KEY_BYTES)


def derive_client_key_v3(claim_key: bytes, device_nonce: bytes, client_nonce: bytes,
                         device_id: bytes, client_id: bytes) -> bytes:
    salt = bytes(device_nonce) + bytes(client_nonce)
    info = ENROLL_INFO + bytes(device_id) + bytes(client_id)
    return hkdf_sha256(salt, bytes(claim_key), info, 32)


def client_proof(client_key: bytes, transcript: bytes) -> bytes:
    return hmac.new(bytes(client_key), bytes(transcript), hashlib.sha256).digest()


def finish_proof(session_key: bytes, domain: bytes, transcript: bytes) -> bytes:
    return hmac.new(bytes(session_key), bytes(domain) + bytes(transcript), hashlib.sha256).digest()


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
