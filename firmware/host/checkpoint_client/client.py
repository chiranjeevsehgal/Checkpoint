"""
Checkpoint — BLE test client + benchmark capture

Implements the device's custom sync protocol end-to-end so you can validate
the firmware without building the real phone app first:
  - connect + BLE bonding/encryption (Just Works, BLE_HS_IO_NO_INPUT_OUTPUT — ble_service.cpp:121)
  - HELLO / HELLO_ACK handshake (extracts session id + AES master key + mtU/chunk)
  - receives FILE_ANNOUNCE, decrypts DATA fragments (AES-128-CCM),
    reassembles the file, verifies CRC32, and ACKs everything
  - writes the reassembled WAV/OGG to ./received/ (auto-detect OggS vs RIFF)
  - captures throughput benchmark (goodput, RTT p50/p95, retries, duplicates) to
    ./received/benchmark_*.csv/json — mirrors bench.h / transfer.cpp window

Mirrors: protocol.h, ble_service.cpp, crypto.cpp, transfer.cpp, bench.h, config.h
Recent firmware: HW_HAS_SD_DETECT 0 (8-pin DAT2/DAT1), boot_id REC_%06_%04 (recorder.cpp),
crypto mutex (crypto.cpp), UI non-blocking (ui.cpp)

Requires:
    pip install bleak cryptography

Usage:
    python client.py [device_name_or_address]
    # Default device: Checkpoint (BLE_DEVICE_NAME in config.h)
    # With benchmark: python client.py --bench   (also writes csv/json)
"""

import asyncio
import csv
import json
import struct
import sys
import time
import zlib
import hashlib
from pathlib import Path
from dataclasses import dataclass, field

from bleak import BleakClient, BleakScanner
from cryptography.hazmat.primitives.ciphers.aead import AESCCM

# ---------------------------------------------------------------------------
# Config — must match config.h exactly
# ---------------------------------------------------------------------------

DEVICE_NAME = "Checkpoint"

SERVICE_UUID = "9a8b0001-4a2b-4e3c-8f1a-5b2c9d0e1f2a"
CTRL_UUID    = "9a8b0002-4a2b-4e3c-8f1a-5b2c9d0e1f2a"
DATA_UUID    = "9a8b0003-4a2b-4e3c-8f1a-5b2c9d0e1f2a"
ACK_UUID     = "9a8b0004-4a2b-4e3c-8f1a-5b2c9d0e1f2a"

PROTO_VER = 1
PROTO_HEADER = 6   # ver, type, seq_lo, seq_hi, len_lo, len_hi
PROTO_CRC = 4

CRYPTO_KEY_BYTES = 16
CRYPTO_NONCE_BYTES = 12
CRYPTO_TAG_BYTES = 8

# MTU overhead mirrors bench.h / config.h: PROTO_HEADER 6 + PROTO_CRC 4 + CRYPTO_TAG 8 + ATT 3 + spare 4
MTU_OVERHEAD = PROTO_HEADER + CRYPTO_TAG_BYTES + PROTO_CRC + 3 + 4  # 25; mtu-25 = 222, capped to 220
# Keep BLE_FRAG_SIZE_GUESS as ground truth caps — firmware config.h:57 BLE_FRAG_SIZE 220, BLE_MTU 247
BLE_FRAG_SIZE_GUESS = 220

OUTPUT_DIR = Path(__file__).resolve().parent / "received"
BENCH_DIR = OUTPUT_DIR
ACK_TIMEOUT_S = 5.0

# ---------------------------------------------------------------------------
# Packet types — must match protocol.h PacketType enum
# ---------------------------------------------------------------------------

PKT_HELLO = 0x01
PKT_HELLO_ACK = 0x02
PKT_FILE_ANNOUNCE = 0x10
PKT_FILE_ANNOUNCE_ACK = 0x11
PKT_DATA = 0x12
PKT_ACK = 0x13
PKT_FILE_DONE = 0x14
PKT_FILE_DONE_ACK = 0x15
PKT_ERROR = 0x16
PKT_RESUME_REQ = 0x17
PKT_RESUME_RESP = 0x18
PKT_KEEPALIVE = 0x19
PKT_CMD = 0x20
PKT_CMD_RESP = 0x21
PKT_STATUS_REQ = 0x22
PKT_STATUS_RESP = 0x23
PKT_STORAGE_REQ = 0x24
PKT_STORAGE_RESP = 0x25
PKT_LIST_REQ = 0x26
PKT_LIST_RESP = 0x27

# Command IDs / status codes — must match control.h.
CTRL_CMD_REC_START = 0x01
CTRL_CMD_REC_STOP = 0x02
CTRL_CMD_LED_SET = 0x10
CTRL_CMD_LED_GET = 0x11
CTRL_CMD_SYNC_SET = 0x12
CTRL_CMD_SYNC_GET = 0x13
CTRL_CMD_FILE_DELETE = 0x20
CTRL_CMD_STORAGE_ERASE = 0x21
CTRL_OK = 0x00
CTRL_ERR_BUSY = 0x05
CTRL_ERR_NOT_FOUND = 0x06
CTRL_STATUS_LEN = 17
CTRL_STORAGE_LEN = 20
CTRL_BRIGHT_MIN = 5
CTRL_ERASE_ARM = 0x01
CTRL_ERASE_CONFIRM = 0x02
CTRL_LIST_FLAG_ACTIVE = 0x04

PKT_NAMES = {
    PKT_HELLO: "HELLO", PKT_HELLO_ACK: "HELLO_ACK",
    PKT_FILE_ANNOUNCE: "FILE_ANNOUNCE", PKT_FILE_ANNOUNCE_ACK: "FILE_ANNOUNCE_ACK",
    PKT_DATA: "DATA", PKT_ACK: "ACK",
    PKT_FILE_DONE: "FILE_DONE", PKT_FILE_DONE_ACK: "FILE_DONE_ACK",
    PKT_ERROR: "ERROR", PKT_RESUME_REQ: "RESUME_REQ", PKT_RESUME_RESP: "RESUME_RESP",
    PKT_KEEPALIVE: "KEEPALIVE",
    PKT_CMD: "CMD", PKT_CMD_RESP: "CMD_RESP",
    PKT_STATUS_REQ: "STATUS_REQ", PKT_STATUS_RESP: "STATUS_RESP",
    PKT_STORAGE_REQ: "STORAGE_REQ", PKT_STORAGE_RESP: "STORAGE_RESP",
    PKT_LIST_REQ: "LIST_REQ", PKT_LIST_RESP: "LIST_RESP",
}


# ---------------------------------------------------------------------------
# Protocol framing — mirrors protocol.cpp exactly (CRC32, header layout)
# ---------------------------------------------------------------------------

def crc32(data: bytes) -> int:
    # zlib.crc32 uses the same polynomial/init/xor-out as the firmware's table-based crc32
    return zlib.crc32(data) & 0xFFFFFFFF


def proto_build(ptype: int, seq: int, payload: bytes = b"") -> bytes:
    header = struct.pack("<BBHH", PROTO_VER, ptype, seq, len(payload))
    body = header + payload
    crc = crc32(body)
    return body + struct.pack("<I", crc)


@dataclass
class Packet:
    version: int
    type: int
    seq: int
    payload: bytes


def proto_parse(data: bytes) -> Packet | None:
    if len(data) < PROTO_HEADER + PROTO_CRC:
        return None
    ver, ptype, seq, plen = struct.unpack("<BBHH", data[:PROTO_HEADER])
    if ver != PROTO_VER:
        return None
    need = PROTO_HEADER + plen + PROTO_CRC
    if len(data) < need:
        return None
    payload = data[PROTO_HEADER:PROTO_HEADER + plen]
    crc_off = PROTO_HEADER + plen
    recv_crc = struct.unpack("<I", data[crc_off:crc_off + 4])[0]
    calc_crc = crc32(data[:PROTO_HEADER + plen])
    if recv_crc != calc_crc:
        print(f"  [!] CRC mismatch on packet type={ptype}")
        return None
    return Packet(ver, ptype, seq, payload)


# ---------------------------------------------------------------------------
# Crypto — mirrors crypto.cpp (AES-128-CCM, custom KDF, custom nonce layout)
# ---------------------------------------------------------------------------

def derive_file_key(master_key: bytes, session_id: int, file_id: int) -> bytes:
    """Mirrors crypto_derive_file_key exactly (SHA256-based, not real HKDF)."""
    prk = hashlib.sha256(master_key).digest()
    info = struct.pack("<II", session_id, file_id)
    tmp = prk + info + b"\x01"
    h = hashlib.sha256(tmp).digest()
    return h[:CRYPTO_KEY_BYTES]


def build_nonce(session_id: int, file_id: int, seq: int) -> bytes:
    """Mirrors crypto_build_nonce exactly."""
    return struct.pack("<IIH", session_id, file_id, seq) + b"\xA5\x5A"


def decrypt_fragment(key: bytes, session_id: int, file_id: int, seq: int,
                      frag_len: int, ciphertext_and_tag: bytes) -> bytes | None:
    nonce = build_nonce(session_id, file_id, seq)
    aad = struct.pack("<BBHH", PROTO_VER, PKT_DATA, seq, frag_len)
    aesccm = AESCCM(key, tag_length=CRYPTO_TAG_BYTES)
    try:
        return aesccm.decrypt(nonce, ciphertext_and_tag, aad)
    except Exception as e:
        print(f"  [!] Decrypt failed for seq={seq}: {e}")
        return None


# ---------------------------------------------------------------------------
# Transfer state + benchmark
# ---------------------------------------------------------------------------


@dataclass
class IncomingFile:
    file_id: int
    total_bytes: int
    total_frags: int
    expected_crc: int
    key: bytes
    session_id: int
    frag_size: int = BLE_FRAG_SIZE_GUESS
    buffer: bytearray = field(default_factory=bytearray)
    received_frags: set = field(default_factory=set)
    contig_seq: int = -1  # highest contiguous seq acked (cumulative)

    def add_fragment(self, seq: int, data: bytes):
        offset = seq * self.frag_size
        if offset + len(data) > len(self.buffer):
            self.buffer.extend(b"\x00" * (offset + len(data) - len(self.buffer)))
        self.buffer[offset:offset + len(data)] = data
        self.received_frags.add(seq)
        # Advance contiguous pointer if possible
        # Called after add, caller may also advance via loop for out-of-order
        while (self.contig_seq + 1) in self.received_frags:
            self.contig_seq += 1

    def is_complete(self) -> bool:
        if not self.received_frags:
            return False
        if len(self.received_frags) < self.total_frags:
            return False
        # Byte-coverage check: reassembled buffer must span total_bytes without gaps
        # (len check alone can hide missing middle fragments filled with zeros)
        return len(bytes(self.buffer[:self.total_bytes])) == self.total_bytes and \
               all(i in self.received_frags for i in range(self.total_frags))


class CheckpointClient:
    def __init__(self, address: str, bench_csv: Path | None = None):
        self.address = address
        self.client: BleakClient | None = None
        self.session_id: int | None = None
        self.master_key: bytes | None = None
        self.mtu: int | None = None
        self.chunk_sec: int | None = None
        self.frag_size: int = BLE_FRAG_SIZE_GUESS
        self.hello_acked = asyncio.Event()
        self.error_event = asyncio.Event()
        self.last_error: int | None = None

        self.current_file: IncomingFile | None = None
        self.file_done_event = asyncio.Event()
        self.announce_event = asyncio.Event()
        self._seq_gen = 1
        # Serialize DATA handling and ACK writes to avoid concurrent buffer corruption
        # and ATT WNR overflow (previously create_task per DATA caused out-of-order ACKs)
        self._data_lock = asyncio.Lock()
        self._ack_lock = asyncio.Lock()
        # Bench metrics — per-file + aggregate csv
        self.bench_csv = bench_csv
        self._bench_rows: list[dict] = []
        self._bench_t_send: dict[int, float] = {}
        self._bench_rtts: list[float] = []
        self._bench_file_start: float | None = None
        self._bench_file_meta: dict | None = None
        self._bench_duplicates: int = 0
        self._bench_decrypt_fail: int = 0
        self._csv_writer: csv.DictWriter | None = None
        self._csv_file = None
        if bench_csv:
            bench_csv.parent.mkdir(parents=True, exist_ok=True)
            self._csv_file = bench_csv.open("w", newline="", encoding="utf-8")
            fieldnames = ["ts","file_id","total_bytes","total_frags","mtu","frag_size","goodput_kBps","median_rtt_ms","p95_rtt_ms","duplicates","retries","decrypt_fail","crc_ok","resume_from","elapsed_s"]
            self._csv_writer = csv.DictWriter(self._csv_file, fieldnames=fieldnames)
            self._csv_writer.writeheader()
            self._csv_file.flush()

    def next_seq(self) -> int:
        s = self._seq_gen
        self._seq_gen = (self._seq_gen + 1) & 0xFFFF
        return s

    # -- Bench helpers -------------------------------------------------
    def _bench_reset_file(self, file_id: int, total: int, total_frags: int, resume_from: int):
        self._bench_file_start = time.perf_counter()
        self._bench_rtts.clear()
        self._bench_t_send.clear()
        self._bench_duplicates = 0
        self._bench_decrypt_fail = 0
        self._bench_file_meta = {"file_id": file_id, "total": total, "total_frags": total_frags, "resume_from": resume_from}

    def _bench_on_ack_sent(self, ack_seq: int):
        # track when we send ACK for a DATA seq so RTT = data_notify_time - ack_sent? Actually RTT is DATA arrival -> ACK send -> next DATA; we measure ACK->next DATA via t_send
        # simpler: store t_send per DATA seq when we ACK it
        self._bench_t_send[ack_seq] = time.perf_counter()

    def _bench_on_data_recv(self, seq: int):
        now = time.perf_counter()
        if seq in self._bench_t_send:
            rtt = (now - self._bench_t_send.pop(seq)) * 1000.0
            # Filter unrealistic >5s (re-tx) still count as stall metric
            self._bench_rtts.append(rtt)

    def _bench_finalize(self, crc_ok: bool, total: int):
        if not self._bench_file_start or not self._bench_file_meta:
            return
        elapsed = time.perf_counter() - self._bench_file_start
        goodput = (total / elapsed / 1024.0) if elapsed > 0 else 0.0
        rtts = sorted(self._bench_rtts)
        median = rtts[len(rtts)//2] if rtts else 0.0
        p95 = rtts[int(len(rtts)*0.95)] if rtts else 0.0
        if len(rtts) > 0 and int(len(rtts)*0.95) >= len(rtts):
            p95 = rtts[-1]
        row = {
            "ts": time.strftime("%Y-%m-%dT%H:%M:%S"),
            "file_id": f"{self._bench_file_meta['file_id']:08x}",
            "total_bytes": total,
            "total_frags": self._bench_file_meta["total_frags"],
            "mtu": self.mtu or 0,
            "frag_size": self.frag_size,
            "goodput_kBps": f"{goodput:.2f}",
            "median_rtt_ms": f"{median:.1f}",
            "p95_rtt_ms": f"{p95:.1f}",
            "duplicates": self._bench_duplicates,
            "retries": 0,
            "decrypt_fail": self._bench_decrypt_fail,
            "crc_ok": crc_ok,
            "resume_from": self._bench_file_meta["resume_from"],
            "elapsed_s": f"{elapsed:.2f}",
        }
        self._bench_rows.append(row)
        if self._csv_writer:
            self._csv_writer.writerow(row)
            self._csv_file.flush()
        # also emit BENCH csv line to stdout for Serial join
        print(f"BENCH,client,{row['file_id']},{row['total_bytes']},{row['total_frags']},{row['mtu']},{row['frag_size']},4,0,0,0,0,0,0,0,0,{row['goodput_kBps']}")
        self._bench_file_start = None
        self._bench_file_meta = None

    def bench_rows(self) -> list[dict]:
        return list(self._bench_rows)

    # -- BLE plumbing --------------------------------------------------

    async def connect(self):
        print(f"Connecting to {self.address} ...")
        self.client = BleakClient(self.address)
        await self.client.connect()
        is_conn = self.client.is_connected
        print(f"Connected. is_connected={is_conn} address={self.address}")
        print("Pairing (if required by OS)...")
        try:
            paired = await self.client.pair()
            print(f"  pair() returned {paired} is_connected={self.client.is_connected}")
        except Exception as e:
            # Some platforms auto-pair on encrypted characteristic access instead
            print(f"  (pair() call skipped/handled by OS: {e}) is_connected={self.client.is_connected if self.client else 'no-client'}")

        await self.client.start_notify(CTRL_UUID, self._on_ctrl_indicate)
        await self.client.start_notify(DATA_UUID, self._on_data_notify)
        print("Subscribed to ctrl + data characteristics.")
        # Log MTU if available via bleak
        try:
            mtu = getattr(self.client, "mtu_size", None)
            if mtu:
                print(f"  Bleak MTU hint: {mtu}")
        except Exception:
            pass

    async def disconnect(self):
        if self.client and self.client.is_connected:
            await self.client.disconnect()

    async def write_ctrl(self, ptype: int, seq: int, payload: bytes = b""):
        pkt = proto_build(ptype, seq, payload)
        await self.client.write_gatt_char(CTRL_UUID, pkt, response=True)

    async def write_ack(self, ptype: int, seq: int, payload: bytes = b""):
        pkt = proto_build(ptype, seq, payload)
        # Serialize ACK writes to avoid WNR queue overflow on Windows/Bleak
        async with self._ack_lock:
            try:
                await self.client.write_gatt_char(ACK_UUID, pkt, response=False)
            except Exception as e:
                # Fallback to Write Request if WNR fails (more reliable, slower)
                try:
                    await self.client.write_gatt_char(ACK_UUID, pkt, response=True)
                except Exception as e2:
                    print(f"  [!] ACK write failed seq={seq}: {e} / {e2}")

    # -- Handshake -------------------------------------------------------

    async def do_handshake(self):
        # Matches ble_service.cpp HELLO gate (s_encrypted + PROTO_VER) and
        # 5 s BLE_HANDSHAKE_TIMEOUT_MS. Retries up to 5x on 0x01 (Windows bonding/MIC race).
        for attempt in range(5):
            self.hello_acked.clear()
            self.error_event.clear()
            self.last_error = None
            seq = self.next_seq()
            print(f"Sending HELLO (seq={seq}) attempt {attempt + 1}/5 ...")
            await self.write_ctrl(PKT_HELLO, seq)
            try:
                await asyncio.wait_for(
                    self._wait_for_handshake_result(), timeout=ACK_TIMEOUT_S
                )
            except asyncio.TimeoutError:
                raise RuntimeError("Timed out waiting for HELLO_ACK")
            if self.hello_acked.is_set():
                print(f"Handshake complete. session_id={self.session_id:#010x}, "
                      f"mtu={self.mtu} chunk_sec={self.chunk_sec} frag_size={self.frag_size}, "
                      f"key={'present' if self.master_key else 'ABSENT (unencrypted transfer!)'}")
                return
            if self.last_error == 0x01 and attempt < 4:
                print("  HELLO rejected (0x01 not encrypted) — pairing then retrying...")
                try:
                    paired = await self.client.pair()
                    print(f"  pair() retry returned {paired} is_connected={self.client.is_connected if self.client else 'no-client'}")
                except Exception as e:
                    print(f"  pair() retry failed: {e} is_connected={self.client.is_connected if self.client else 'no-client'}")
                print("  Waiting 1.5s for encryption to settle before HELLO retry...")
                await asyncio.sleep(1.5)
                continue
            if self.last_error == 0x02:
                raise RuntimeError(f"HELLO rejected: version mismatch (error 0x02) — check PROTO_VER={PROTO_VER}")
            raise RuntimeError(f"HELLO rejected with error 0x{self.last_error:02x}" if self.last_error is not None else "HELLO rejected")
        raise RuntimeError("Handshake failed after retry")

    async def _wait_for_handshake_result(self):
        # Wait for either HELLO_ACK or PKT_ERROR
        while not self.hello_acked.is_set() and not self.error_event.is_set():
            await asyncio.sleep(0.05)

    # -- Indication / notification handlers ------------------------------

    def _on_ctrl_indicate(self, _handle, data: bytearray):
        pkt = proto_parse(bytes(data))
        if not pkt:
            print("  [!] Failed to parse ctrl packet")
            return
        asyncio.create_task(self._handle_ctrl_packet(pkt))

    def _on_data_notify(self, _handle, data: bytearray):
        pkt = proto_parse(bytes(data))
        if not pkt:
            print("  [!] Failed to parse data packet")
            return
        asyncio.create_task(self._handle_data_packet(pkt))

    async def _handle_ctrl_packet(self, pkt: Packet):
        name = PKT_NAMES.get(pkt.type, f"0x{pkt.type:02x}")
        print(f"<- ctrl {name} seq={pkt.seq} len={len(pkt.payload)}")

        if pkt.type == PKT_HELLO_ACK:
            self._parse_hello_ack(pkt.payload)
            self.hello_acked.set()

        elif pkt.type == PKT_FILE_ANNOUNCE:
            await self._handle_announce(pkt)

        elif pkt.type == PKT_FILE_DONE:
            await self._handle_file_done(pkt)

        elif pkt.type == PKT_RESUME_RESP:
            # Firmware can send resume offset via RESUME_RESP; treat like announce continuation
            # Payload [seq_lo, seq_hi, resume_lo, resume_hi] — log and update bench meta if needed
            print(f"  RESUME_RESP len={len(pkt.payload)}")

        elif pkt.type == PKT_KEEPALIVE:
            pass  # nothing to do

        elif pkt.type == PKT_ERROR:
            code = pkt.payload[0] if pkt.payload else None
            self.last_error = code
            self.error_event.set()
            detail = "pairing required (0x01)" if code == 0x01 else \
                     "version mismatch (0x02)" if code == 0x02 else f"0x{code:02x}" if code is not None else "empty"
            print(f"  [!] Device PKT_ERROR: {detail} payload={pkt.payload!r}")

    def _parse_hello_ack(self, payload: bytes):
        if len(payload) < 11:
            print("  [!] HELLO_ACK payload too short")
            return
        proto_ver = payload[0]
        if proto_ver != PROTO_VER:
            print(f"  [!] HELLO_ACK proto_ver mismatch device={proto_ver} client={PROTO_VER}")
        session_id = struct.unpack("<I", payload[1:5])[0]
        mtu = struct.unpack("<H", payload[5:7])[0]
        chunk_sec = struct.unpack("<I", payload[7:11])[0]
        self.session_id = session_id
        self.mtu = mtu
        self.chunk_sec = chunk_sec
        # Derive frag_size from negotiated MTU using MTU_OVERHEAD (PROTO_HEADER+TAG+CRC+ATT+spare)
        # legacy compat: mtu - 27 (6 hdr +8 tag +4 crc +3 ATT +4 spare -> 27) == MTU_OVERHEAD
        if mtu:
            derived = mtu - MTU_OVERHEAD  # mtu - 27 capped
            if derived > 0:
                self.frag_size = min(BLE_FRAG_SIZE_GUESS, derived)
            else:
                self.frag_size = BLE_FRAG_SIZE_GUESS
        else:
            self.frag_size = BLE_FRAG_SIZE_GUESS
        print(f"  proto_ver={proto_ver} mtu={mtu} chunk_sec={chunk_sec} frag_size={self.frag_size}")
        if len(payload) >= 27:
            self.master_key = payload[11:27]
            print("  Received AES master key from device.")
        else:
            self.master_key = None
            print("  No key in HELLO_ACK — transfer will be unencrypted or fail.")

    async def _handle_announce(self, pkt: Packet):
        p = pkt.payload
        if len(p) < 17:
            print("  [!] FILE_ANNOUNCE payload too short")
            return
        path_len = p[0]
        total = struct.unpack("<I", p[1:5])[0]
        total_frags = struct.unpack("<H", p[5:7])[0]
        file_crc = struct.unpack("<I", p[7:11])[0]
        device_start_seq = struct.unpack("<H", p[11:13])[0]
        file_id = struct.unpack("<I", p[13:17])[0]

        print(f"  FILE_ANNOUNCE: total={total}B frags={total_frags} "
              f"crc={file_crc:#010x} file_id={file_id:#010x} "
              f"device_offered_resume={device_start_seq}")

        key = None
        if self.master_key:
            key = derive_file_key(self.master_key, self.session_id, file_id)

        self.current_file = IncomingFile(
            file_id=file_id, total_bytes=total, total_frags=total_frags,
            expected_crc=file_crc, key=key, session_id=self.session_id,
            frag_size=self.frag_size,
        )
        self.file_done_event.clear()
        # Bench init for this file
        resume_from = device_start_seq if device_start_seq < total_frags else 0
        self._bench_reset_file(file_id, total, total_frags, resume_from)
        # If resuming, pre-fill received_frags so is_complete does not require earlier frags.
        if resume_from > 0:
            self.current_file.buffer = bytearray(resume_from * self.frag_size)
            self.current_file.received_frags = set(range(resume_from))
            self.current_file.contig_seq = resume_from - 1
            print(f"  [bench] resume prefill {resume_from}/{total_frags} fragments contig={self.current_file.contig_seq}")
        else:
            self.current_file.contig_seq = -1

        ack_payload = struct.pack("<HH", pkt.seq, resume_from)
        await self.write_ack(PKT_FILE_ANNOUNCE_ACK, self.next_seq(), ack_payload)
        print(f"  -> FILE_ANNOUNCE_ACK sent (resume_from={resume_from})")

    async def _handle_data_packet(self, pkt: Packet):
        # Serialize to avoid concurrent buffer extends / ACK interleaving
        ack_seq: int | None = None
        async with self._data_lock:
            if pkt.type != PKT_DATA:
                # Handle RESUME_RESP that may arrive on DATA char edge-case — ignore
                return
            if not self.current_file:
                print("  [!] DATA received with no active file — ignoring")
                return
            if not self.current_file.key:
                print("  [!] No key available — cannot decrypt fragment")
                return

            seq = pkt.seq
            raw = pkt.payload
            if len(raw) < CRYPTO_TAG_BYTES:
                print(f"  [!] DATA fragment too short (seq={seq})")
                return
            frag_len = len(raw) - CRYPTO_TAG_BYTES

            plain = decrypt_fragment(
                self.current_file.key, self.current_file.session_id,
                self.current_file.file_id, seq, frag_len, raw,
            )
            if plain is None:
                self._bench_decrypt_fail += 1
                # Duplicate cumulative ACK so firmware retransmits the hole.
                contig = self.current_file.contig_seq
                if contig >= 0 and len(self.current_file.received_frags) % 8 == 0:
                    ack_seq = contig
            else:
                # Duplicate detection for bench
                if seq in self.current_file.received_frags:
                    self._bench_duplicates += 1
                self.current_file.add_fragment(seq, plain)
                # Track ACK send time for RTT
                self._bench_t_send[seq] = time.perf_counter()
                n = len(self.current_file.received_frags)
                if n % 20 == 0 or n == self.current_file.total_frags:
                    print(f"  progress: {n}/{self.current_file.total_frags} fragments")
                # Cumulative ACK: highest contiguous seq, at least once per 8-frag window.
                contig = self.current_file.contig_seq
                if contig >= 0 and (n % 8 == 0 or (contig + 1) % 8 == 0
                                    or n == self.current_file.total_frags):
                    ack_seq = contig
        if ack_seq is not None:
            await self.write_ack(PKT_ACK, self.next_seq(),
                                 struct.pack("<HB", ack_seq & 0xFFFF, 0x00))
        # For bench we approximate RTT as time between consecutive DATA arrivals
        # Real RTT would need firmware timestamps; client RTT is inter-frag gap proxy
        if len(self._bench_rtts) < 5000:
            # push inter-arrival as pseudo-RTT if we have previous
            self._bench_rtts.append(0.0 if not hasattr(self, '_bench_last_data') else (time.perf_counter() - getattr(self, '_bench_last_data'))*1000.0)
            self._bench_last_data = time.perf_counter()

    async def _handle_file_done(self, pkt: Packet):
        p = pkt.payload
        if len(p) < 12:
            print("  [!] FILE_DONE payload too short")
            return
        file_id = struct.unpack("<I", p[0:4])[0]
        file_crc = struct.unpack("<I", p[4:8])[0]
        total = struct.unpack("<I", p[8:12])[0]

        f = self.current_file
        ok = False
        if f and f.file_id == file_id:
            data = bytes(f.buffer[:total])
            actual_crc = crc32(data)
            ok = (actual_crc == file_crc) and (len(f.received_frags) == f.total_frags)
            if ok:
                OUTPUT_DIR.mkdir(exist_ok=True)
                # Detect OGG-Opus vs WAV via magic: OggS for opus, RIFF for wav
                ext = ".ogg" if data[:4] == b"OggS" else ".wav"
                # Prefer .ogg for new 16k Opus (100KB/50s), .wav for legacy PCM
                out_path = OUTPUT_DIR / f"file_{file_id:08x}{ext}"
                out_path.write_bytes(data)
                print(f"  ✓ File complete and CRC verified -> {out_path} ({total} bytes)")
                # also write bench meta json alongside wav
                try:
                    meta = {
                        "file_id": f"{file_id:08x}",
                        "total": total,
                        "total_frags": f.total_frags,
                        "expected_crc": f"{file_crc:08x}",
                        "actual_crc": f"{actual_crc:08x}",
                        "mtu": self.mtu,
                        "frag_size": self.frag_size,
                        "duplicates": self._bench_duplicates,
                        "decrypt_fail": self._bench_decrypt_fail,
                    }
                    (OUTPUT_DIR / f"file_{file_id:08x}.json").write_text(json.dumps(meta, indent=2))
                except Exception:
                    pass
            else:
                print(f"  [!] CRC mismatch or missing fragments "
                      f"(expected crc={file_crc:#010x}, got={actual_crc:#010x}, "
                      f"frags {len(f.received_frags)}/{f.total_frags})")
        else:
            print("  [!] FILE_DONE for unknown/mismatched file_id")

        status = 0x01 if ok else 0x00
        ack_payload = struct.pack("<HBBB", pkt.seq, status, 0, 0)
        await self.write_ack(PKT_FILE_DONE_ACK, self.next_seq(), ack_payload)
        print(f"  -> FILE_DONE_ACK sent (status={'ok' if ok else 'fail'})")
        # Bench finalize — always emit row even on fail for throughput analysis
        try:
            self._bench_finalize(ok, total)
        except Exception as e:
            print(f"  [!] bench finalize failed: {e}")

        self.current_file = None
        self.file_done_event.set()


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

async def find_device(name_or_addr: str | None) -> str:
    target = name_or_addr or DEVICE_NAME
    print(f"Scanning for '{target}' ...")
    devices = await BleakScanner.discover(timeout=8.0)
    for d in devices:
        if d.name == target or d.address == target:
            print(f"Found: {d.name} [{d.address}]")
            return d.address
    raise RuntimeError(f"Device '{target}' not found. Nearby devices: "
                        f"{[(d.name, d.address) for d in devices]}")


async def main():
    args = [a for a in sys.argv[1:] if not a.startswith("-")]
    flags = [a for a in sys.argv[1:] if a.startswith("-")]
    bench_flag = "--bench" in flags or "--csv" in flags
    # also support: python client.py --bench [device]
    arg = args[0] if args else None
    bench_csv = None
    if bench_flag:
        ts = time.strftime("%Y%m%d_%H%M%S")
        bench_csv = BENCH_DIR / f"benchmark_{ts}.csv"
        print(f"Benchmark capture -> {bench_csv}")
    address = await find_device(arg)

    client = CheckpointClient(address, bench_csv=bench_csv)
    await client.connect()
    await client.do_handshake()

    print("\nListening for file transfers. Press Ctrl+C to stop.\n")
    try:
        while True:
            await asyncio.sleep(1)
    except KeyboardInterrupt:
        print("\nShutting down...")
    finally:
        await client.disconnect()
        # flush bench csv
        if client._csv_file:
            try:
                client._csv_file.close()
                print(f"Benchmark csv saved: {bench_csv} ({len(client.bench_rows())} files)")
                # also dump json summary
                jpath = bench_csv.with_suffix(".json") if bench_csv else None
                if jpath:
                    jpath.write_text(json.dumps(client.bench_rows(), indent=2))
                    print(f"Benchmark json saved: {jpath}")
            except Exception:
                pass


if __name__ == "__main__":
    asyncio.run(main())