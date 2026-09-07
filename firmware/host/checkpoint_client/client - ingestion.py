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
    # For VAD gate (optional, fail-open if missing):
    #   pip install torch torchaudio silero-vad

Usage:
    python client.py [device_name_or_address]
    # Default device: Checkpoint (BLE_DEVICE_NAME in config.h)
    # With benchmark: python client.py --bench   (also writes csv/json)
    # With ingestion: python "client - Copy.py" --bench --ingest
    #   env: INGEST_BASE_URL (default http://localhost:8080)
    #        INGEST_USER_ID  (default dev UUID aaaaaaaa-...; Bearer token)
    #        INGEST_ENABLED  (1/0, default 1)
    #        INGEST_DELETE_AFTER (1/0, default 1 — temp-then-delete)
    #        INGEST_POLL_ENABLED (1/0, default 1 — poll GET until SUBMITTED)
    #        INGEST_POLL_TIMEOUT_S (default 30 — queue-wait for Kafka publish)
    #        INGEST_POLL_INTERVAL_S (default 1.0)
    # flags: --ingest / --no-ingest, --ingest-url <url>, --user-id <uuid>, --keep
    # flags: --no-queue-wait (return on READY, skip SUBMITTED poll)
    #        --queue-wait-timeout <s> (override INGEST_POLL_TIMEOUT_S)
    # flags: --no-rebond (disable automatic unpair+fresh-pair on stale bond)
    # With VAD gate (filter non-speech before upload):
    #   env: VAD_ENABLED (1/0, default 1), VAD_THRESHOLD (default 0.85),
    #        VAD_MIN_SPEECH_S (default 1.5), VAD_MIN_SPEECH_MS (800),
    #        VAD_MIN_SILENCE_MS (500), VAD_PAD_MS (200)
    # flags: --vad / --no-vad, --vad-threshold <f>, --min-speech <seconds>
"""

import asyncio
import csv
import json
import os
import struct
import sys
import time
import urllib.error
import urllib.request
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
# Ingestion service — direct upload (replaces local-only storage)
# Mirrors ingestion-service/postman.json flow:
#   1. POST {base}/v1/uploads {filename, content_type, size_bytes} + Bearer <uuid>
#      -> {upload_id, upload:{method:PUT, url:presigned MinIO}}
#   2. PUT <presignedUrl> raw bytes, Content-Type audio/ogg, no auth
#   3. POST {base}/v1/uploads/{id}/complete {size_bytes, checksum_sha256}
#      -> {upload_id, status:READY} (idempotent; SUBMITTED only on replay)
#   4. Poll GET {base}/v1/uploads/{id} until status SUBMITTED — the outbox
#      dispatcher has published to Kafka topic transcription.jobs.v1 (~2s).
#      SUBMITTED means queued to Kafka, not transcribed. At-least-once:
#      duplicates share the same event_id. Verify with:
#      docker compose exec kafka .../kafka-console-consumer.sh
#        --topic transcription.jobs.v1 --bootstrap-server kafka:9092
# Only audio/ogg accepted (domain/upload.go AllowedContentTypes), 10MB max.
# API-only: this client never talks to Kafka directly (no kafka dep).
# ---------------------------------------------------------------------------

INGEST_BASE_URL = os.getenv("INGEST_BASE_URL", "http://localhost:8080").rstrip("/")
INGEST_USER_ID = os.getenv("INGEST_USER_ID", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
INGEST_ENABLED_DEFAULT = os.getenv("INGEST_ENABLED", "1") == "1"
INGEST_DELETE_AFTER_DEFAULT = os.getenv("INGEST_DELETE_AFTER", "1") == "1"
INGEST_TIMEOUT_S = float(os.getenv("INGEST_TIMEOUT_S", "15"))
INGEST_MAX_BYTES = 10 * 1024 * 1024  # mirrors domain.MaxUploadBytes
# Queue-wait: poll GET until SUBMITTED (Kafka publish). Display-only topic hint.
INGEST_POLL_ENABLED_DEFAULT = os.getenv("INGEST_POLL_ENABLED", "1") == "1"
INGEST_POLL_TIMEOUT_S = float(os.getenv("INGEST_POLL_TIMEOUT_S", "30"))
INGEST_POLL_INTERVAL_S = float(os.getenv("INGEST_POLL_INTERVAL_S", "1.0"))
KAFKA_TOPIC_HINT = os.getenv("KAFKA_TOPIC_TRANSCRIPTION", "transcription.jobs.v1")

# ---------------------------------------------------------------------------
# VAD gate — Silero VAD filters non-speech BEFORE the ingestion upload.
# Sample logic preserved verbatim (OGG repair for firmware's combined
# OpusHead+OpusTags page 0). Fail-open: any VAD error uploads anyway.
# ---------------------------------------------------------------------------

VAD_ENABLED_DEFAULT = os.getenv("VAD_ENABLED", "1") == "1"
VAD_THRESHOLD = float(os.getenv("VAD_THRESHOLD", "0.85"))
VAD_MIN_SPEECH_S = float(os.getenv("VAD_MIN_SPEECH_S", "1.5"))
VAD_MIN_SPEECH_MS = int(os.getenv("VAD_MIN_SPEECH_MS", "800"))
VAD_MIN_SILENCE_MS = int(os.getenv("VAD_MIN_SILENCE_MS", "500"))
VAD_PAD_MS = int(os.getenv("VAD_PAD_MS", "200"))

# Auto-rebond: on persistent HELLO 0x01 (stale OS bond — device lost its LTK,
# e.g. reboot with RAM-only bonds), delete the bond and pair fresh once.
# This automates the manual "unpair in system settings" workaround.
BLE_AUTO_REBOND_DEFAULT = os.getenv("BLE_AUTO_REBOND", "1") == "1"

BENCH_FIELDNAMES = ["ts","file_id","total_bytes","total_frags","mtu","frag_size","goodput_kBps","median_rtt_ms","p95_rtt_ms","duplicates","retries","decrypt_fail","crc_ok","resume_from","elapsed_s","ingest_upload_id","ingest_status","ingest_error","vad_status","vad_speech_s"]

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

# Command IDs in PKT_CMD payload[0] — must match control.h CtrlCmd.
CTRL_CMD_REC_START = 0x01
CTRL_CMD_REC_STOP = 0x02
CTRL_CMD_LED_SET = 0x10
CTRL_CMD_LED_GET = 0x11

# Status codes in PKT_CMD_RESP payload[1] — must match control.h CtrlStatus.
CTRL_OK = 0x00
CTRL_ERR_NOT_READY = 0x01
CTRL_ERR_NO_SD = 0x02
CTRL_ERR_BAD_ARG = 0x03
CTRL_ERR_DENIED = 0x04

CTRL_STATUS_LEN = 16
CTRL_BRIGHT_MIN = 5

PKT_NAMES = {
    PKT_HELLO: "HELLO", PKT_HELLO_ACK: "HELLO_ACK",
    PKT_FILE_ANNOUNCE: "FILE_ANNOUNCE", PKT_FILE_ANNOUNCE_ACK: "FILE_ANNOUNCE_ACK",
    PKT_DATA: "DATA", PKT_ACK: "ACK",
    PKT_FILE_DONE: "FILE_DONE", PKT_FILE_DONE_ACK: "FILE_DONE_ACK",
    PKT_ERROR: "ERROR", PKT_RESUME_REQ: "RESUME_REQ", PKT_RESUME_RESP: "RESUME_RESP",
    PKT_KEEPALIVE: "KEEPALIVE",
    PKT_CMD: "CMD", PKT_CMD_RESP: "CMD_RESP",
    PKT_STATUS_REQ: "STATUS_REQ", PKT_STATUS_RESP: "STATUS_RESP",
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
# Ingestion uploader — stdlib only (urllib in executor so BLE loop never blocks)
# ---------------------------------------------------------------------------

def _http_json(method: str, url: str, body: dict | None, headers: dict,
               timeout: float) -> tuple[int, dict | bytes]:
    """Blocking JSON helper. Returns (status, parsed_json_or_raw)."""
    raw = json.dumps(body).encode() if body is not None else None
    req_headers = dict(headers)
    if body is not None:
        req_headers.setdefault("Content-Type", "application/json")
    req = urllib.request.Request(url, data=raw, headers=req_headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            payload = resp.read()
            status = resp.status
    except urllib.error.HTTPError as e:
        try:
            err_body = e.read().decode(errors="ignore")[:500]
        except Exception:
            err_body = ""
        raise RuntimeError(f"{method} {url} -> HTTP {e.code}: {err_body}")
    ctype = ""
    try:
        ctype = resp.headers.get("Content-Type", "")
    except Exception:
        pass
    if "json" in ctype.lower() or (payload[:1] in (b"{", b"[")):
        try:
            return status, json.loads(payload.decode())
        except Exception:
            return status, payload
    return status, payload


def _http_put_bytes(url: str, data: bytes, content_type: str, timeout: float) -> int:
    req = urllib.request.Request(
        url, data=data,
        headers={"Content-Type": content_type, "Content-Length": str(len(data))},
        method="PUT",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status
    except urllib.error.HTTPError as e:
        try:
            err_body = e.read().decode(errors="ignore")[:500]
        except Exception:
            err_body = ""
        raise RuntimeError(f"PUT presigned -> HTTP {e.code}: {err_body}")


class IngestionUploader:
    """Direct entry-point to ingestion-service (no local retention required).

    Upload is 3-step + queue-wait: create, PUT to MinIO, complete (->READY),
    then poll GET until SUBMITTED (outbox dispatcher published to Kafka).
    """

    def __init__(self, base_url: str = INGEST_BASE_URL, user_id: str = INGEST_USER_ID,
                 timeout: float = INGEST_TIMEOUT_S,
                 poll_enabled: bool = INGEST_POLL_ENABLED_DEFAULT,
                 poll_timeout: float = INGEST_POLL_TIMEOUT_S,
                 poll_interval: float = INGEST_POLL_INTERVAL_S):
        self.base_url = (base_url or INGEST_BASE_URL).rstrip("/")
        self.user_id = user_id or INGEST_USER_ID
        self.timeout = timeout
        self.poll_enabled = poll_enabled
        self.poll_timeout = poll_timeout
        self.poll_interval = poll_interval if poll_interval > 0 else 1.0

    def _auth(self, extra: dict | None = None) -> dict:
        h = {"Authorization": f"Bearer {self.user_id}"}
        if extra:
            h.update(extra)
        return h

    def get_status_sync(self, upload_id: str) -> str:
        """Blocking GET upload status. Returns status string. Raises on failure."""
        status, body = _http_json(
            "GET", f"{self.base_url}/v1/uploads/{upload_id}",
            None, self._auth(), self.timeout,
        )
        if status != 200 or not isinstance(body, dict) or "status" not in body:
            raise RuntimeError(f"get status HTTP {status}: {str(body)[:300]}")
        return str(body.get("status", ""))

    def _wait_submitted(self, upload_id: str) -> tuple[str, float]:
        """Poll GET until SUBMITTED or timeout. Returns (final_status, waited_s)."""
        start = time.monotonic()
        last = "READY"
        deadline = start + max(0.0, self.poll_timeout)
        while time.monotonic() < deadline:
            time.sleep(self.poll_interval)
            try:
                last = self.get_status_sync(upload_id) or last
            except Exception:
                continue
            if last == "SUBMITTED":
                break
            if last not in ("READY", "SUBMITTED", "UPLOADING"):
                break
        return last, time.monotonic() - start

    def upload_sync(self, data: bytes, filename: str,
                    content_type: str = "audio/ogg",
                    idempotency_key: str | None = None) -> tuple[str, str]:
        """Blocking 3-step flow + queue-wait. Returns (upload_id, status). Raises on failure."""
        size = len(data)
        if size > INGEST_MAX_BYTES:
            raise RuntimeError(f"too-large: {size} > {INGEST_MAX_BYTES}")
        # 1. Create
        headers = self._auth()
        if idempotency_key:
            headers["Idempotency-Key"] = idempotency_key[:128]
        status, body = _http_json(
            "POST", f"{self.base_url}/v1/uploads",
            {"filename": filename, "content_type": content_type, "size_bytes": size},
            headers, self.timeout,
        )
        if not isinstance(body, dict) or "upload_id" not in body or "upload" not in body:
            raise RuntimeError(f"create: unexpected response HTTP {status}: {str(body)[:300]}")
        upload_id = body["upload_id"]
        put_url = body["upload"]["url"]
        # 2. PUT bytes to MinIO (no auth — presigned URL is the credential)
        put_status = _http_put_bytes(put_url, data, content_type, timeout=max(self.timeout, 30))
        if put_status not in (200, 201, 204):
            raise RuntimeError(f"PUT presigned -> HTTP {put_status}")
        # 3. Complete (idempotent — safe to resend). Include sha256 like postman size check.
        checksum = hashlib.sha256(data).hexdigest()
        c_status, c_body = _http_json(
            "POST", f"{self.base_url}/v1/uploads/{upload_id}/complete",
            {"size_bytes": size, "checksum_sha256": checksum},
            self._auth(), self.timeout,
        )
        c_status_str = c_body.get("status", "") if isinstance(c_body, dict) else ""
        if c_status != 200 or c_status_str not in ("READY", "SUBMITTED"):
            raise RuntimeError(f"complete HTTP {c_status}: {str(c_body)[:300]}")
        if c_status_str == "SUBMITTED" or not self.poll_enabled:
            return upload_id, c_status_str
        # 4. Queue-wait: complete returned READY, dispatcher publishes async.
        final, _waited = self._wait_submitted(upload_id)
        return upload_id, final

    async def upload_async(self, data: bytes, filename: str,
                           content_type: str = "audio/ogg",
                           idempotency_key: str | None = None) -> tuple[str, str]:
        loop = asyncio.get_event_loop()
        return await loop.run_in_executor(
            None, self.upload_sync, data, filename, content_type, idempotency_key
        )


# ---------------------------------------------------------------------------
# VAD gate — Silero VAD + OGG repair (sample logic, runs in executor)
# ---------------------------------------------------------------------------

def ogg_crc(data) -> int:
    crc = 0
    for byte in data:
        crc ^= byte << 24
        for _ in range(8):
            if crc & 0x80000000:
                crc = ((crc << 1) & 0xFFFFFFFF) ^ 0x04C11DB7
            else:
                crc = (crc << 1) & 0xFFFFFFFF
    return crc


def make_ogg_page(header_type, granule_position, serial, sequence, segments, payload) -> bytes:
    page = bytearray()
    page += b"OggS"
    page += b"\x00"
    page += bytes([header_type])
    page += struct.pack("<Q", granule_position)
    page += struct.pack("<I", serial)
    page += struct.pack("<I", sequence)
    page += b"\x00\x00\x00\x00"  # CRC placeholder
    page += bytes([len(segments)])
    page += bytes(segments)
    page += payload
    crc = ogg_crc(page)
    page[22:26] = struct.pack("<I", crc)
    return bytes(page)


def parse_ogg_pages(data: bytes) -> list:
    pages = []
    pos = 0
    while pos < len(data):
        if data[pos:pos + 4] != b"OggS":
            raise ValueError(f"Invalid OGG page at byte {pos}")
        n_segments = data[pos + 26]
        table_start = pos + 27
        table_end = table_start + n_segments
        segments = data[table_start:table_end]
        payload_size = sum(segments)
        page_end = table_end + payload_size
        pages.append(data[pos:page_end])
        pos = page_end
    return pages


def repair_opus_ogg(data: bytes) -> bytes:
    """Split firmware's combined OpusHead+OpusTags page 0 into two pages, in RAM."""
    pages = parse_ogg_pages(data)
    if not pages:
        raise ValueError("No OGG pages found")
    first = pages[0]
    header_type = first[5]
    serial = struct.unpack("<I", first[14:18])[0]
    n_segments = first[26]
    segments = list(first[27:27 + n_segments])
    payload_start = 27 + n_segments
    payload = first[payload_start:]
    # Already normal?
    if payload.startswith(b"OpusHead") and len(segments) == 1:
        return data
    if len(segments) < 2:
        raise ValueError(f"Unexpected OGG layout: {segments}")
    head_size = segments[0]
    tags_size = segments[1]
    opus_head = payload[:head_size]
    opus_tags = payload[head_size:head_size + tags_size]
    if not opus_head.startswith(b"OpusHead"):
        raise ValueError("OpusHead not found")
    if not opus_tags.startswith(b"OpusTags"):
        raise ValueError("OpusTags not found")
    head_page = make_ogg_page(0x02, 0, serial, 0, [head_size], opus_head)
    tags_page = make_ogg_page(0x00, 0, serial, 1, [tags_size], opus_tags)
    fixed_pages = [head_page, tags_page]
    for original in pages[1:]:
        page = bytearray(original)
        page_serial = struct.unpack("<I", page[14:18])[0]
        if page_serial == serial:
            old_sequence = struct.unpack("<I", page[18:22])[0]
            page[18:22] = struct.pack("<I", old_sequence + 1)
            page[22:26] = b"\x00\x00\x00\x00"
            page[22:26] = struct.pack("<I", ogg_crc(page))
        fixed_pages.append(bytes(page))
    return b"".join(fixed_pages)


def vad_prewarm() -> None:
    """Import the torch stack BEFORE any BLE/WinRT activity (call before scan).

    A WinRT Bluetooth scan poisons c10.dll loading afterwards (deterministic
    WinError 1114, any thread), so torch/torchaudio/silero_vad must be
    imported first. Import-only: no weights, safe to call early. Raises
    RuntimeError with install hint when packages are absent.
    """
    try:
        import torch  # noqa: F401
        import torchaudio  # noqa: F401
        import silero_vad  # noqa: F401
    except ModuleNotFoundError as e:
        raise RuntimeError(
            f"VAD package missing (pip install silero-vad torch torchaudio): {e}"
        )


def vad_load_model(retries: int = 2):
    """Load Silero VAD once at startup. Retries transient native failures.

    A first-import WinError 1114 on c10.dll (transient Windows DLL lock) is
    retried after a short sleep instead of giving up. Missing packages raise
    immediately with an install hint; native load failures raise the real
    cause (not "silero_vad missing"). Fail-open decision stays with caller.
    """
    import time as _time
    last: Exception | None = None
    for attempt in range(max(1, retries)):
        try:
            from silero_vad import load_silero_vad
            return load_silero_vad()
        except ModuleNotFoundError as e:
            raise RuntimeError(
                f"VAD package missing (pip install silero-vad torch torchaudio): {e}"
            )
        except Exception as e:
            last = e
            if attempt + 1 < max(1, retries):
                print(f"[vad] load attempt {attempt + 1} failed ({e}) — retrying in 3s ...")
                _time.sleep(3)
    raise RuntimeError(f"VAD model load failed after retries: {last}")


def _decode_ogg_to_tensor(repaired: bytes):
    """Decode (repaired) Opus OGG bytes to (mono Tensor, sample_rate).

    Tries torchaudio/torchcodec first, then ffmpeg subprocess (Opus-capable,
    present on this host), then soundfile. Raises RuntimeError with combined
    detail if every backend fails.
    """
    import io as _io
    errors = []
    # 1. torchaudio (needs torchcodec since torchaudio 2.9)
    try:
        import torchaudio
        waveform, sample_rate = torchaudio.load(_io.BytesIO(repaired))
        if waveform.shape[0] > 1:
            waveform = waveform.mean(dim=0)
        else:
            waveform = waveform.squeeze(0)
        return waveform, sample_rate
    except Exception as e:
        errors.append(f"torchaudio: {e}")
    # 2. ffmpeg subprocess -> 16k mono f32le (handles Opus OGG reliably)
    try:
        import shutil
        import subprocess
        exe = shutil.which("ffmpeg")
        if exe is None:
            raise RuntimeError("ffmpeg not on PATH")
        proc = subprocess.run(
            [exe, "-hide_banner", "-loglevel", "error",
             "-i", "pipe:0", "-ar", "16000", "-ac", "1",
             "-f", "f32le", "-acodec", "pcm_f32le", "pipe:1"],
            input=repaired, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            timeout=30,
        )
        if proc.returncode != 0:
            raise RuntimeError(proc.stderr.decode(errors="ignore")[:300])
        if not proc.stdout or len(proc.stdout) < 4:
            raise RuntimeError("ffmpeg produced no audio")
        import torch
        import numpy as np
        pcm = np.frombuffer(proc.stdout, dtype=np.float32).copy()
        return torch.from_numpy(pcm), 16000
    except Exception as e:
        errors.append(f"ffmpeg: {e}")
    # 3. soundfile (if installed; Vorbis ok, Opus depends on libsndfile build)
    try:
        import soundfile as sf
        import torch
        wav, sr = sf.read(_io.BytesIO(repaired), dtype="float32", always_2d=True)
        import numpy as np
        mono = np.mean(wav, axis=1).astype("float32")
        return torch.from_numpy(mono), sr
    except Exception as e:
        errors.append(f"soundfile: {e}")
    raise RuntimeError("OGG decode failed [" + " | ".join(errors) + "]")


def vad_has_speech(data: bytes, model, threshold: float = VAD_THRESHOLD,
                   min_speech_s: float = VAD_MIN_SPEECH_S) -> tuple:
    """Blocking VAD check. Returns (vad_status, total_speech_s, segments).

    vad_status: 'speech' | 'no-speech' | 'error'. Never raises — errors
    map to 'error' so the caller can fail-open to upload.
    """
    try:
        from silero_vad import get_speech_timestamps
        import torchaudio
        repaired = repair_opus_ogg(data)
        waveform, sample_rate = _decode_ogg_to_tensor(repaired)
        if sample_rate != 16000:
            waveform = torchaudio.functional.resample(waveform, sample_rate, 16000)
        stamps = get_speech_timestamps(
            waveform, model, sampling_rate=16000, threshold=threshold,
            min_speech_duration_ms=VAD_MIN_SPEECH_MS,
            min_silence_duration_ms=VAD_MIN_SILENCE_MS,
            speech_pad_ms=VAD_PAD_MS, return_seconds=True,
        )
        total = sum(s["end"] - s["start"] for s in stamps)
        if total >= min_speech_s:
            return "speech", float(total), stamps
        return "no-speech", float(total), stamps
    except Exception as e:
        return "error", 0.0, [{"error": str(e)[:200]}]


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
    def __init__(self, address: str, bench_csv: Path | None = None,
                 ingest_enabled: bool = INGEST_ENABLED_DEFAULT,
                 ingest_base_url: str = INGEST_BASE_URL,
                 ingest_user_id: str = INGEST_USER_ID,
                 ingest_delete_after: bool = INGEST_DELETE_AFTER_DEFAULT,
                 ingest_poll_enabled: bool = INGEST_POLL_ENABLED_DEFAULT,
                 ingest_poll_timeout: float = INGEST_POLL_TIMEOUT_S,
                 ingest_poll_interval: float = INGEST_POLL_INTERVAL_S,
                 vad_enabled: bool = VAD_ENABLED_DEFAULT,
                 vad_model=None,
                 vad_threshold: float = VAD_THRESHOLD,
                 vad_min_speech_s: float = VAD_MIN_SPEECH_S,
                 auto_rebond: bool = BLE_AUTO_REBOND_DEFAULT,
                 on_event=None):
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
        # Pending control responses keyed by request seq (CMD_RESP / STATUS_RESP).
        self._ctrl_pending: dict[int, asyncio.Future] = {}
        # Serialize DATA handling and ACK writes to avoid concurrent buffer corruption
        # and ATT WNR overflow (previously create_task per DATA caused out-of-order ACKs)
        self._data_lock = asyncio.Lock()
        self._ack_lock = asyncio.Lock()
        # Ingestion direct-upload (decoupled from BLE ACK — see _handle_file_done)
        self.ingest_enabled = ingest_enabled
        self.ingest_delete_after = ingest_delete_after
        self.uploader = IngestionUploader(
            base_url=ingest_base_url, user_id=ingest_user_id,
            poll_enabled=ingest_poll_enabled,
            poll_timeout=ingest_poll_timeout,
            poll_interval=ingest_poll_interval,
        )
        if self.ingest_enabled:
            qw = f"queue-wait={self.uploader.poll_enabled} timeout={self.uploader.poll_timeout}s"
            print(f"[ingest] enabled -> {self.uploader.base_url} user={self.uploader.user_id[:8]}... delete_after={self.ingest_delete_after} {qw} topic={KAFKA_TOPIC_HINT}")
        else:
            print("[ingest] disabled — files stay in ./received/")
        # VAD gate (Silero, loaded once at startup; fail-open if missing)
        self.vad_enabled = vad_enabled
        self.vad_model = vad_model
        self.vad_threshold = vad_threshold
        self.vad_min_speech_s = vad_min_speech_s
        if self.vad_enabled:
            state = "loaded" if self.vad_model is not None else "MISSING-fail-open"
            print(f"[vad] enabled ({state}) threshold={self.vad_threshold} min_speech={self.vad_min_speech_s}s")
        else:
            print("[vad] disabled — all OGG go straight to ingestion")
        # Bench metrics — per-file + aggregate csv
        self.bench_csv = bench_csv
        # Optional GUI/event callback: on_event(dict). Never raises into BLE path.
        self.on_event = on_event
        # Auto-rebond on persistent 0x01 (stale bond). Disable with --no-rebond.
        self.auto_rebond = auto_rebond
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
            self._csv_writer = csv.DictWriter(self._csv_file, fieldnames=BENCH_FIELDNAMES)
            self._csv_writer.writeheader()
            self._csv_file.flush()

    def next_seq(self) -> int:
        s = self._seq_gen
        self._seq_gen = (self._seq_gen + 1) & 0xFFFF
        return s

    def _emit(self, evt: dict):
        """Fire GUI/event callback without breaking the BLE path."""
        cb = getattr(self, "on_event", None)
        if cb is None:
            return
        try:
            cb(evt)
        except Exception:
            pass

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

    def _bench_finalize(self, crc_ok: bool, total: int,
                        ingest_upload_id: str = "", ingest_status: str = "",
                        ingest_error: str = "", vad_status: str = "",
                        vad_speech_s: str = ""):
        if not self._bench_file_start or not self._bench_file_meta:
            return None
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
            "ingest_upload_id": ingest_upload_id,
            "ingest_status": ingest_status,
            "ingest_error": ingest_error,
            "vad_status": vad_status,
            "vad_speech_s": vad_speech_s,
        }
        self._bench_rows.append(row)
        if self._csv_writer:
            self._csv_writer.writerow(row)
            self._csv_file.flush()
        # also emit BENCH csv line to stdout for Serial join
        print(f"BENCH,client,{row['file_id']},{row['total_bytes']},{row['total_frags']},{row['mtu']},{row['frag_size']},4,0,0,0,0,0,0,0,0,{row['goodput_kBps']}")
        self._bench_file_start = None
        self._bench_file_meta = None
        return row

    def _bench_update_ingest(self, file_id_hex: str, upload_id: str,
                             status: str, error: str = "",
                             vad_status: str | None = None,
                             vad_speech_s: str | None = None):
        """Mutate in-memory bench row + rewrite CSV so final ingest result persists.

        Called from the decoupled background upload task (event-loop thread,
        so no lock needed vs _bench_finalize which runs on the same loop).
        """
        for r in self._bench_rows:
            if r.get("file_id") == file_id_hex:
                r["ingest_upload_id"] = upload_id
                r["ingest_status"] = status
                r["ingest_error"] = error[:200] if error else ""
                if vad_status is not None:
                    r["vad_status"] = vad_status
                if vad_speech_s is not None:
                    r["vad_speech_s"] = vad_speech_s
                break
        else:
            return
        self._emit({"type": "ingest", "file_id": file_id_hex, "upload_id": upload_id,
                    "ingest_status": status, "ingest_error": error[:200] if error else "",
                    "vad_status": vad_status, "vad_speech_s": vad_speech_s})
        if self._csv_file and self._csv_writer:
            try:
                self._csv_file.seek(0)
                self._csv_file.truncate(0)
                self._csv_writer = csv.DictWriter(self._csv_file, fieldnames=BENCH_FIELDNAMES)
                self._csv_writer.writeheader()
                self._csv_writer.writerows(self._bench_rows)
                self._csv_file.flush()
            except Exception as e:
                print(f"  [!] bench csv rewrite failed: {e}")

    def _bench_rewrite_csv(self):
        """Rewrite CSV from _bench_rows (used at shutdown to flush async updates)."""
        if not self.bench_csv or not self._bench_rows:
            return
        try:
            if self._csv_file:
                try:
                    self._csv_file.close()
                except Exception:
                    pass
                self._csv_file = None
                self._csv_writer = None
            with self.bench_csv.open("w", newline="", encoding="utf-8") as f:
                w = csv.DictWriter(f, fieldnames=BENCH_FIELDNAMES)
                w.writeheader()
                w.writerows(self._bench_rows)
        except Exception as e:
            print(f"  [!] bench csv final rewrite failed: {e}")

    def bench_rows(self) -> list[dict]:
        return list(self._bench_rows)

    async def _ingest_in_background(self, data: bytes, out_path: Path,
                                    meta_path: Path, file_id: int):
        """Decoupled VAD gate + upload: BLE already ACKed ok. Filter, upload, temp-delete."""
        file_hex = f"{file_id:08x}"
        filename = out_path.name
        idem_key = f"{self.session_id:08x}-{file_hex}" if self.session_id is not None else file_hex
        # -- VAD gate (fail-open): only speech reaches the ingestion API --
        vad_status = "disabled"
        vad_speech = 0.0
        if self.vad_enabled:
            if self.vad_model is None:
                vad_status = "model-missing-fail-open"
                print(f"  [vad] {filename}: model unavailable — fail-open to upload")
            else:
                print(f"  [vad] checking {filename} ({len(data)}B) threshold={self.vad_threshold} min={self.vad_min_speech_s}s ...")
                try:
                    loop = asyncio.get_event_loop()
                    vad_status, vad_speech, segments = await loop.run_in_executor(
                        None, vad_has_speech, data, self.vad_model,
                        self.vad_threshold, self.vad_min_speech_s,
                    )
                except Exception as e:
                    vad_status, vad_speech, segments = "error", 0.0, [{"error": str(e)[:200]}]
                print(f"VAD,{file_hex},{vad_status},{vad_speech:.2f}")
                self._emit({"type": "vad", "file_id": file_hex, "vad_status": vad_status,
                            "vad_speech_s": round(vad_speech, 2)})
                if isinstance(segments, list) and segments and isinstance(segments[0], dict) and "error" in segments[0]:
                    print(f"  [vad] detail: {segments[0]['error']}")
                    try:
                        meta_err = {}
                        if meta_path.exists():
                            meta_err = json.loads(meta_path.read_text())
                        meta_err.update({"vad_status": vad_status, "vad_error": segments[0]["error"]})
                        meta_path.write_text(json.dumps(meta_err, indent=2))
                    except Exception:
                        pass
                if vad_status == "no-speech":
                    msg = f"no human speech ({vad_speech:.2f}s < {self.vad_min_speech_s}s)"
                    print(f"  [vad] filtered {filename}: {msg} — skipping upload")
                    print(f"INGEST,{file_hex},,skipped-no-speech,{msg}")
                    self._bench_update_ingest(file_hex, "", "skipped-no-speech", msg,
                                              vad_status, f"{vad_speech:.2f}")
                    try:
                        meta = {}
                        if meta_path.exists():
                            meta = json.loads(meta_path.read_text())
                        meta.update({"vad_status": vad_status, "vad_speech_s": round(vad_speech, 2),
                                     "ingest_status": "skipped-no-speech"})
                        meta_path.write_text(json.dumps(meta, indent=2))
                    except Exception:
                        pass
                    if self.ingest_delete_after and "--keep" not in sys.argv:
                        try:
                            out_path.unlink(missing_ok=True)
                            meta_path.unlink(missing_ok=True)
                            print(f"  [vad] temp deleted (no-speech) {out_path.name}")
                        except Exception as e:
                            print(f"  [!] vad temp delete failed: {e}")
                    else:
                        print(f"  [vad] kept (no-speech, --keep) {out_path.name}")
                    return
                if vad_status == "error":
                    print(f"  [vad] error on {filename} — fail-open to upload")
                else:
                    print(f"  [vad] speech {vad_speech:.2f}s in {filename} — uploading")
        print(f"  [ingest] uploading {filename} ({len(data)}B) -> {self.uploader.base_url} ...")
        try:
            upload_id, status = await self.uploader.upload_async(
                data, filename, "audio/ogg", idem_key
            )
            if status == "SUBMITTED":
                print(f"  [ingest] OK {filename} -> upload_id={upload_id} status=SUBMITTED (queued to Kafka {KAFKA_TOPIC_HINT})")
                print(f"INGEST,{file_hex},{upload_id},SUBMITTED,,")
                self._bench_update_ingest(file_hex, upload_id, status, "",
                                          vad_status, f"{vad_speech:.2f}")
            else:
                # READY on queue-wait timeout: server durable (MinIO+Postgres),
                # dispatcher will still publish. Temp files deleted per policy.
                # Check outbox_events.last_error if stuck; verify with:
                # docker compose exec kafka .../kafka-console-consumer.sh
                #   --topic <topic> --bootstrap-server kafka:9092
                msg = f"queued-timeout after {self.uploader.poll_timeout:.0f}s, Kafka publish pending (upload durable, check outbox)"
                print(f"  [ingest] OK {filename} -> upload_id={upload_id} status=READY ({msg})")
                print(f"INGEST,{file_hex},{upload_id},READY,{msg},")
                self._bench_update_ingest(file_hex, upload_id, "READY", msg,
                                          vad_status, f"{vad_speech:.2f}")
            try:
                meta = {}
                if meta_path.exists():
                    meta = json.loads(meta_path.read_text())
                meta.update({"ingest_upload_id": upload_id, "ingest_status": status,
                             "kafka_topic": KAFKA_TOPIC_HINT,
                             "vad_status": vad_status, "vad_speech_s": round(vad_speech, 2)})
                meta_path.write_text(json.dumps(meta, indent=2))
            except Exception:
                pass
            if self.ingest_delete_after:
                try:
                    out_path.unlink(missing_ok=True)
                    meta_path.unlink(missing_ok=True)
                    print(f"  [ingest] temp deleted {out_path.name}")
                except Exception as e:
                    print(f"  [!] ingest temp delete failed: {e}")
        except Exception as e:
            err = str(e)[:200]
            print(f"  [!] ingest failed {filename}: {err} (kept {out_path})")
            print(f"INGEST,{file_hex},,,{err}")
            self._bench_update_ingest(file_hex, "", "failed", err,
                                      vad_status, f"{vad_speech:.2f}")

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

    async def disconnect_graceful(self, wait_pending_s: float = 10.0):
        """Graceful shutdown that PRESERVES bonding (never unpairs).

        Stops notifications first so the firmware stops sending, waits for
        decoupled VAD/upload tasks to finish, then disconnects. The OS bond
        is kept, so the next Connect normally reuses it without re-pairing.
        (If the device drops its side of the bond, do_handshake() rebonds
        automatically — see _rebond.)
        Queue-wait polling runs inside upload_async, so pending covers it;
        the wait extends to poll_timeout+10s when queue-wait is enabled.
        """
        try:
            if self.client and self.client.is_connected:
                for uuid in (CTRL_UUID, DATA_UUID):
                    try:
                        await self.client.stop_notify(uuid)
                    except Exception:
                        pass
        except Exception:
            pass
        try:
            if self.ingest_enabled:
                budget = wait_pending_s
                try:
                    if self.uploader.poll_enabled:
                        budget = max(budget, self.uploader.poll_timeout + 10.0)
                except Exception:
                    pass
                pending = [r for r in self._bench_rows if r.get("ingest_status") == "pending"]
                if pending:
                    import asyncio as _aio
                    for _ in range(int(budget * 10)):
                        await _aio.sleep(0.1)
                        if not any(r.get("ingest_status") == "pending" for r in self._bench_rows):
                            break
        except Exception:
            pass
        await self.disconnect()
        try:
            self._bench_rewrite_csv()
        except Exception:
            pass

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

    # -- Remote transport + LED control (control.h) ---------------------

    def _ctrl_complete(self, seq: int, result):
        fut = self._ctrl_pending.pop(seq, None)
        if fut is not None and not fut.done():
            fut.set_result(result)

    async def _ctrl_roundtrip(self, ptype: int, payload: bytes, timeout: float = 5.0):
        seq = self.next_seq()
        loop = asyncio.get_event_loop()
        fut = loop.create_future()
        self._ctrl_pending[seq] = fut
        try:
            await self.write_ctrl(ptype, seq, payload)
            return await asyncio.wait_for(fut, timeout=timeout)
        finally:
            self._ctrl_pending.pop(seq, None)

    async def cmd_rec_start(self, timeout: float = 5.0) -> int:
        """Ask device to start recording. Returns CTRL_* status code."""
        res = await self._ctrl_roundtrip(PKT_CMD, bytes([CTRL_CMD_REC_START]), timeout)
        return int(res.get("status", CTRL_ERR_NOT_READY))

    async def cmd_rec_stop(self, timeout: float = 5.0) -> int:
        """Ask device to stop recording. Returns CTRL_* status code."""
        res = await self._ctrl_roundtrip(PKT_CMD, bytes([CTRL_CMD_REC_STOP]), timeout)
        return int(res.get("status", CTRL_ERR_NOT_READY))

    async def cmd_led_set(self, muted: bool, brightness: int, timeout: float = 5.0) -> int:
        """Set LED muted + brightness (0-255). Returns CTRL_* status code."""
        bright = max(0, min(255, int(brightness)))
        if not muted and bright < CTRL_BRIGHT_MIN:
            bright = CTRL_BRIGHT_MIN
        res = await self._ctrl_roundtrip(
            PKT_CMD, bytes([CTRL_CMD_LED_SET, 0x01 if muted else 0x00, bright]), timeout)
        return int(res.get("status", CTRL_ERR_NOT_READY))

    async def cmd_led_get(self, timeout: float = 5.0) -> dict:
        """Returns {status, muted, brightness}."""
        res = await self._ctrl_roundtrip(PKT_CMD, bytes([CTRL_CMD_LED_GET]), timeout)
        return res

    async def req_status(self, timeout: float = 5.0) -> dict:
        """Returns device status dict (recording, vad_*, muted, brightness, ...)."""
        res = await self._ctrl_roundtrip(PKT_STATUS_REQ, b"", timeout)
        return res

    @staticmethod
    def parse_status(payload: bytes) -> dict:
        """Parse 16-byte STATUS_RESP payload into a dict (see control.h layout)."""
        if len(payload) < CTRL_STATUS_LEN:
            return {}
        chunks = struct.unpack("<I", payload[8:12])[0]
        utt = struct.unpack("<I", payload[12:16])[0]
        pend = struct.unpack("<H", payload[6:8])[0]
        level = struct.unpack("b", payload[5:6])[0]
        return {
            "recording": bool(payload[0]),
            "vad_active": bool(payload[1]),
            "vad_speech": bool(payload[2]),
            "muted": bool(payload[3]),
            "brightness": payload[4],
            "level_dbfs": level,
            "pending": pend,
            "chunks": chunks,
            "utterances": utt,
        }

    # -- Handshake -------------------------------------------------------

    async def _rebond(self) -> bool:
        """Delete a stale OS bond and pair fresh. Fixes persistent HELLO 0x01.

        When the device loses its LTK (e.g. reboot with RAM-only bonds) while
        Windows still holds its side, every reconnect comes up unencrypted and
        pair() is a no-op — the manual fix is unpairing in system settings.
        This automates it. Returns True if HELLO should be retried.
        Never raises.
        """
        c = self.client
        if c is None or not hasattr(c, "unpair"):
            print("  [bond] unpair() unavailable — cannot rebond")
            return False
        try:
            print("  [bond] removing stale bond (unpair) ...")
            await c.unpair()
            print("  [bond] unpair ok")
        except Exception as e:
            print(f"  [bond] unpair failed: {e}")
            return False
        try:
            await self.disconnect()
        except Exception:
            pass
        await asyncio.sleep(1.0)
        try:
            await self.connect()  # pairs + resubscribes
        except Exception as e:
            print(f"  [bond] reconnect after unpair failed: {e}")
            return False
        print("  Waiting 2s for encryption to settle before HELLO retry...")
        await asyncio.sleep(2.0)
        return True

    async def do_handshake(self):
        # Matches ble_service.cpp HELLO gate (s_encrypted + PROTO_VER) and
        # 5 s BLE_HANDSHAKE_TIMEOUT_MS. pair() retries cover the first-connect
        # Windows bonding/MIC race; one unpair+fresh-pair covers a stale bond.
        rebonded = False
        for attempt in range(7):
            self.hello_acked.clear()
            self.error_event.clear()
            self.last_error = None
            seq = self.next_seq()
            print(f"Sending HELLO (seq={seq}) attempt {attempt + 1}/7 ...")
            try:
                await self.write_ctrl(PKT_HELLO, seq)
            except Exception as e:
                if "not connected" in str(e).lower() or "disconnected" in str(e).lower():
                    print(f"  link dropped ({e}) — reconnecting ...")
                    try:
                        await self.connect()
                        continue
                    except Exception as e2:
                        raise RuntimeError(f"Reconnect failed: {e2}")
                raise
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
            if self.last_error == 0x01 and attempt < 6:
                if self.auto_rebond and not rebonded and attempt >= 1:
                    print("  HELLO rejected (0x01) persists after pair() — stale bond suspected, rebonding ...")
                    rebonded = True
                    if await self._rebond():
                        continue
                    print("  rebond failed/unavailable — falling back to pair() retry...")
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

        elif pkt.type == PKT_CMD_RESP:
            p = pkt.payload
            if len(p) >= 2:
                cmd, status = p[0], p[1]
                result: dict = {"cmd": cmd, "status": status}
                if cmd == CTRL_CMD_LED_GET and len(p) >= 4:
                    result.update({"muted": bool(p[2]), "brightness": p[3]})
                print(f"  CMD_RESP cmd=0x{cmd:02x} status={status}")
                self._ctrl_complete(pkt.seq, result)
                self._emit({"type": "cmd_resp", "cmd": cmd, "status": status,
                            **({} if "muted" not in result else
                               {"muted": result["muted"], "brightness": result["brightness"]})})

        elif pkt.type == PKT_STATUS_RESP:
            info = self.parse_status(pkt.payload)
            if info:
                print(f"  STATUS_RESP rec={info['recording']} vad={info['vad_active']}/{info['vad_speech']} "
                      f"muted={info['muted']} bright={info['brightness']} pend={info['pending']}")
                self._ctrl_complete(pkt.seq, info)
                self._emit({"type": "rec_status", **info})

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
        self._emit({"type": "announce", "file_id": f"{file_id:08x}",
                    "total_bytes": total, "total_frags": total_frags})

    async def _handle_data_packet(self, pkt: Packet):
        # Serialize to avoid concurrent buffer extends / ACK interleaving
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
                # Fire-and-forget: no per-frag NACK, just count (file will retry on FILE_DONE CRC fail)
                # struct.pack("<HB", seq, 0x01) kept for test compat
                return

            # Duplicate detection for bench
            if seq in self.current_file.received_frags:
                self._bench_duplicates += 1
            self.current_file.add_fragment(seq, plain)
            status = 0x00  # ok
            # Track ACK send time for RTT (no per-frag ACK in blast mode)
            self._bench_t_send[seq] = time.perf_counter()
            # Fire-and-forget: no per-frag PKT_ACK, only FILE_DONE_ACK at end
            # Device blasts without halt, relies on FILE_DONE CRC + full retry
            # struct.pack("<HB", seq, status) kept for test compat
            # Cumulative ACK removed: ack_seq = contig_seq no longer sent

            n = len(self.current_file.received_frags)
            if n % 20 == 0 or n == self.current_file.total_frags:
                print(f"  progress: {n}/{self.current_file.total_frags} fragments")
                self._emit({"type": "progress", "file_id": f"{self.current_file.file_id:08x}",
                            "received": n, "total_frags": self.current_file.total_frags})
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
        data = b""
        out_path: Path | None = None
        meta_path: Path | None = None
        is_ogg = False
        if f and f.file_id == file_id:
            data = bytes(f.buffer[:total])
            actual_crc = crc32(data)
            ok = (actual_crc == file_crc) and (len(f.received_frags) == f.total_frags)
            if ok:
                OUTPUT_DIR.mkdir(exist_ok=True)
                # Detect OGG-Opus vs WAV via magic: OggS for opus, RIFF for wav
                ext = ".ogg" if data[:4] == b"OggS" else ".wav"
                is_ogg = (ext == ".ogg")
                # Prefer .ogg for new 16k Opus (100KB/50s), .wav for legacy PCM
                out_path = OUTPUT_DIR / f"file_{file_id:08x}{ext}"
                meta_path = OUTPUT_DIR / f"file_{file_id:08x}.json"
                # Temp write: deleted after successful ingestion when enabled
                out_path.write_bytes(data)
                print(f"  ✓ File complete and CRC verified -> {out_path} ({total} bytes)")
                # also write bench meta json alongside (updated with ingest result later)
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
                    meta_path.write_text(json.dumps(meta, indent=2))
                except Exception:
                    pass
                if not is_ogg:
                    print(f"  [ingest] skip {out_path.name}: WAV not accepted by ingestion (audio/ogg only) — kept on disk")
                elif total > INGEST_MAX_BYTES:
                    print(f"  [ingest] skip {out_path.name}: {total}B > {INGEST_MAX_BYTES}B max — kept on disk")
                elif not self.ingest_enabled:
                    print(f"  [ingest] disabled — kept {out_path.name} on disk")
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
        # Bench finalize — always emit row even on fail for throughput analysis.
        # Decoupled: BLE ACK already sent; VAD + ingestion run in background after this.
        ingest_init_status = ""
        vad_init_status = ""
        if ok and out_path is not None:
            if not is_ogg:
                ingest_init_status = "skipped-wav"
                vad_init_status = "skipped"
            elif total > INGEST_MAX_BYTES:
                ingest_init_status = "skipped-too-large"
                vad_init_status = "skipped"
            elif not self.ingest_enabled:
                ingest_init_status = "disabled"
                vad_init_status = "disabled"
            else:
                ingest_init_status = "pending"
                vad_init_status = "pending" if self.vad_enabled else "disabled"
        try:
            self._bench_finalize(ok, total, ingest_status=ingest_init_status,
                                 vad_status=vad_init_status)
        except Exception as e:
            print(f"  [!] bench finalize failed: {e}")
        self._emit({"type": "file_done", "file_id": f"{file_id:08x}",
                    "crc_ok": ok, "total_bytes": total,
                    "ingest_status": ingest_init_status, "vad_status": vad_init_status})

        # Fire decoupled background upload (does NOT block BLE or ACK).
        if ok and out_path is not None and meta_path is not None and ingest_init_status == "pending":
            try:
                asyncio.create_task(
                    self._ingest_in_background(bytes(data), out_path, meta_path, file_id)
                )
            except Exception as e:
                print(f"  [!] ingest schedule failed: {e}")

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
    raw = sys.argv[1:]
    args = [a for a in raw if not a.startswith("-")]
    flags = [a for a in raw if a.startswith("-")]
    bench_flag = "--bench" in flags or "--csv" in flags
    # Ingestion flags (env defaults, CLI overrides): --ingest / --no-ingest,
    # --ingest-url <url> / --ingest-url=<url>, --user-id <uuid> / --user-id=<uuid>, --keep
    # --no-queue-wait (return on READY), --queue-wait-timeout <s>
    ingest_enabled = INGEST_ENABLED_DEFAULT
    if "--no-ingest" in flags:
        ingest_enabled = False
    if "--ingest" in flags:
        ingest_enabled = True
    ingest_url = INGEST_BASE_URL
    ingest_user = INGEST_USER_ID
    ingest_poll_enabled = INGEST_POLL_ENABLED_DEFAULT and ("--no-queue-wait" not in flags)
    ingest_poll_timeout = INGEST_POLL_TIMEOUT_S
    ingest_poll_interval = INGEST_POLL_INTERVAL_S
    # VAD flags: --vad / --no-vad, --vad-threshold <f>, --min-speech <seconds>
    vad_enabled = VAD_ENABLED_DEFAULT
    if "--no-vad" in flags:
        vad_enabled = False
    if "--vad" in flags:
        vad_enabled = True
    vad_threshold = VAD_THRESHOLD
    vad_min_speech = VAD_MIN_SPEECH_S
    for i, tok in enumerate(raw):
        if tok == "--ingest-url" and i + 1 < len(raw):
            ingest_url = raw[i + 1].rstrip("/")
        elif tok.startswith("--ingest-url="):
            ingest_url = tok.split("=", 1)[1].rstrip("/")
        elif tok == "--user-id" and i + 1 < len(raw):
            ingest_user = raw[i + 1]
        elif tok.startswith("--user-id="):
            ingest_user = tok.split("=", 1)[1]
        elif tok == "--vad-threshold" and i + 1 < len(raw):
            try:
                vad_threshold = float(raw[i + 1])
            except ValueError:
                pass
        elif tok.startswith("--vad-threshold="):
            try:
                vad_threshold = float(tok.split("=", 1)[1])
            except ValueError:
                pass
        elif tok == "--min-speech" and i + 1 < len(raw):
            try:
                vad_min_speech = float(raw[i + 1])
            except ValueError:
                pass
        elif tok.startswith("--min-speech="):
            try:
                vad_min_speech = float(tok.split("=", 1)[1])
            except ValueError:
                pass
        elif tok == "--queue-wait-timeout" and i + 1 < len(raw):
            try:
                ingest_poll_timeout = float(raw[i + 1])
            except ValueError:
                pass
        elif tok.startswith("--queue-wait-timeout="):
            try:
                ingest_poll_timeout = float(tok.split("=", 1)[1])
            except ValueError:
                pass
    ingest_delete = INGEST_DELETE_AFTER_DEFAULT and ("--keep" not in flags)
    auto_rebond = BLE_AUTO_REBOND_DEFAULT and ("--no-rebond" not in flags)
    # also support: python client.py --bench [device]
    arg = args[0] if args else None
    bench_csv = None
    if bench_flag:
        ts = time.strftime("%Y%m%d_%H%M%S")
        bench_csv = BENCH_DIR / f"benchmark_{ts}.csv"
        print(f"Benchmark capture -> {bench_csv}")
    # Load Silero VAD once at startup (fail-open: continue without VAD on error)
    vad_model = None
    if vad_enabled and ingest_enabled:
        print(f"[vad] loading Silero model (threshold={vad_threshold} min={vad_min_speech}s) ...")
        try:
            loop = asyncio.get_event_loop()
            vad_model = await loop.run_in_executor(None, vad_load_model)
            print("[vad] model loaded")
        except Exception as e:
            print(f"[vad] load failed — fail-open, uploads continue without filtering: {e}")
            vad_model = None
    elif vad_enabled and not ingest_enabled:
        print("[vad] ingestion disabled — VAD not loaded")
        vad_enabled = False
    address = await find_device(arg)

    client = CheckpointClient(address, bench_csv=bench_csv,
                              ingest_enabled=ingest_enabled,
                              ingest_base_url=ingest_url,
                              ingest_user_id=ingest_user,
                              ingest_delete_after=ingest_delete,
                              ingest_poll_enabled=ingest_poll_enabled,
                              ingest_poll_timeout=ingest_poll_timeout,
                              ingest_poll_interval=ingest_poll_interval,
                              vad_enabled=vad_enabled,
                              vad_model=vad_model,
                              vad_threshold=vad_threshold,
                              vad_min_speech_s=vad_min_speech,
                              auto_rebond=auto_rebond)
    await client.connect()
    await client.do_handshake()

    print("\nListening for file transfers. Press Ctrl+C to stop.\n")
    try:
        while True:
            await asyncio.sleep(1)
    except KeyboardInterrupt:
        print("\nShutting down...")
    finally:
        # Give decoupled uploads a grace window, then persist final ingest status.
        # Queue-wait polling runs inside upload_async, so extend the grace when enabled.
        try:
            pending = [r for r in client.bench_rows() if r.get("ingest_status") == "pending"]
            if pending and client.ingest_enabled:
                budget = 10.0
                try:
                    if client.uploader.poll_enabled:
                        budget = max(budget, client.uploader.poll_timeout + 10.0)
                except Exception:
                    pass
                print(f"[ingest] waiting up to {budget:.0f}s for {len(pending)} pending upload(s)...")
                for _ in range(int(budget * 10)):
                    await asyncio.sleep(0.1)
                    if not any(r.get("ingest_status") == "pending" for r in client._bench_rows):
                        break
        except Exception:
            pass
        await client.disconnect()
        # flush bench csv (rewrite to include async ingest updates) + json
        if bench_csv:
            try:
                client._bench_rewrite_csv()
                print(f"Benchmark csv saved: {bench_csv} ({len(client.bench_rows())} files)")
                # also dump json summary
                jpath = bench_csv.with_suffix(".json")
                jpath.write_text(json.dumps(client.bench_rows(), indent=2))
                print(f"Benchmark json saved: {jpath}")
            except Exception as e:
                print(f"  [!] bench save failed: {e}")
        elif client._csv_file:
            try:
                client._csv_file.close()
            except Exception:
                pass


if __name__ == "__main__":
    asyncio.run(main())