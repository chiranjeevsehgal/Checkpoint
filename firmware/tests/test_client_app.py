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
    header = struct.pack("<BBHH", 1, 0x12, 300, 5)
    expected = header + b"hello" + struct.pack("<I", zlib.crc32(header + b"hello") & 0xFFFFFFFF)
    assert proto_build(0x12, 300, b"hello") == expected
    assert proto_parse(expected).payload == b"hello"
    prk = hashlib.sha256(b"0" * 16).digest()
    info = struct.pack("<II", 1, 2)
    assert derive_file_key(b"0" * 16, 1, 2) == \
        hashlib.sha256(prk + info + b"\x01").digest()[:16]
    assert build_nonce(9, 0x1234ABCD, 5) == struct.pack("<IIH", 9, 0x1234ABCD, 5) + b"\xA5\x5A"
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
    assert row["file_id"] == "1234abcd"
    assert list(row.keys()) == cfg.BENCH_FIELDNAMES
    assert row["median_rtt_ms"] == row["p95_rtt_ms"]  # single sample
    csv_path = tmp_path / "bench.csv"
    rec2 = BenchRecorder(csv_path)
    rec2.reset_file(0x1, 10, 1, 0)
    rec2.finalize(False, 10, 0, 220, ingest_status="pending", vad_status="pending")
    rec2.update_ingest("00000001", "u1", "SUBMITTED")
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
def test_settle_for_encryption_recovers():
    import asyncio

    from client_app.ble_client import CheckpointClient
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    calls = []
    printed = []

    async def fake_exchange(label):
        calls.append(label)
        if len(calls) < 3:
            client.last_error = 0x01
            return "error"
        return "acked"

    client._hello_exchange = fake_exchange
    client._print_handshake_complete = lambda: printed.append(True)
    assert asyncio.run(client._settle_for_encryption(tries=3, wait_s=0)) is True
    assert len(calls) == 3 and printed == [True]


@needs_deps
def test_settle_for_encryption_gives_up():
    import asyncio

    from client_app.ble_client import CheckpointClient
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    calls = []

    async def fake_exchange(label):
        calls.append(label)
        client.last_error = 0x01
        return "error"

    client._hello_exchange = fake_exchange
    assert asyncio.run(client._settle_for_encryption(tries=3, wait_s=0)) is False
    assert len(calls) == 3


@needs_deps
def test_is_link_drop_mapping():
    from client_app.ble_client import CheckpointClient
    assert CheckpointClient._is_link_drop("Not connected") is True
    assert CheckpointClient._is_link_drop("Characteristic X was not found!") is True
    assert CheckpointClient._is_link_drop("Device disconnected") is True
    assert CheckpointClient._is_link_drop("access denied") is False


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
def test_hello_exchange_timeout_reconnects(monkeypatch):
    import asyncio

    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient
    monkeypatch.setattr(cfg, "ACK_TIMEOUT_S", 0.05)
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    reconnected = []

    async def fake_write(ptype, seq, payload=b""):
        return None

    async def fake_wait():
        await asyncio.sleep(10)

    async def fake_reconnect(tries=3, wait_s=2.0):
        reconnected.append(True)

    client.write_ctrl = fake_write
    client._wait_for_handshake_result = fake_wait
    client._connect_with_retry = fake_reconnect
    assert asyncio.run(client._hello_exchange("attempt 1/7")) == "reconnected"
    assert reconnected == [True]


@needs_deps
def _make_fake_bleak():
    class FakeBleak:
        def __init__(self, address):
            self.is_connected = False

        async def connect(self):
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

    monkeypatch.setattr(mod, "BleakClient", _make_fake_bleak())
    monkeypatch.setitem(_sys.modules, "winrt.windows.devices.bluetooth", FakeBtMod)
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    asyncio.run(client.connect())
    assert "no WinRT record" in capsys.readouterr().out


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
