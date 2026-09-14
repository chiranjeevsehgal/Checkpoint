"""
Regression tests for the clean client_app package.
Run: python -m pytest firmware/tests/test_client_app.py -v
Covers: BLE wire format, crypto vectors, response parsers, bench schema
+ math, ui_events mapping (headless fakes), worker write serialization.
No BLE hardware, display, or torch needed.
"""
import hashlib
import importlib.util
import struct
import sys
import zlib
from pathlib import Path

import pytest

HOST = Path(__file__).resolve().parent.parent / "host" / "checkpoint_client"
sys.path.insert(0, str(HOST))

bleak = importlib.util.find_spec("bleak")
crypto_lib = importlib.util.find_spec("cryptography")
needs_deps = pytest.mark.skipif(
    bleak is None or crypto_lib is None, reason="bleak/cryptography not installed")


@needs_deps
def test_proto_roundtrip_and_rejects():
    from client_app.protocol import proto_build, proto_parse
    pkt = proto_build(0x12, 300, b"hello")
    parsed = proto_parse(pkt)
    assert (parsed.type, parsed.seq, parsed.payload) == (0x12, 300, b"hello")
    assert proto_parse(b"\x01\x02") is None  # too short
    bad = bytearray(proto_build(0x01, 1, b"x"))
    bad[-1] ^= 0xFF
    assert proto_parse(bytes(bad)) is None  # crc mismatch


@needs_deps
def test_wire_format_vectors():
    """Independent vectors for the firmware-mirroring byte layouts."""
    from client_app import config as cfg
    from client_app.crypto import build_nonce, derive_file_key
    from client_app.protocol import proto_build, proto_parse
    header = struct.pack("<BBHH", cfg.PROTO_VER, 0x12, 300, 5)
    expected = header + b"hello" + struct.pack("<I", zlib.crc32(header + b"hello") & 0xFFFFFFFF)
    assert proto_build(0x12, 300, b"hello") == expected
    assert proto_parse(expected).payload == b"hello"
    import hmac as _hmac
    prk = _hmac.new(b"", b"0" * 16, hashlib.sha256).digest()
    info = b"checkpoint-file-v1" + struct.pack("<IQ", 1, 2)
    assert derive_file_key(b"0" * 16, 1, 2) == \
        _hmac.new(prk, info + b"\x01", hashlib.sha256).digest()[:16]
    assert build_nonce(9, 0x1234ABCD12345678, 5) == \
        hashlib.sha256(b"checkpoint-nonce-v1"
                       + struct.pack("<IQH", 9, 0x1234ABCD12345678, 5)).digest()[:12]
    assert cfg.BENCH_FIELDNAMES == [
        "ts", "file_id", "total_bytes", "total_frags", "mtu", "frag_size",
        "goodput_kBps", "median_rtt_ms", "p95_rtt_ms", "duplicates", "retries",
        "decrypt_fail", "crc_ok", "resume_from", "elapsed_s",
        "ingest_upload_id", "ingest_status", "ingest_error",
        "vad_status", "vad_speech_s"]
    assert cfg.OUTPUT_DIR.name == "received"
    assert cfg.OUTPUT_DIR.parent.name == "checkpoint_client"


@needs_deps
def test_aes_ccm_decrypt_roundtrip():
    from cryptography.hazmat.primitives.ciphers.aead import AESCCM

    from client_app import config as cfg
    from client_app.crypto import build_nonce, decrypt_fragment, derive_file_key
    key = derive_file_key(b"K" * 16, 9, 0x1234ABCD)
    nonce = build_nonce(9, 0x1234ABCD, 5)
    aad = struct.pack("<BBHH", cfg.PROTO_VER, cfg.PKT_DATA, 5, 4)
    ct = AESCCM(key, tag_length=cfg.CRYPTO_TAG_BYTES).encrypt(nonce, b"test", aad)
    assert decrypt_fragment(key, 9, 0x1234ABCD, 5, 4, ct) == b"test"
    assert decrypt_fragment(key, 9, 0x1234ABCD, 5, 4, b"bad!tag!") is None


@needs_deps
def test_response_parsers():
    from client_app.ble_client import CheckpointClient
    status = CheckpointClient.parse_status(
        bytes([1, 1, 0, 0, 30, 225, 10, 3, 0, 44, 0, 0, 0, 5, 0, 0, 1]))
    assert status["recording"] is True
    assert status["brightness"] == 30
    assert status["sync"] is True
    assert CheckpointClient.parse_status(bytes(16))["sync"] is True  # legacy default
    assert CheckpointClient.parse_status(b"short") == {}
    assert CheckpointClient.parse_storage(struct.pack("<QQHH", 10**9, 2 * 10**8, 12, 3)) == \
        {"total": 10**9, "used": 2 * 10**8, "files": 12, "pending": 3}
    entry = struct.pack("<B", 10) + b"/rec/a.gg!" + struct.pack("<IB", 100, 1)
    listed = CheckpointClient.parse_file_list(struct.pack("<HHB", 0, 1, 1) + entry)
    assert listed["entries"] == [{"name": "/rec/a.gg!", "size": 100, "flags": 1}]


@needs_deps
def test_bench_finalize_math_and_schema(tmp_path):
    from client_app import config as cfg
    from client_app.bench import BenchRecorder
    rec = BenchRecorder(None)
    assert rec.finalize(True, 100, 247, 220) is None  # no active file
    rec.reset_file(0x1234ABCD, 1000, 5, 0)
    row = rec.finalize(True, 1000, 247, 220)
    assert row["file_id"] == "000000001234abcd"
    assert list(row.keys()) == cfg.BENCH_FIELDNAMES
    assert row["median_rtt_ms"] == row["p95_rtt_ms"]  # single sample
    csv_path = tmp_path / "bench.csv"
    rec2 = BenchRecorder(csv_path)
    rec2.reset_file(0x1, 10, 1, 0)
    rec2.finalize(False, 10, 0, 220, ingest_status="pending", vad_status="pending")
    rec2.update_ingest("0000000000000001", "u1", "SUBMITTED")
    assert rec2.rows()[0]["ingest_status"] == "SUBMITTED"
    rec2.rewrite_csv()
    assert "SUBMITTED" in csv_path.read_text()
    rec2.close()


class _FakeTree:
    def __init__(self):
        self.rows = {}

    def exists(self, iid):
        return iid in self.rows

    def insert(self, *a, iid=None, text=None, values=None, **kw):
        self.rows[iid] = list(values)

    def item(self, iid, what=None, values=None):
        if values is not None:
            self.rows[iid] = list(values)
        return tuple(self.rows[iid])

    def get_children(self):
        return list(self.rows)

    def delete(self, iid):
        self.rows.pop(iid, None)

    def selection(self):
        return []


class _FakeVar:
    def __init__(self, value=None):
        self.value = value

    def set(self, value):
        self.value = value

    def get(self):
        return self.value


class _FakeLabel:
    def __init__(self):
        self.text = ""

    def configure(self, text=None, **kw):
        self.text = text


def _refs():
    from client_app.ui_events import ListState, UiRefs
    logs = []
    refreshed = []
    refs = UiRefs(
        tree=_FakeTree(), prog={}, prog_label=_FakeLabel(),
        dev_status_var=_FakeVar(), led_muted_var=_FakeVar(False),
        bright_var=_FakeVar(30), sync_var=_FakeVar(True),
        storage_var=_FakeVar(), storage_bar={}, dev_tree=_FakeTree(),
        list_page_var=_FakeVar(), list_state=ListState(),
        log=logs.append, storage_refresh=lambda: refreshed.append(True))
    return refs, logs, refreshed


def test_transfer_events():
    from client_app.ui_events import apply_event
    refs, _logs, _ref = _refs()
    apply_event(refs, {"type": "announce", "file_id": "ab12",
                       "total_bytes": 100, "total_frags": 4})
    assert refs.tree.rows["ab12"][1] == "0%"
    apply_event(refs, {"type": "progress", "file_id": "ab12",
                       "received": 2, "total_frags": 4})
    assert refs.tree.rows["ab12"][1] == "50% (2/4)"
    assert refs.prog["value"] == 50
    apply_event(refs, {"type": "vad", "file_id": "ab12",
                       "vad_status": "speech", "vad_speech_s": 2.5})
    assert refs.tree.rows["ab12"][2] == "speech 2.5s"
    apply_event(refs, {"type": "ingest", "file_id": "ab12", "upload_id": "u12345678",
                       "ingest_status": "SUBMITTED", "ingest_error": "",
                       "vad_status": "speech", "vad_speech_s": "2.50"})
    assert refs.tree.rows["ab12"][3].startswith("SUBMITTED")
    apply_event(refs, {"type": "file_done", "file_id": "ab12",
                       "crc_ok": True, "total_bytes": 100})
    assert refs.tree.rows["ab12"][1] == "ok"
    assert refs.prog["value"] == 100


def test_device_and_storage_events():
    from client_app.ui_events import apply_event
    refs, logs, refreshed = _refs()
    apply_event(refs, {"type": "rec_status", "recording": True, "vad_active": False,
                       "vad_speech": False, "muted": True, "brightness": 40,
                       "level_dbfs": -20, "pending": 2, "chunks": 9,
                       "utterances": 1, "sync": False})
    assert "rec: ON" in refs.dev_status_var.value
    assert refs.led_muted_var.value is True
    assert refs.bright_var.value == 40
    assert refs.sync_var.value is False
    apply_event(refs, {"type": "cmd_resp", "cmd": 0x10, "status": 0,
                       "muted": False, "brightness": 60})
    assert any("cmd=0x10" in line for line in logs)
    assert refs.bright_var.value == 60
    apply_event(refs, {"type": "cmd_resp", "cmd": 0x21, "status": 0, "removed": 3})
    assert refreshed == [True]
    apply_event(refs, {"type": "storage", "total": 1000, "used": 250,
                       "files": 4, "pending": 1})
    assert "25%" in refs.storage_var.value
    assert refs.storage_bar["value"] == 25
    apply_event(refs, {"type": "file_list", "start": 0, "total": 2, "entries": [
        {"name": "a.ogg", "size": 1536, "flags": 1},
        {"name": "b.ogg", "size": 100, "flags": 0}]})
    assert refs.dev_tree.rows["a.ogg"][1] == "pending"
    assert refs.dev_tree.rows["b.ogg"][1] == "synced"
    assert refs.list_page_var.value == "1–2 of 2"


@needs_deps
def test_reconnect_uses_stored_client_id(monkeypatch):
    """Blocker 1: HELLO carries no identity; AUTH must use the stored id.

    Simulates a normal reconnect: the client starts with a fresh random id,
    learns device_id from HELLO_ACK, loads the stored credential, and must
    send the STORED client_id in AUTH (the firmware never saw the random one).
    """
    import asyncio
    import struct

    import client_app.credentials as credsmod
    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient
    from client_app.crypto import (
        SERVER_DOM,
        build_transcript,
        derive_session_key_v3,
        finish_proof,
    )
    from client_app.protocol import proto_build, proto_parse

    device_id = bytes(range(16))
    device_nonce = bytes(range(64, 80))
    stored_id = bytes(range(16, 32))
    stored_key = bytes(range(32, 64))
    session = 0xAABBCCDD
    monkeypatch.setattr(
        credsmod, "load_client_credential",
        lambda dev: (stored_id, stored_key) if bytes(dev) == device_id else None)

    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    initial_random = bytes(client.client_id)
    assert initial_random != stored_id
    writes = []

    async def fake_write(ptype, seq, payload=b""):
        writes.append((ptype, bytes(payload)))

    client.write_ctrl = fake_write
    hello_ack = (struct.pack("<B", cfg.PROTO_VER) + struct.pack("<I", session)
                 + struct.pack("<H", 247) + struct.pack("<I", 50)
                 + device_id + device_nonce + bytes([0]))
    assert client._parse_hello_ack(hello_ack) is True

    async def run():
        await client._auth_exchange()

    async def driver():
        for _ in range(500):
            await asyncio.sleep(0.01)
            auth_writes = [w for w in writes if w[0] == cfg.PKT_AUTH]
            if auth_writes:
                break
        assert auth_writes, "AUTH was never sent"
        auth_payload = auth_writes[0][1]
        assert len(auth_payload) == 64
        assert auth_payload[:16] == stored_id
        assert auth_payload[:16] != initial_random
        client_nonce = auth_payload[16:32]
        transcript = build_transcript(device_id, stored_id, session,
                                      device_nonce, client_nonce, 0)
        expect_session = derive_session_key_v3(stored_key, device_nonce, client_nonce,
                                               device_id, stored_id, session)
        server_proof = finish_proof(expect_session, SERVER_DOM, transcript)
        await client._handle_ctrl_packet(
            proto_parse(proto_build(cfg.PKT_AUTH_OK, 1, server_proof)))

    async def both():
        await asyncio.gather(run(), driver())

    asyncio.run(both())
    expect_session = derive_session_key_v3(
        stored_key, device_nonce, client.client_nonce,
        device_id, stored_id, session)
    assert client.session_key == expect_session


@needs_deps
def test_ready_error_does_not_save_credentials(monkeypatch):
    """Blocker 2: a device ERROR during READY must fail, never save."""
    import asyncio

    import client_app.credentials as credsmod
    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient

    saved = []
    deleted = []
    monkeypatch.setattr(credsmod, "save_client_credential",
                        lambda dev, cid, ckey, pending=False: saved.append((bytes(dev), bytes(cid), bytes(ckey))))
    monkeypatch.setattr(credsmod, "delete_credential",
                        lambda dev: deleted.append(bytes(dev)))
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    client.enroll = True

    async def fake_hello_exchange(label):
        client.session_id = 0x1234ABCD
        client.device_id = bytes(16)
        client.device_nonce = bytes(16)
        client.auth_mode = 1
        client.hello_acked.set()
        return "acked"

    async def fake_auth_exchange():
        client.session_key = b"S" * 16
        client._auth_transcript = b"T" * 88
        client._pending_enroll_key = b"E" * 32

    async def fake_write_ctrl(ptype, seq, payload=b""):
        if ptype == cfg.PKT_READY:
            client.last_error = 0x03
            client.error_event.set()

    client._hello_exchange = fake_hello_exchange
    client._auth_exchange = fake_auth_exchange
    client.write_ctrl = fake_write_ctrl
    with pytest.raises(RuntimeError, match="READY rejected"):
        asyncio.run(client.do_handshake())
    assert saved == [], "rejected READY must not persist an enrollment"
    assert deleted == [bytes(16)], "rejected READY must drop the pending enrollment"


@needs_deps
def test_ready_retry_on_dropped_ack(monkeypatch):
    """Issue 4 (host): a lost READY_ACK is retried, then succeeds."""
    import asyncio
    import struct

    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient
    from client_app.protocol import proto_build, proto_parse

    monkeypatch.setattr(cfg, "ACK_TIMEOUT_S", 0.2)
    try:
        client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
        ready_sends = []

        async def fake_hello_exchange(label):
            client.session_id = 0x1234ABCD
            client.device_id = bytes(16)
            client.device_nonce = bytes(16)
            client.hello_acked.set()
            return "acked"

        async def fake_auth_exchange():
            client.session_key = b"S" * 16
            client._auth_transcript = b"T" * 88

        async def fake_write_ctrl(ptype, seq, payload=b""):
            if ptype == cfg.PKT_READY:
                ready_sends.append(bytes(payload))
                if len(ready_sends) == 1:
                    return  # READY_ACK lost on the way back
                pkt = proto_parse(proto_build(
                    cfg.PKT_READY_ACK, 1, struct.pack("<I", 0x1234ABCD)))
                await client._handle_ctrl_packet(pkt)

        client._hello_exchange = fake_hello_exchange
        client._auth_exchange = fake_auth_exchange
        client.write_ctrl = fake_write_ctrl
        asyncio.run(client.do_handshake())
        assert len(ready_sends) == 2, "one drop must cause exactly one retry"
        assert all(len(p) == 36 for p in ready_sends)
    finally:
        monkeypatch.undo()


@needs_deps
def test_enroll_clears_enroll_mode(monkeypatch):
    """Blocker 3: after a validated READY_ACK the client is a normal client."""
    import asyncio
    import struct

    import client_app.credentials as credsmod
    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient
    from client_app.protocol import proto_build, proto_parse

    saved = []
    marked = []
    monkeypatch.setattr(credsmod, "save_client_credential",
                        lambda dev, cid, ckey, pending=False: saved.append((bytes(dev), bytes(cid), bytes(ckey))))
    monkeypatch.setattr(credsmod, "mark_active",
                        lambda dev: marked.append(bytes(dev)))
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False,
                              claim_key=bytes(32), enroll=True)

    async def fake_hello_exchange(label):
        client.session_id = 0x1234ABCD
        client.device_id = bytes(16)
        client.device_nonce = bytes(16)
        client.auth_mode = 1
        client.hello_acked.set()
        return "acked"

    async def fake_auth_exchange():
        client.session_key = b"S" * 16
        client._auth_transcript = b"T" * 88
        client._pending_enroll_key = b"E" * 32

    async def fake_write_ctrl(ptype, seq, payload=b""):
        if ptype == cfg.PKT_READY:
            pkt = proto_parse(proto_build(
                cfg.PKT_READY_ACK, 1, struct.pack("<I", 0x1234ABCD)))
            await client._handle_ctrl_packet(pkt)

    client._hello_exchange = fake_hello_exchange
    client._auth_exchange = fake_auth_exchange
    client.write_ctrl = fake_write_ctrl
    asyncio.run(client.do_handshake())
    assert saved == [], "activation must not re-save"
    assert marked == [bytes(16)]
    assert client.client_key == b"E" * 32
    assert client.enroll is False
    assert client.claim_key is None


@needs_deps
def test_parse_claim_hex_rejects_bad_lengths():
    from client_app.ble_client import parse_claim_hex
    assert parse_claim_hex("ab" * 32) == bytes.fromhex("ab" * 32)
    uri = "checkpoint://claim?device=" + "cd" * 16 + "&key=" + "ab" * 32
    assert parse_claim_hex(uri) == bytes.fromhex("ab" * 32)
    assert parse_claim_hex("uri " + uri) == bytes.fromhex("ab" * 32)
    assert parse_claim_hex("  " + "ab" * 32 + "\n") == bytes.fromhex("ab" * 32)
    for bad in ["00", "ab" * 16, "ab" * 31, "ab" * 33, "zz" * 32, "", "  "]:
        try:
            parse_claim_hex(bad)
            raised = None
        except RuntimeError as e:
            raised = e
        assert raised is not None, f"claim {bad!r} must be rejected"
    try:
        parse_claim_hex("ab" * 16)
        device_hint = None
    except RuntimeError as e:
        device_hint = e
    assert device_hint is not None and "device id" in str(device_hint)


@needs_deps
def test_ready_ack_validates_session():
    import asyncio
    import struct

    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient
    from client_app.protocol import proto_build, proto_parse

    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    client.session_id = 0x1234ABCD

    async def run(payload_bytes, session):
        client.ready_ack_event.clear()
        client.error_event.clear()
        client.last_error = None
        client.session_id = session
        await client._handle_ctrl_packet(
            proto_parse(proto_build(cfg.PKT_READY_ACK, 1, payload_bytes)))

    asyncio.run(run(b"short", 0x1234ABCD))
    assert client.error_event.is_set() and not client.ready_ack_event.is_set()
    asyncio.run(run(struct.pack("<I", 0xDEADBEEF), 0x1234ABCD))
    assert client.error_event.is_set() and not client.ready_ack_event.is_set()
    asyncio.run(run(struct.pack("<I", 0x1234ABCD), 0x1234ABCD))
    assert client.ready_ack_event.is_set() and not client.error_event.is_set()


@needs_deps
def test_transcript_byte_exact():
    """Issue 11 (host half): transcript layout must match firmware auth.cpp."""
    import struct

    from client_app.crypto import build_transcript
    dev = bytes(range(16))
    cli = bytes(range(16, 32))
    dn = bytes(range(32, 48))
    cn = bytes(range(48, 64))
    sess, mode = 0x12345678, 1
    expect = (b"checkpoint-auth-v3" + dev + cli + struct.pack("<I", sess)
              + dn + cn + bytes([mode]))
    assert build_transcript(dev, cli, sess, dn, cn, mode) == expect
    assert len(expect) == 87


@needs_deps
def test_v3_auth_transcript_and_proofs():
    from client_app.crypto import (
        build_transcript,
        client_proof,
        derive_client_key_v3,
        derive_session_key_v3,
        finish_proof,
    )
    dev = bytes(range(16))
    cli = bytes(range(16, 32))
    dn = bytes(range(32, 48))
    cn = bytes(range(48, 64))
    sess = 0x12345678
    claim = bytes(range(32))
    t = build_transcript(dev, cli, sess, dn, cn, 1)
    assert t.startswith(b"checkpoint-auth-v3")
    assert len(t) == 18 + 16 + 16 + 4 + 16 + 16 + 1
    ckey = derive_client_key_v3(claim, dn, cn, dev, cli)
    assert len(ckey) == 32
    ksess = derive_session_key_v3(ckey, dn, cn, dev, cli, sess)
    assert len(ksess) == 16
    proof = client_proof(ckey, t)
    assert proof == client_proof(ckey, t)
    assert proof != client_proof(bytes(32), t)
    srv = finish_proof(ksess, b"checkpoint-server-finish-v3", t)
    cli_fin = finish_proof(ksess, b"checkpoint-client-finish-v3", t)
    assert srv != cli_fin
    # Tampering any transcript field invalidates the proof.
    bad = bytearray(t)
    bad[30] ^= 0xFF
    assert client_proof(ckey, bytes(bad)) != proof
    # Different nonces give different session keys.
    ksess2 = derive_session_key_v3(ckey, bytes(16), cn, dev, cli, sess)
    assert ksess2 != ksess


def _make_fake_cred_store(monkeypatch):
    import client_app.credentials as credsmod
    store = {}

    def fake_save(dev, cid, ckey, pending=False):
        store[bytes(dev)] = (bytes(cid), bytes(ckey), bool(pending))

    def fake_load(dev):
        rec = store.get(bytes(dev))
        return (rec[0], rec[1]) if rec else None

    def fake_pending(dev):
        rec = store.get(bytes(dev))
        return bool(rec and rec[2])

    def fake_active(dev):
        rec = store.get(bytes(dev))
        if rec:
            store[bytes(dev)] = (rec[0], rec[1], False)

    def fake_delete(dev):
        store.pop(bytes(dev), None)

    monkeypatch.setattr(credsmod, "save_client_credential", fake_save)
    monkeypatch.setattr(credsmod, "load_client_credential", fake_load)
    monkeypatch.setattr(credsmod, "is_pending", fake_pending)
    monkeypatch.setattr(credsmod, "mark_active", fake_active)
    monkeypatch.setattr(credsmod, "delete_credential", fake_delete)
    return store


def _canned_hello_ack(session=0xAABBCCDD, mtu=247, mode=0, device=None, nonce=None):
    import struct
    return (struct.pack("<B", 3) + struct.pack("<I", session)
            + struct.pack("<H", mtu) + struct.pack("<I", 50)
            + (device or bytes(16)) + (nonce or bytes(range(16))) + bytes([mode]))


@needs_deps
def test_cli_device_arg_not_swallowed_by_claim():
    from client_app.ble_client import parse_cli_args
    key = "ab" * 32
    cli = parse_cli_args(["--enroll", "--claim", key])
    assert cli["device"] is None
    assert cli["enroll"] is True and cli["claim_hex"] == key
    cli = parse_cli_args(["Checkpoint", "--enroll", "--claim", key])
    assert cli["device"] == "Checkpoint"
    addr = "AA:BB:CC:DD:EE:FF"
    cli = parse_cli_args([addr, "--enroll", "--claim", key])
    assert cli["device"] == addr
    cli = parse_cli_args([f"--claim={key}", "--enroll", "MyDev"])
    assert cli["device"] == "MyDev" and cli["claim_hex"] == key
    cli = parse_cli_args([])
    assert cli["device"] is None and cli["enroll"] is False
    # --cli belongs to __main__ dispatch; the client parser must ignore it.
    cli = parse_cli_args(["--cli", "--enroll", "--claim", key])
    assert cli["device"] is None and cli["enroll"] is True


@needs_deps
def test_enroll_saves_pending_before_ready(monkeypatch):
    """Reliability: once AUTH_OK verifies, the credential is durable."""
    import asyncio
    import struct

    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient
    from client_app.crypto import (
        SERVER_DOM,
        build_transcript,
        derive_client_key_v3,
        derive_session_key_v3,
        finish_proof,
    )
    from client_app.protocol import proto_build, proto_parse

    store = _make_fake_cred_store(monkeypatch)
    device_id = bytes(range(16))
    device_nonce = bytes(range(64, 80))
    session = 0xAABBCCDD
    claim = bytes(range(32, 64))
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False,
                              claim_key=claim, enroll=True)
    writes = []

    async def fake_write(ptype, seq, payload=b""):
        writes.append((ptype, bytes(payload)))

    client.write_ctrl = fake_write
    assert client._parse_hello_ack(
        _canned_hello_ack(session, 247, 1, device_id, device_nonce)) is True

    async def run():
        await client._auth_exchange()

    async def driver():
        for _ in range(500):
            await asyncio.sleep(0.01)
            if any(w[0] == cfg.PKT_AUTH for w in writes):
                break
        auth_payload = next(w[1] for w in writes if w[0] == cfg.PKT_AUTH)
        client_nonce = auth_payload[16:32]
        enroll_key = derive_client_key_v3(claim, device_nonce, client_nonce,
                                          device_id, auth_payload[:16])
        transcript = build_transcript(device_id, auth_payload[:16], session,
                                      device_nonce, client_nonce, 1)
        expect_session = derive_session_key_v3(enroll_key, device_nonce, client_nonce,
                                               device_id, auth_payload[:16], session)
        server_proof = finish_proof(expect_session, SERVER_DOM, transcript)
        await client._handle_ctrl_packet(
            proto_parse(proto_build(cfg.PKT_AUTH_OK, 1, server_proof)))

    async def both():
        await asyncio.gather(run(), driver())

    asyncio.run(both())
    rec = store.get(device_id)
    assert rec is not None, "credential must be durable before READY is sent"
    assert rec[2] is True, "pre-READY credential must be marked pending"


@needs_deps
def test_ready_timeout_keeps_pending_then_reuses_it(monkeypatch):
    """Disconnect in the commit window: pending survives, next link reuses it."""
    import asyncio
    import struct

    import client_app.credentials as credsmod
    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient
    from client_app.protocol import proto_build, proto_parse

    store = _make_fake_cred_store(monkeypatch)
    monkeypatch.setattr(cfg, "ACK_TIMEOUT_S", 0.2)
    device_id = bytes(range(16))
    try:
        first = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False,
                                 claim_key=bytes(32), enroll=True)

        async def fake_hello_exchange(label):
            first.session_id = 0x1234ABCD
            first.device_id = device_id
            first.device_nonce = bytes(16)
            first.auth_mode = 1
            first.hello_acked.set()
            return "acked"

        async def fake_auth_exchange():
            first.session_key = b"S" * 16
            first._auth_transcript = b"T" * 88
            first._pending_enroll_key = b"E" * 32
            credsmod.save_client_credential(
                first.device_id, first.client_id, first._pending_enroll_key, pending=True)

        async def dropping_write(ptype, seq, payload=b""):
            pass  # every READY_ACK lost

        first._hello_exchange = fake_hello_exchange
        first._auth_exchange = fake_auth_exchange
        first.write_ctrl = dropping_write
        try:
            asyncio.run(first.do_handshake())
            raised = None
        except ConnectionError as e:
            raised = e
        assert raised is not None and "READY timed out" in str(raised)
        rec = store.get(device_id)
        assert rec is not None and rec[2] is True, "ambiguous commit must keep pending"

        second = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
        marked = []
        monkeypatch.setattr(credsmod, "mark_active",
                            lambda dev: (marked.append(bytes(dev))),
                            raising=False)

        async def fake_hello2(label):
            second.session_id = 0x1234ABCD
            second.device_id = device_id
            second.device_nonce = bytes(16)
            second.hello_acked.set()
            return "acked"

        async def fake_auth2():
            found = credsmod.load_client_credential(device_id)
            assert found is not None, "pending credential must be tried as normal K_client"
            second.client_id, second.client_key = found[0], found[1]
            second.session_key = b"S" * 16
            second._auth_transcript = b"T" * 88
            second._was_pending = credsmod.is_pending(device_id)

        async def ok_write(ptype, seq, payload=b""):
            if ptype == cfg.PKT_READY:
                pkt = proto_parse(proto_build(
                    cfg.PKT_READY_ACK, 1, struct.pack("<I", 0x1234ABCD)))
                await second._handle_ctrl_packet(pkt)

        second._hello_exchange = fake_hello2
        second._auth_exchange = fake_auth2
        second.write_ctrl = ok_write
        asyncio.run(second.do_handshake())
        assert marked == [device_id], "working pending credential must be marked active"
        assert second.enroll is False
    finally:
        monkeypatch.undo()


@needs_deps
def test_enroll_requires_active_window(monkeypatch):
    import asyncio

    import client_app.credentials as credsmod
    from client_app.ble_client import CheckpointClient

    monkeypatch.setattr(credsmod, "save_client_credential",
                        lambda *a, **k: (_ for _ in ()).throw(AssertionError("must not save")))
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False,
                              claim_key=bytes(32), enroll=True)

    async def fake_hello_exchange(label):
        client.session_id = 0x1234ABCD
        client.device_id = bytes(16)
        client.device_nonce = bytes(16)
        client.auth_mode = 0  # device reports: not in enrollment
        client.hello_acked.set()
        return "acked"

    client._hello_exchange = fake_hello_exchange
    try:
        asyncio.run(client.do_handshake())
        raised = None
    except RuntimeError as e:
        raised = e
    assert raised is not None and "Enrollment window is not active" in str(raised)


@needs_deps
def test_hello_exchange_raises_without_reconnecting(monkeypatch):
    import asyncio

    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient
    monkeypatch.setattr(cfg, "ACK_TIMEOUT_S", 0.05)
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)

    async def fake_write(ptype, seq, payload=b""):
        return None

    async def fake_wait():
        await asyncio.sleep(10)

    reconnected = []
    client.write_ctrl = fake_write
    client._wait_for_handshake_result = fake_wait
    client._connect_with_retry = lambda *a, **k: reconnected.append(True)
    try:
        asyncio.run(client._hello_exchange("attempt 1/7"))
        raised = None
    except ConnectionError as e:
        raised = e
    assert raised is not None and "timed out" in str(raised)
    assert reconnected == [], "hello exchange must not reconnect (supervisor owns that)"


@needs_deps
def test_supervisor_keeps_disconnect_between_handshake_and_wait():
    import asyncio

    from client_app.ble_client import CheckpointClient, supervise_link
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    connects = []

    class FakeDevice:
        address = "00:00:00:00:00:00"
        name = "Checkpoint"

    class FakeConnected:
        is_connected = True

    async def fake_rediscover(timeout=3.0):
        return FakeDevice()

    async def fake_connect(device=None):
        assert device is not None
        connects.append(1)
        client.client = FakeConnected()
        # Faithful to connect(): installing a generation clears stale events.
        client.link_lost.clear()

    async def fake_handshake():
        client.link_state = "up"
        # Bluetooth drops after handshake but before the steady-state wait.
        client.link_lost.set()
        client.client = None

    async def fake_disconnect():
        client.client = None

    client.rediscover = fake_rediscover
    client.connect = fake_connect
    client.do_handshake = fake_handshake
    client.disconnect = fake_disconnect
    stopped = [False]

    async def run():
        async def stopper():
            await asyncio.sleep(3.5)
            stopped[0] = True

        await asyncio.gather(
            supervise_link(client, log=lambda m: None,
                           is_stopped=lambda: stopped[0]),
            stopper())

    asyncio.run(run())
    assert len(connects) >= 2, "erased disconnect must still trigger reconnect"


@needs_deps
def test_connect_with_retry_recovers():
    import asyncio

    from client_app.ble_client import CheckpointClient
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    attempts = []

    async def flaky_connect():
        attempts.append(1)
        if len(attempts) < 3:
            raise RuntimeError("no adapter")

    client.connect = flaky_connect
    asyncio.run(client._connect_with_retry(tries=3, wait_s=0))
    assert len(attempts) == 3


@needs_deps
def test_hello_exchange_write_failure_raises():
    import asyncio

    from client_app.ble_client import CheckpointClient
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)

    async def failing_write(ptype, seq, payload=b""):
        raise RuntimeError("not connected")

    client.write_ctrl = failing_write
    try:
        asyncio.run(client._hello_exchange("attempt 1/7"))
        raised = None
    except ConnectionError as e:
        raised = e
    assert raised is not None and "HELLO write failed" in str(raised)


@needs_deps
def _make_fake_bleak():
    class FakeBleak:
        def __init__(self, address, *args, **kwargs):
            self.is_connected = False
            self.disconnected_callback = kwargs.get("disconnected_callback")

        async def connect(self, *args, **kwargs):
            self.is_connected = True

        async def pair(self):
            return None

        async def start_notify(self, *args):
            return None

    return FakeBleak


@needs_deps
def test_connect_logs_windows_bond_state(monkeypatch, capsys):
    import asyncio
    import sys as _sys

    from client_app import ble_client as mod
    from client_app.ble_client import CheckpointClient

    class FakePairing:
        is_paired = True

    class FakeDI:
        pairing = FakePairing()

    class FakeDev:
        device_information = FakeDI()

    class FakeBtMod:
        class BluetoothLEDevice:
            @staticmethod
            async def from_bluetooth_address_async(addr):
                assert addr == 0xE8F60A89384D
                return FakeDev()

    monkeypatch.setattr(mod, "BleakClient", _make_fake_bleak())
    monkeypatch.setitem(_sys.modules, "winrt.windows.devices.bluetooth", FakeBtMod)
    client = CheckpointClient("E8:F6:0A:89:38:4D", ingest_enabled=False, vad_enabled=False)
    asyncio.run(client.connect())
    assert "Windows bond present: True" in capsys.readouterr().out


@needs_deps
def test_connect_bond_probe_reports_missing_record(monkeypatch, capsys):
    import asyncio
    import sys as _sys

    from client_app import ble_client as mod
    from client_app.ble_client import CheckpointClient

    class FakeBtMod:
        class BluetoothLEDevice:
            @staticmethod
            async def from_bluetooth_address_async(addr):
                return None

    paired = []

    class PairingBleak:
        def __init__(self, address, *args, **kwargs):
            self.is_connected = False

        async def connect(self, *args, **kwargs):
            self.is_connected = True

        async def pair(self):
            paired.append(True)
            return True

        async def start_notify(self, *args):
            return None

    monkeypatch.setattr(mod, "BleakClient", PairingBleak)
    monkeypatch.setitem(_sys.modules, "winrt.windows.devices.bluetooth", FakeBtMod)
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    try:
        asyncio.run(client.connect())
        raised = None
    except RuntimeError as e:
        raised = e
    out = capsys.readouterr().out
    assert "Windows bond present: False" in out
    assert raised is not None and "refusing to auto-pair" in str(raised)
    assert paired == []


@needs_deps
def test_connect_pairs_in_enroll_mode(monkeypatch, capsys):
    import asyncio
    import sys as _sys

    from client_app import ble_client as mod
    from client_app.ble_client import CheckpointClient

    class FakeBtMod:
        class BluetoothLEDevice:
            @staticmethod
            async def from_bluetooth_address_async(addr):
                return None

    paired = []

    class PairingBleak:
        def __init__(self, address, *args, **kwargs):
            self.is_connected = False

        async def connect(self, *args, **kwargs):
            self.is_connected = True

        async def pair(self):
            paired.append(True)
            return True

        async def start_notify(self, *args):
            return None

    monkeypatch.setattr(mod, "BleakClient", PairingBleak)
    monkeypatch.setitem(_sys.modules, "winrt.windows.devices.bluetooth", FakeBtMod)
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False,
                              claim_key=bytes(32), enroll=True)
    asyncio.run(client.connect())
    out = capsys.readouterr().out
    assert "pairing for enrollment" in out
    assert paired == [True]


@needs_deps
def test_worker_serializes_device_writes():
    import asyncio

    from client_app.worker import BleWorker
    worker = BleWorker(on_log=lambda m: None, on_event=lambda e: None,
                       on_status=lambda t, c: None)
    worker.ensure_loop()
    try:
        order = []

        async def slow():
            await asyncio.sleep(0.2)
            order.append("slow")

        async def fast():
            order.append("fast")

        done_slow = worker.submit_serial(slow())
        done_fast = worker.submit_serial(fast())
        done_slow.result(timeout=10)
        done_fast.result(timeout=10)
        assert order == ["slow", "fast"]
    finally:
        worker.stop_loop()


def _make_phase1_client(monkeypatch=None):
    import asyncio

    from client_app.ble_client import CheckpointClient
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    sent = []

    async def fake_write_ack(ptype, seq, payload=b"", required=False):
        sent.append((ptype, seq, payload, required))
        return True

    client.write_ack = fake_write_ack
    return client, sent


def _announce_packet(file_id=0xABCD1234ABCD1234, total=44000, frags=200,
                     crc=0x12345678, start_seq=100, seq=7):
    import struct

    from client_app.protocol import proto_build
    payload = (b"\x10" + struct.pack("<I", total) + struct.pack("<H", frags)
               + struct.pack("<I", crc) + struct.pack("<H", start_seq)
               + struct.pack("<Q", file_id))
    return proto_build(0x10, seq, payload)


@needs_deps
def test_announce_restarts_without_part_state():
    import asyncio
    import struct

    from client_app import config as cfg
    from client_app.protocol import proto_parse
    client, sent = _make_phase1_client()
    pkt = proto_parse(_announce_packet(start_seq=100))

    async def run():
        await client._handle_announce(pkt)

    asyncio.run(run())
    assert client.current_file is not None
    assert client.current_file.received_frags == set()
    assert client.current_file.contig_seq == -1
    assert len(client.current_file.buffer) == 0
    assert len(sent) == 1 and sent[0][0] == cfg.PKT_FILE_ANNOUNCE_ACK
    assert sent[0][3] is True  # required ACK
    resume_echoed = struct.unpack("<H", sent[0][2][2:4])[0]
    assert resume_echoed == 0


@needs_deps
def test_file_done_race_keeps_new_file():
    import asyncio

    from client_app.protocol import proto_build, proto_parse
    client, sent = _make_phase1_client()

    async def run():
        await client._handle_announce(proto_parse(_announce_packet(file_id=0xAAAA, seq=1)))
        file_b_announce = proto_parse(_announce_packet(file_id=0xBBBB, seq=2))
        await client._handle_announce(file_b_announce)
        assert client.current_file is not None
        assert client.current_file.file_id == 0xBBBB
        # Stale FILE_DONE for A arrives after B started — B must survive.
        import struct
        done_a = proto_parse(proto_build(
            0x14, 9, struct.pack("<QII", 0xAAAA, 0x12345678, 44000)))
        await client._handle_file_done(done_a)
        assert client.current_file is not None
        assert client.current_file.file_id == 0xBBBB

    asyncio.run(run())


@needs_deps
def test_file_done_matching_id_clears_state(tmp_path, monkeypatch):
    import asyncio
    import struct
    import zlib

    from client_app import config as cfg
    from client_app.ble_client import IncomingFile
    from client_app.protocol import proto_build, proto_parse
    monkeypatch.setattr(cfg, "OUTPUT_DIR", tmp_path)
    client, sent = _make_phase1_client()
    data = b"hello"
    f = IncomingFile(file_id=0xCCCC, total_bytes=len(data), total_frags=1,
                     expected_crc=zlib.crc32(data) & 0xFFFFFFFF,
                     key=b"k" * 16, session_id=1)
    f.add_fragment(0, data)
    client.current_file = f
    done = proto_parse(proto_build(
        0x14, 11, struct.pack("<QII", 0xCCCC, f.expected_crc, len(data))))

    async def run():
        await client._handle_file_done(done)

    asyncio.run(run())
    assert client.current_file is None
    assert (tmp_path / "file_000000000000cccc.ogg").exists() or \
        (tmp_path / "file_000000000000cccc.wav").exists()


@needs_deps
def test_write_ack_returns_bool():
    import asyncio

    from client_app.ble_client import CheckpointClient

    class FlakyBleak:
        def __init__(self, fail_wnr=True, fail_wr=False):
            self.fail_wnr = fail_wnr
            self.fail_wr = fail_wr

        async def write_gatt_char(self, uuid, data, response):
            if response and self.fail_wr:
                raise RuntimeError("wr down")
            if not response and self.fail_wnr:
                raise RuntimeError("wnr down")

    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    client.client = FlakyBleak(fail_wnr=True, fail_wr=False)
    assert asyncio.run(client.write_ack(0x13, 1, b"\x00\x00\x00")) is True
    client.client = FlakyBleak(fail_wnr=True, fail_wr=True)
    assert asyncio.run(client.write_ack(0x13, 2, b"\x00\x00\x00", required=True)) is False


@needs_deps
def test_link_lost_fails_pending_commands():
    import asyncio

    from client_app.ble_client import CheckpointClient
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    loop = asyncio.new_event_loop()
    try:
        fut = loop.create_future()
        client._ctrl_pending[42] = fut

        async def wait_it():
            return await fut

        waiter = asyncio.ensure_future(wait_it(), loop=loop)
        loop.call_soon(client._handle_link_lost)
        try:
            loop.run_until_complete(asyncio.wait_for(waiter, timeout=5))
            raised = None
        except ConnectionError as e:
            raised = e
        assert raised is not None and "disconnected" in str(raised)
        assert client._ctrl_pending == {}
        assert client.link_lost.is_set()
    finally:
        loop.close()


@needs_deps
def test_supervisor_reconnects_on_link_loss():
    import asyncio

    from client_app.ble_client import CheckpointClient, supervise_link
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    connects = []
    losses = [False]

    class FakeDevice:
        address = "00:00:00:00:00:00"
        name = "Checkpoint"

    class FakeConnected:
        is_connected = True

    async def fake_rediscover(timeout=3.0):
        return FakeDevice()

    async def fake_connect(device=None):
        assert device is not None
        connects.append(1)
        client.client = FakeConnected()
        # Faithful to connect(): installing a generation clears stale events.
        client.link_lost.clear()
        client.link_state = "up"

    async def fake_handshake():
        pass

    client.rediscover = fake_rediscover
    client.connect = fake_connect
    client.do_handshake = fake_handshake

    async def fake_disconnect():
        client.client = None

    client.disconnect = fake_disconnect
    stopped = [False]

    async def run():
        async def stopper():
            await asyncio.sleep(0.2)
            if len(connects) >= 1 and not losses[0]:
                losses[0] = True
                client.link_lost.set()
            await asyncio.sleep(2.0)
            stopped[0] = True

        await asyncio.gather(
            supervise_link(client, log=lambda m: None,
                           is_stopped=lambda: stopped[0]),
            stopper())

    asyncio.run(run())
    assert len(connects) >= 2, f"supervisor must reconnect after link loss: {connects}"


@needs_deps
def test_hello_ack_validation():
    import struct

    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient

    def payload(ver=3, mtu=247, mode=0):
        p = struct.pack("<B", ver) + struct.pack("<I", 0x11111111)
        p += struct.pack("<H", mtu) + struct.pack("<I", 50)
        p += bytes(range(16)) + bytes(range(16, 32)) + struct.pack("<B", mode)
        return p

    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    assert client._parse_hello_ack(b"short") is False
    assert client._parse_hello_ack(payload(ver=99)) is False
    assert client._parse_hello_ack(payload(mtu=100)) is False
    assert client._parse_hello_ack(payload()[:-1]) is False
    assert client._parse_hello_ack(payload() + b"\x00") is False
    assert client._parse_hello_ack(payload()) is True
    assert client.frag_size == 220
    assert client.session_key is None
    assert client.device_id == bytes(range(16))
    assert client.device_nonce == bytes(range(16, 32))


@needs_deps
def test_handshake_sends_ready():
    import asyncio
    import struct

    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient
    from client_app.protocol import proto_build, proto_parse
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    writes = []

    async def fake_hello_exchange(label):
        client.session_id = 0x1234ABCD
        client.mtu = 247
        client.chunk_sec = 50
        client.device_id = bytes(16)
        client.device_nonce = bytes(range(16))
        client.session_key = b"S" * 16
        client._auth_transcript = b"T" * 88
        client.hello_acked.set()
        return "acked"

    async def fake_auth_exchange():
        client.session_key = b"S" * 16
        client._auth_transcript = b"T" * 88

    async def fake_write_ctrl(ptype, seq, payload=b""):
        writes.append((ptype, seq, payload))
        if ptype == cfg.PKT_READY:
            pkt = proto_parse(proto_build(
                cfg.PKT_READY_ACK, 1, struct.pack("<I", 0x1234ABCD)))
            await client._handle_ctrl_packet(pkt)

    client._hello_exchange = fake_hello_exchange
    client._auth_exchange = fake_auth_exchange
    client.write_ctrl = fake_write_ctrl
    asyncio.run(client.do_handshake())
    assert writes, "READY must be sent after AUTH validation"
    assert writes[0][0] == cfg.PKT_READY
    assert struct.unpack("<I", writes[0][2][:4])[0] == 0x1234ABCD
    assert len(writes[0][2]) == 36


@needs_deps
def test_next_seq_skips_zero():
    from client_app.ble_client import CheckpointClient
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    client._seq_gen = 0xFFFF
    assert client.next_seq() == 0xFFFF
    assert client.next_seq() == 1
    assert client._seq_gen == 2


@needs_deps
def test_link_lost_tears_down_session(tmp_path, monkeypatch):
    import asyncio
    import gc

    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient, IncomingFile
    monkeypatch.setattr(cfg, "OUTPUT_DIR", tmp_path)
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    loop = asyncio.new_event_loop()
    try:
        # Pending command whose waiter died mid-write: nobody will await it.
        fut = loop.create_future()
        client._ctrl_pending[7] = fut
        client.session_id = 0x12345678
        client.session_key = b"K" * 16
        client.master_key = b"K" * 16
        client.mtu = 517
        client.chunk_sec = 50
        client.current_file = IncomingFile(
            file_id=0x1, total_bytes=220, total_frags=1, expected_crc=0,
            key=b"k" * 16, session_id=1)
        client._open_part(0x1, 220)
        assert client._part_fh is not None
        loop.call_soon(client._handle_link_lost)
        loop.run_until_complete(asyncio.sleep(0.05))
        assert client._ctrl_pending == {}
        assert client.current_file is None
        assert client.session_id is None
        assert client.session_key is None
        assert client.mtu is None
        assert client._part_fh is None
        assert client.link_lost.is_set()
        # Exception was retrieved inside teardown: no "never retrieved" on GC.
        assert isinstance(fut.exception(), ConnectionError)
        del fut
        gc.collect()
    finally:
        loop.close()


@needs_deps
def test_stale_disconnect_callback_ignored():
    import asyncio

    from client_app.ble_client import CheckpointClient
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    current, old = object(), object()
    client.client = current
    client.link_state = "up"
    client.link_lost.clear()
    # Late callback from a superseded generation must not kill the live link.
    client._handle_link_lost(old)
    assert client.link_state == "up"
    assert not client.link_lost.is_set()
    # Callback for the active generation still works.
    client._handle_link_lost(current)
    assert client.link_state == "down"
    assert client.link_lost.is_set()


@needs_deps
def test_connect_installs_new_generation(monkeypatch):
    import asyncio

    from client_app import ble_client as mod
    from client_app.ble_client import CheckpointClient

    created = []

    class FakeBleak:
        def __init__(self, address, *args, **kwargs):
            created.append(kwargs.get("disconnected_callback"))
            self.is_connected = False

        async def connect(self, *args, **kwargs):
            self.is_connected = True

        async def pair(self):
            return True

        async def start_notify(self, *args):
            return None

    monkeypatch.setattr(mod, "BleakClient", FakeBleak)
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    old = object()
    client.client = old
    client.link_state = "up"
    client.link_lost.set()

    async def fake_bond():
        return True

    monkeypatch.setattr(client, "_windows_bond_present", fake_bond)
    asyncio.run(client.connect())
    assert client.client is not old
    assert callable(created[-1])
    assert client.link_state == "down"
    assert not client.link_lost.is_set()


@needs_deps
def test_supervisor_escalates_after_repeated_handshake_failure():
    import asyncio

    from client_app.ble_client import CheckpointClient, supervise_link
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False,
                              claim_key=bytes(32), enroll=True)
    connects, escalations = [], []

    class FakeDevice:
        address = "00:00:00:00:00:00"
        name = "Checkpoint"

    class FakeConnected:
        is_connected = True

    async def fake_rediscover(timeout=3.0):
        return FakeDevice()

    async def fake_connect(device=None):
        connects.append(1)
        client.client = FakeConnected()
        client.link_lost.clear()

    async def failing_handshake():
        raise ConnectionError("HELLO timed out")

    async def fake_disconnect():
        client.client = None

    async def fake_clear_bond(log=print):
        escalations.append(1)

    client.rediscover = fake_rediscover
    client.connect = fake_connect
    client.do_handshake = failing_handshake
    client.disconnect = fake_disconnect
    client.clear_stale_bond = fake_clear_bond
    stopped = [False]

    async def run():
        async def stopper():
            for _ in range(60):
                await asyncio.sleep(0.2)
                if escalations:
                    break
            stopped[0] = True

        await asyncio.gather(
            supervise_link(client, log=lambda m: None,
                           is_stopped=lambda: stopped[0]),
            stopper())

    asyncio.run(run())
    assert escalations == [1], "must escalate exactly once per 3 setup failures"
    assert len(connects) >= 3


@needs_deps
def test_supervisor_escalates_after_repeated_connect_failure():
    import asyncio

    from client_app.ble_client import CheckpointClient, supervise_link
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False,
                              claim_key=bytes(32), enroll=True)
    connects, escalations = [], []

    class FakeDevice:
        address = "00:00:00:00:00:00"
        name = "Checkpoint"

    async def fake_rediscover(timeout=3.0):
        return FakeDevice()

    async def failing_connect(device=None):
        # Link dies during subscribe, before any handshake: the observed
        # power-cycle failure mode.
        connects.append(1)
        raise ConnectionError("Characteristic was not found!")

    async def fake_disconnect():
        client.client = None

    async def fake_clear_bond(log=print):
        escalations.append(1)

    client.rediscover = fake_rediscover
    client.connect = failing_connect
    client.disconnect = fake_disconnect
    client.clear_stale_bond = fake_clear_bond
    stopped = [False]

    async def run():
        async def stopper():
            for _ in range(60):
                await asyncio.sleep(0.2)
                if escalations:
                    break
            stopped[0] = True

        await asyncio.gather(
            supervise_link(client, log=lambda m: None,
                           is_stopped=lambda: stopped[0]),
            stopper())

    asyncio.run(run())
    assert escalations == [1], "connect-phase failures must escalate too"
    assert len(connects) >= 3


@needs_deps
def test_clear_stale_bond_unpairs_and_resets():
    import asyncio

    from client_app.ble_client import CheckpointClient
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    calls = []

    class FakeBleak:
        async def unpair(self):
            calls.append("unpair")

        async def disconnect(self):
            calls.append("disconnect")

    current = FakeBleak()
    client.client = current
    client.link_state = "up"
    asyncio.run(client.clear_stale_bond(log=lambda m: None))
    assert calls == ["unpair", "disconnect"]
    assert client.client is None
    assert client.link_state == "down"


@needs_deps
def test_ctrl_writes_serialized():
    import asyncio

    from client_app.ble_client import CheckpointClient

    class OrderBleak:
        def __init__(self):
            self.events = []

        async def write_gatt_char(self, uuid, data, response):
            self.events.append("enter")
            await asyncio.sleep(0.05)
            self.events.append("exit")

    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    client.client = OrderBleak()

    async def run():
        await asyncio.gather(client.write_ctrl(0x22, 1), client.write_ctrl(0x22, 2))

    asyncio.run(run())
    assert client.client.events == ["enter", "exit", "enter", "exit"]


@needs_deps
def test_part_resume_continues_where_left_off(tmp_path, monkeypatch):
    import asyncio
    import json
    import struct

    from client_app import config as cfg
    from client_app.protocol import proto_parse
    monkeypatch.setattr(cfg, "OUTPUT_DIR", tmp_path)
    monkeypatch.setattr(cfg, "BLE_FRAG_SIZE_GUESS", 220)
    client, sent = _make_phase1_client()
    file_id, total, frags, crc = 0xDDDD0001, 220 * 10, 10, 0x11111111
    part = tmp_path / f"file_{file_id:016x}.part"
    part.write_bytes(b"A" * 220 * 4 + b"\x00" * 220 * 6)
    (tmp_path / f"file_{file_id:016x}.part.json").write_text(json.dumps({
        "crc": f"{crc:08x}", "total": total, "total_frags": frags,
        "frag_size": 220, "received": [0, 1, 2, 3]}))

    async def run():
        await client._handle_announce(proto_parse(
            _announce_packet(file_id=file_id, total=total, frags=frags,
                             crc=crc, start_seq=0)))

    asyncio.run(run())
    assert client.current_file.contig_seq == 3
    assert client.current_file.buffer[:220] == b"A" * 220
    resume_echoed = struct.unpack("<H", sent[0][2][2:4])[0]
    assert resume_echoed == 4


@needs_deps
def test_part_resume_rejects_tampered_sidecar(tmp_path, monkeypatch):
    import asyncio
    import json
    import struct

    from client_app import config as cfg
    from client_app.protocol import proto_parse
    monkeypatch.setattr(cfg, "OUTPUT_DIR", tmp_path)
    monkeypatch.setattr(cfg, "BLE_FRAG_SIZE_GUESS", 220)
    client, sent = _make_phase1_client()
    file_id = 0xDDDD0002
    (tmp_path / f"file_{file_id:016x}.part").write_bytes(b"\x00" * 2200)
    (tmp_path / f"file_{file_id:016x}.part.json").write_text(json.dumps({
        "crc": "deadbeef", "total": 2200, "total_frags": 10,
        "frag_size": 220, "received": [0, 1]}))

    async def run():
        await client._handle_announce(proto_parse(
            _announce_packet(file_id=file_id, total=2200, frags=10,
                             crc=0x22222222, start_seq=0)))

    asyncio.run(run())
    assert client.current_file.contig_seq == -1
    assert client.current_file.received_frags == set()
    resume_echoed = struct.unpack("<H", sent[0][2][2:4])[0]
    assert resume_echoed == 0


@needs_deps
def test_duplicate_file_done_reacks_from_cache(tmp_path, monkeypatch):
    import asyncio
    import struct
    import zlib

    from client_app import config as cfg
    from client_app.ble_client import IncomingFile
    from client_app.protocol import proto_build, proto_parse
    monkeypatch.setattr(cfg, "OUTPUT_DIR", tmp_path)
    client, sent = _make_phase1_client()
    data = b"OggS" + b"\x00" * 216
    total, frags = len(data), 1
    crc = zlib.crc32(data) & 0xFFFFFFFF
    saved = tmp_path / "file_00ee00ee.ogg"
    saved.write_bytes(data)
    client.completed[0x00EE00EE] = {"crc": f"{crc:08x}", "size": total,
                                    "path": str(saved)}
    f = IncomingFile(file_id=0x00EE00EE, total_bytes=total, total_frags=frags,
                     expected_crc=crc, key=b"k" * 16, session_id=1)
    client.current_file = f  # empty buffer: duplicate DONE for a verified file
    done = proto_parse(proto_build(0x14, 21, struct.pack("<QII", 0x00EE00EE, crc, total)))

    async def run():
        await client._handle_file_done(done)

    asyncio.run(run())
    acks = [s for s in sent if s[0] == cfg.PKT_FILE_DONE_ACK]
    assert acks and acks[0][2][2] == 0x01  # success re-ACKed, no retransfer
    assert client.current_file is None


@needs_deps
def test_hkdf_rfc5869_vector():
    """RFC 5869 Test Case 1 (first block) — proves hkdf_sha256 is standard
    HKDF, not custom. Single-block outputs only (keys are 16 bytes)."""
    from client_app.crypto import hkdf_sha256
    ikm = bytes.fromhex("0b" * 22)
    salt = bytes.fromhex("000102030405060708090a0b0c")
    info = bytes.fromhex("f0f1f2f3f4f5f6f7f8f9")
    okm = hkdf_sha256(salt, ikm, info, 32)
    assert okm.hex() == ("3cb25f25faacd57a90434f64d0362f2a"
                         "2d2d0a90cf1a5a4c5db02d56ecc4c5bf")


@needs_deps
def test_supervisor_no_bond_escalation_in_normal_mode():
    import asyncio

    from client_app.ble_client import CheckpointClient, supervise_link
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    connects, escalations = [], []

    class FakeDevice:
        address = "00:00:00:00:00:00"
        name = "Checkpoint"

    async def fake_rediscover(timeout=3.0):
        return FakeDevice()

    async def failing_connect(device=None):
        connects.append(1)
        raise ConnectionError("Characteristic was not found!")

    async def fake_disconnect():
        client.client = None

    async def fake_clear_bond(log=print):
        escalations.append(1)

    client.rediscover = fake_rediscover
    client.connect = failing_connect
    client.disconnect = fake_disconnect
    client.clear_stale_bond = fake_clear_bond
    stopped = [False]

    async def run():
        async def stopper():
            for _ in range(60):
                await asyncio.sleep(0.2)
                if len(connects) >= 3:
                    break
            stopped[0] = True

        await asyncio.gather(
            supervise_link(client, log=lambda m: None,
                           is_stopped=lambda: stopped[0]),
            stopper())

    asyncio.run(run())
    assert len(connects) >= 3
    assert escalations == [], "normal mode must not auto-clear bonds"


@needs_deps
def test_end_to_end_key_schedule():
    """v3 schedule: claim -> client key -> session key -> file key."""
    import hashlib
    import hmac as _hmac
    import struct

    from client_app.crypto import derive_client_key_v3, derive_file_key, derive_session_key_v3
    claim = bytes(range(32))
    dev = bytes(range(16))
    cli = bytes(range(16, 32))
    dn = bytes(range(32, 48))
    cn = bytes(range(48, 64))
    session = 0x12345678
    uid = 0x1122334455667788
    client_key = derive_client_key_v3(claim, dn, cn, dev, cli)
    session_key = derive_session_key_v3(client_key, dn, cn, dev, cli, session)
    file_key = derive_file_key(session_key, session, uid)
    salt = dn + cn
    prk1 = _hmac.new(salt, claim, hashlib.sha256).digest()
    expect_client = _hmac.new(
        prk1, b"checkpoint-client-v3" + dev + cli + b"\x01",
        hashlib.sha256).digest()
    assert client_key == expect_client
    prk2 = _hmac.new(salt, expect_client, hashlib.sha256).digest()
    expect_session = _hmac.new(
        prk2, b"checkpoint-session-v3" + dev + cli + struct.pack("<I", session) + b"\x01",
        hashlib.sha256).digest()[:16]
    assert session_key == expect_session
    prk3 = _hmac.new(b"", expect_session, hashlib.sha256).digest()
    expect_file = _hmac.new(
        prk3, b"checkpoint-file-v1" + struct.pack("<IQ", session, uid) + b"\x01",
        hashlib.sha256).digest()[:16]
    assert file_key == expect_file


@needs_deps
def test_cloud_opcodes_present():
    from client_app import config as cfg
    assert cfg.CTRL_CMD_TIME_SET == 0x14
    assert cfg.CTRL_CMD_GET_CLOUD_SECRET == 0x23
    assert cfg.CTRL_CMD_CLEAR_TRUSTED_SLOTS == 0x24
    assert cfg.CTRL_CMD_FORGET_SELF == 0x25


@needs_deps
def test_cloud_secret_roundtrip():
    from client_app import config as cfg
    from client_app.crypto import build_cloud_nonce, open_cloud_secret
    from cryptography.hazmat.primitives.ciphers.aead import AESCCM

    session_id = 0x11223344
    seq = 0x0102
    session_key = bytes(range(16))
    secret = bytes([0x11]) * cfg.AUTH_CLOUD_SECRET_BYTES
    nonce = build_cloud_nonce(session_id, seq)
    aad = struct.pack("<BBH", cfg.PROTO_VER, cfg.CTRL_CMD_GET_CLOUD_SECRET, seq)
    sealed = AESCCM(session_key, tag_length=cfg.CRYPTO_TAG_BYTES).encrypt(nonce, secret, aad)

    assert open_cloud_secret(session_key, session_id, seq, nonce + sealed) == secret
    assert open_cloud_secret(session_key, session_id, seq + 1, nonce + sealed) is None
