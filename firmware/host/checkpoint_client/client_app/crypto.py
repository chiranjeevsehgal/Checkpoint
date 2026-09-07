"""AES-128-CCM helpers. Mirrors crypto.cpp exactly."""

import hashlib
import struct

from cryptography.hazmat.primitives.ciphers.aead import AESCCM

from . import config as cfg


def derive_file_key(master_key: bytes, session_id: int, file_id: int) -> bytes:
    prk = hashlib.sha256(master_key).digest()
    info = struct.pack("<II", session_id, file_id)
    return hashlib.sha256(prk + info + b"\x01").digest()[:cfg.CRYPTO_KEY_BYTES]


def build_nonce(session_id: int, file_id: int, seq: int) -> bytes:
    return struct.pack("<IIH", session_id, file_id, seq) + b"\xA5\x5A"


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
