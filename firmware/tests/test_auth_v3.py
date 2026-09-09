"""
Protocol v3 acceptance checks (no hardware).
Run: python -m pytest firmware/tests/test_auth_v3.py -v
Covers: key-transport removal, challenge-response presence, 2-slot NVS,
central auth gate, enrollment LED/button, USB recovery, v3 KDF vectors,
replay resistance (distinct nonces -> distinct session keys).
"""
import struct
from pathlib import Path

import pytest

BASE = Path(__file__).resolve().parent.parent / "checkpoint"
HOST = Path(__file__).resolve().parent.parent / "host" / "checkpoint_client"

bleak = __import__("importlib.util", fromlist=["find_spec"]).find_spec("bleak")
crypto_lib = __import__("importlib.util", fromlist=["find_spec"]).find_spec("cryptography")
needs_deps = pytest.mark.skipif(
    bleak is None or crypto_lib is None, reason="bleak/cryptography not installed")


def read(name):
    return (BASE / name).read_text(encoding="utf-8", errors="ignore")


def test_no_session_key_in_hello_ack():
    ble = read("ble_service.cpp")
    assert "payload[11:27]" not in ble and "payload + 11, sess_key" not in ble
    assert "memcpy(payload + 27, s_device_nonce" in ble
    assert "sizeof(payload), resp" in ble or "sizeof(payload)" in ble
    print("PASS no key in HELLO_ACK")


def test_challenge_response_present():
    ble = read("ble_service.cpp")
    for token in ("handle_hello", "handle_auth", "handle_ready",
                  "auth_begin", "auth_verify_client_proof",
                  "auth_verify_client_finish", "PKT_AUTH_OK", "PKT_READY_ACK",
                  "send_ready_ack"):
        assert token in ble, f"missing {token}"
    auth = read("auth.cpp")
    for token in ("ckauth", '"c%d_id"', '"c%d_key"',
                  "crypto_verify_hmac_ct", "checkpoint-auth-v3",
                  "checkpoint-server-finish-v3", "checkpoint-client-finish-v3"):
        assert token in auth, f"auth.cpp missing {token}"
    cry = read("crypto.cpp")
    for token in ("checkpoint-session-v3", "checkpoint-client-v3"):
        assert token in cry, f"crypto.cpp missing {token}"
    print("PASS challenge-response present")


def test_two_slot_limit_and_commit_after_ready():
    auth = read("auth.cpp")
    assert "AUTH_MAX_CLIENTS" in read("auth.h")
    assert "free_slot() < 0" in auth, "enroll must reject when 2/2 full"
    assert "persist_slot" in auth
    # Pending enroll key committed only inside verify_client_finish.
    finish = auth.split("auth_verify_client_finish")[1]
    assert "persist_slot" in finish
    assert "auth_close_enrollment" in finish
    print("PASS slots + commit-after-ready")


def test_central_auth_gate():
    ble = read("ble_service.cpp")
    assert "if (!auth_is_authenticated())" in ble
    assert "send_error_locked(0x03)" in ble
    tr = read("transfer.cpp")
    assert "ble_peer_authenticated" in tr
    assert tr.count("ble_peer_authenticated") >= 3
    ctl = read("control.cpp")
    assert "ble_peer_authenticated" in ctl
    print("PASS central gate")


def test_gatt_hardened():
    ble = read("ble_service.cpp")
    assert "WRITE_ENC" in ble
    assert "WRITE | NIMBLE_PROPERTY::INDICATE | NIMBLE_PROPERTY::READ" not in ble
    assert "NOTIFY | NIMBLE_PROPERTY::READ" not in ble
    print("PASS gatt")


def test_button_and_led_and_usb():
    ui = read("ui.cpp")
    assert "UI_LONG_PRESS_MS" in ui
    assert "auth_open_enrollment" in ui
    assert "OVERLAY_ENROLL" in ui and "OVERLAY_AUTH_OK" in ui
    assert "LED_ENROLL" not in ui and "LED_AUTH_OK" not in ui, \
        "enrollment must be an overlay, not a base LED state"
    cfg = read("config.h")
    assert "UI_LONG_PRESS_MS 5000" in cfg
    assert "AUTH_ENROLL_WINDOW_MS 60000" in cfg
    assert "PROTO_VER 3" in cfg
    auth = read("auth.cpp")
    for cmd in ("auth list", "auth forget ", "auth reset", "auth export"):
        assert cmd in auth, f"USB recovery missing {cmd}"
    assert "keep this secret" in auth
    print("PASS button/led/usb")


@needs_deps
def test_v3_kdf_replay_resistance():
    import sys
    sys.path.insert(0, str(HOST))
    from client_app.crypto import (
        build_transcript,
        client_proof,
        derive_client_key_v3,
        derive_session_key_v3,
    )
    claim = bytes(range(32))
    dev = bytes(16)
    cli = bytes(range(16, 32))
    sess = 0xAABBCCDD
    t1 = build_transcript(dev, cli, sess, bytes(range(16)), bytes(range(16, 32)), 0)
    t2 = build_transcript(dev, cli, sess, bytes(range(1, 17)), bytes(range(16, 32)), 0)
    assert t1 != t2
    key = derive_client_key_v3(claim, bytes(range(16)), bytes(range(16, 32)), dev, cli)
    p1 = client_proof(key, t1)
    p2 = client_proof(key, t2)
    assert p1 != p2, "replayed proof must not verify under a fresh nonce"
    k1 = derive_session_key_v3(key, bytes(range(16)), bytes(range(16, 32)), dev, cli, sess)
    k2 = derive_session_key_v3(key, bytes(range(1, 17)), bytes(range(16, 32)), dev, cli, sess)
    assert k1 != k2, "reconnect must yield a different session key"
    # Mode bit binds enrollment vs normal.
    tn = build_transcript(dev, cli, sess, bytes(range(16)), bytes(range(16, 32)), 0)
    te = build_transcript(dev, cli, sess, bytes(range(16)), bytes(range(16, 32)), 1)
    assert client_proof(key, tn) != client_proof(key, te)


@needs_deps
def test_v3_wire_lengths():
    import sys
    sys.path.insert(0, str(HOST))
    from client_app import config as cfg
    assert cfg.PROTO_VER == 3
    assert cfg.PKT_AUTH == 0x03 and cfg.PKT_AUTH_OK == 0x04 and cfg.PKT_READY_ACK == 0x05
    hello = bytes([0])
    assert len(hello) == 1
    auth_p = bytes(16) + bytes(16) + bytes(32)
    assert len(auth_p) == 64
    ready = struct.pack("<I", 1) + bytes(32)
    assert len(ready) == 36
    ble = read("ble_service.cpp")
    assert "hello.len != 1" in ble, "HELLO must be exactly flags[1]"
    assert "pkt.len != 64" in ble, "AUTH must be exactly 64 bytes"
    assert "ready.len != 36" in ble, "READY must be exactly 36 bytes"


def test_ready_idempotent_and_session_zeroized():
    ble = read("ble_service.cpp")
    assert "if (s_handshaked && auth_is_authenticated())" in ble, \
        "duplicate READY on a live session must resend READY_ACK"
    auth = read("auth.cpp")
    assert "memset(s_session_key, 0, sizeof(s_session_key))" in auth, \
        "disconnect must zero the session key"
    assert "s_has_session_key = false" in auth
