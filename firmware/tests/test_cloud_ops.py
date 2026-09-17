"""
Cloud-secret BLE control operations (no hardware).
Run: python -m pytest firmware/tests/test_cloud_ops.py -v
Covers: get-cloud-secret and clear-trusted-slots opcodes, AEAD response
framing, and ACK-before-invalidate ordering for the release wipe.
"""
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"


def read(name):
    return (BASE / name).read_text(encoding="utf-8", errors="ignore")


def test_cloud_control_opcodes():
    header = read("control.h")
    source = read("control.cpp")
    assert "CTRL_CMD_GET_CLOUD_SECRET = 0x23" in header
    assert "CTRL_CMD_CLEAR_TRUSTED_SLOTS = 0x24" in header
    assert "control_send_cloud_secret" in source
    assert "auth_clear_slots" in source
    # ACK before invalidating the session: clear is deferred to the next tick.
    assert "s_clear_slots_pending" in source
    assert "auth_clear_slots();" in source


def test_forget_self_control_opcode():
    header = read("control.h")
    source = read("control.cpp")
    assert "CTRL_CMD_FORGET_SELF = 0x25" in header
    assert "CTRL_CMD_FORGET_SELF" in source
    assert "auth_forget_self" in source
    # ACK before invalidating the session: forget is deferred to the next tick.
    assert "s_forget_self_pending" in source
    assert "auth_forget_self();" in source
    gate = source.split("if (forget_self_req)")[1].split("if (!status_req)")[0]
    assert gate.index("control_send_cmd_resp") < gate.index("s_forget_self_pending = true")


def test_cloud_secret_response_is_aead_framed():
    source = read("control.cpp")
    block = source.split("void control_send_cloud_secret")[1].split("void control_poll")[0]
    assert "crypto_build_cloud_nonce" in block
    assert "crypto_aead_encrypt" in block
    assert "ble_session_id()" in block
    payload_len = "2 + CRYPTO_NONCE_BYTES + AUTH_CLOUD_SECRET_BYTES + CRYPTO_TAG_BYTES"
    assert payload_len in block, "response must be cmd+status+nonce+cipher+tag"


def test_clear_slots_acks_before_clearing():
    source = read("control.cpp")
    gate = source.split("if (clear_slots_req)")[1].split("if (!status_req)")[0]
    ack_pos = gate.index("control_send_cmd_resp")
    pending_pos = gate.index("s_clear_slots_pending = true")
    assert ack_pos < pending_pos
    # The actual wipe runs at the top of the next poll, after the ACK flushed.
    assert "if (s_clear_slots_pending) {" in source
