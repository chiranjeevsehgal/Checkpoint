"""Shared constants. Must match firmware config.h / protocol.h / control.h."""

import os
from pathlib import Path

DEVICE_NAME = "Checkpoint"

SERVICE_UUID = "9a8b0001-4a2b-4e3c-8f1a-5b2c9d0e1f2a"
CTRL_UUID = "9a8b0002-4a2b-4e3c-8f1a-5b2c9d0e1f2a"
DATA_UUID = "9a8b0003-4a2b-4e3c-8f1a-5b2c9d0e1f2a"
ACK_UUID = "9a8b0004-4a2b-4e3c-8f1a-5b2c9d0e1f2a"

PROTO_VER = 1
PROTO_HEADER = 6
PROTO_CRC = 4

CRYPTO_KEY_BYTES = 16
CRYPTO_NONCE_BYTES = 12
CRYPTO_TAG_BYTES = 8

MTU_OVERHEAD = PROTO_HEADER + CRYPTO_TAG_BYTES + PROTO_CRC + 3 + 4
BLE_FRAG_SIZE_GUESS = 220

APP_DIR = Path(__file__).resolve().parent
OUTPUT_DIR = APP_DIR.parent / "received"
BENCH_DIR = OUTPUT_DIR
ACK_TIMEOUT_S = 5.0

INGEST_BASE_URL = os.getenv("INGEST_BASE_URL", "http://localhost:8080").rstrip("/")
INGEST_USER_ID = os.getenv("INGEST_USER_ID", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
INGEST_ENABLED_DEFAULT = os.getenv("INGEST_ENABLED", "1") == "1"
INGEST_DELETE_AFTER_DEFAULT = os.getenv("INGEST_DELETE_AFTER", "1") == "1"
INGEST_TIMEOUT_S = float(os.getenv("INGEST_TIMEOUT_S", "15"))
INGEST_MAX_BYTES = 10 * 1024 * 1024
INGEST_POLL_ENABLED_DEFAULT = os.getenv("INGEST_POLL_ENABLED", "1") == "1"
INGEST_POLL_TIMEOUT_S = float(os.getenv("INGEST_POLL_TIMEOUT_S", "30"))
INGEST_POLL_INTERVAL_S = float(os.getenv("INGEST_POLL_INTERVAL_S", "1.0"))
KAFKA_TOPIC_HINT = os.getenv("KAFKA_TOPIC_TRANSCRIPTION", "transcription.jobs.v1")

VAD_ENABLED_DEFAULT = os.getenv("VAD_ENABLED", "1") == "1"
VAD_THRESHOLD = float(os.getenv("VAD_THRESHOLD", "0.85"))
VAD_MIN_SPEECH_S = float(os.getenv("VAD_MIN_SPEECH_S", "1.5"))
VAD_MIN_SPEECH_MS = int(os.getenv("VAD_MIN_SPEECH_MS", "800"))
VAD_MIN_SILENCE_MS = int(os.getenv("VAD_MIN_SILENCE_MS", "500"))
VAD_PAD_MS = int(os.getenv("VAD_PAD_MS", "200"))

BLE_AUTO_REBOND_DEFAULT = os.getenv("BLE_AUTO_REBOND", "1") == "1"

STATUS_POLL_INTERVAL_S = float(os.getenv("STATUS_POLL_INTERVAL_S", "5"))
STATUS_PUSH_SEQ = 0

BENCH_FIELDNAMES = [
    "ts", "file_id", "total_bytes", "total_frags", "mtu", "frag_size",
    "goodput_kBps", "median_rtt_ms", "p95_rtt_ms", "duplicates", "retries",
    "decrypt_fail", "crc_ok", "resume_from", "elapsed_s",
    "ingest_upload_id", "ingest_status", "ingest_error",
    "vad_status", "vad_speech_s",
]

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

CTRL_CMD_REC_START = 0x01
CTRL_CMD_REC_STOP = 0x02
CTRL_CMD_LED_SET = 0x10
CTRL_CMD_LED_GET = 0x11
CTRL_CMD_SYNC_SET = 0x12
CTRL_CMD_SYNC_GET = 0x13
CTRL_CMD_FILE_DELETE = 0x20
CTRL_CMD_STORAGE_ERASE = 0x21

CTRL_OK = 0x00
CTRL_ERR_NOT_READY = 0x01
CTRL_ERR_NO_SD = 0x02
CTRL_ERR_BAD_ARG = 0x03
CTRL_ERR_DENIED = 0x04
CTRL_ERR_BUSY = 0x05
CTRL_ERR_NOT_FOUND = 0x06

CTRL_STATUS_LEN = 17
CTRL_STORAGE_LEN = 20
CTRL_BRIGHT_MIN = 5

CTRL_ERASE_ARM = 0x01
CTRL_ERASE_CONFIRM = 0x02
CTRL_LIST_FLAG_PENDING = 0x01
CTRL_LIST_FLAG_CRC = 0x02
CTRL_LIST_FLAG_ACTIVE = 0x04
