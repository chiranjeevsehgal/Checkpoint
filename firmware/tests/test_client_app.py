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
    import hmac as _hmac
    prk = _hmac.new(b"", b"0" * 16, hashlib.sha256).digest()
    info = b"checkpoint-file-v1" + struct.pack("<II", 1, 2)
    assert derive_file_key(b"0" * 16, 1, 2) == \
        _hmac.new(prk, info + b"\x01", hashlib.sha256).digest()[:16]
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
        def __init__(self, address, *args, **kwargs):
            self.is_connected = False
            self.disconnected_callback = kwargs.get("disconnected_callback")

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


def _announce_packet(file_id=0xABCD1234, total=44000, frags=200,
                     crc=0x12345678, start_seq=100, seq=7):
    import struct

    from client_app.protocol import proto_build
    payload = (b"\x10" + struct.pack("<I", total) + struct.pack("<H", frags)
               + struct.pack("<I", crc) + struct.pack("<H", start_seq)
               + struct.pack("<I", file_id))
    return proto_build(0x10, seq, payload)


@needs_deps
def test_announce_forces_resume_zero():
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
            0x14, 9, struct.pack("<III", 0xAAAA, 0x12345678, 44000)))
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
        0x14, 11, struct.pack("<III", 0xCCCC, f.expected_crc, len(data))))

    async def run():
        await client._handle_file_done(done)

    asyncio.run(run())
    assert client.current_file is None
    assert (tmp_path / "file_0000cccc.ogg").exists() or \
        (tmp_path / "file_0000cccc.wav").exists()


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

    class FakeConnected:
        is_connected = True

    async def fake_connect():
        connects.append(1)
        client.client = FakeConnected()
        client.link_state = "up"

    async def fake_handshake():
        pass

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

    def payload(ver=1, mtu=247, key=True):
        p = struct.pack("<B", ver) + struct.pack("<I", 0x11111111)
        p += struct.pack("<H", mtu) + struct.pack("<I", 50)
        if key:
            p += b"K" * 16
        return p

    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    assert client._parse_hello_ack(b"short") is False
    assert client._parse_hello_ack(payload(ver=99)) is False
    assert client._parse_hello_ack(payload(mtu=100)) is False
    assert client._parse_hello_ack(payload(key=False)) is False
    assert client._parse_hello_ack(payload()) is True
    assert client.frag_size == 220
    assert client.master_key == b"K" * 16


@needs_deps
def test_handshake_sends_ready():
    import asyncio
    import struct

    from client_app import config as cfg
    from client_app.ble_client import CheckpointClient
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    writes = []

    async def fake_hello_exchange(label):
        client.session_id = 0x1234ABCD
        client.mtu = 247
        client.chunk_sec = 50
        client.master_key = b"K" * 16
        client.frag_size = 220
        client.hello_acked.set()
        return "acked"

    async def fake_write_ctrl(ptype, seq, payload=b""):
        writes.append((ptype, seq, payload))

    client._hello_exchange = fake_hello_exchange
    client.write_ctrl = fake_write_ctrl
    asyncio.run(client.do_handshake())
    assert writes, "READY must be sent after HELLO_ACK validation"
    assert writes[0][0] == cfg.PKT_READY
    assert struct.unpack("<I", writes[0][2])[0] == 0x1234ABCD


@needs_deps
def test_next_seq_skips_zero():
    from client_app.ble_client import CheckpointClient
    client = CheckpointClient("00:00:00:00:00:00", ingest_enabled=False, vad_enabled=False)
    client._seq_gen = 0xFFFF
    assert client.next_seq() == 0xFFFF
    assert client.next_seq() == 1
    assert client._seq_gen == 2


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
    part = tmp_path / f"file_{file_id:08x}.part"
    part.write_bytes(b"A" * 220 * 4 + b"\x00" * 220 * 6)
    (tmp_path / f"file_{file_id:08x}.part.json").write_text(json.dumps({
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
    (tmp_path / f"file_{file_id:08x}.part").write_bytes(b"\x00" * 2200)
    (tmp_path / f"file_{file_id:08x}.part.json").write_text(json.dumps({
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
    done = proto_parse(proto_build(0x14, 21, struct.pack("<III", 0x00EE00EE, crc, total)))

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
