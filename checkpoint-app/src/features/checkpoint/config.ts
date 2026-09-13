export const DEVICE_NAME = 'Checkpoint';

/** Developer surfaces exist only in dev builds and stay off if the user disabled them. */
export function resolveDeveloperMode(stored: string | null, devBuild: boolean): boolean {
  return devBuild && stored !== '0';
}

export interface AutoConnectGate {
  autoSyncEnabled: boolean;
  suppressed: boolean;
  connected: boolean;
  busy: boolean;
  autoConnecting: boolean;
  lastAttemptAt: number;
  now: number;
}

/** Auto-connect runs when enabled, not suppressed, idle, and past the cooldown. */
export function shouldAttemptAutoConnect(gate: AutoConnectGate): boolean {
  if (!gate.autoSyncEnabled || gate.suppressed) return false;
  if (gate.connected || gate.busy || gate.autoConnecting) return false;
  return gate.now - gate.lastAttemptAt >= AUTO_CONNECT_COOLDOWN_MS;
}

export const SERVICE_UUID = '9a8b0001-4a2b-4e3c-8f1a-5b2c9d0e1f2a';
export const CTRL_UUID = '9a8b0002-4a2b-4e3c-8f1a-5b2c9d0e1f2a';
export const DATA_UUID = '9a8b0003-4a2b-4e3c-8f1a-5b2c9d0e1f2a';
export const ACK_UUID = '9a8b0004-4a2b-4e3c-8f1a-5b2c9d0e1f2a';

export const PROTO_VER = 3;
export const PROTO_HEADER = 6;
export const PROTO_CRC = 4;

export const CRYPTO_KEY_BYTES = 16;
export const CRYPTO_NONCE_BYTES = 12;
export const CRYPTO_TAG_BYTES = 8;

export const BLE_FRAG_SIZE = 220;
export const MIN_MTU_REQUIRED = 241;
export const BLE_WINDOW = 8;

export const ACK_TIMEOUT_MS = 5000;
export const READY_RETRIES = 3;
export const CONNECT_ATTEMPT_LIMIT = 3;
export const RECONNECT_DELAY_MS = 2000;
export const AUTO_CONNECT_COOLDOWN_MS = 60_000;
export const COMPLETED_CACHE_SIZE = 16;

export const INGEST_USER_ID_DEFAULT = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa';
export const INGEST_TIMEOUT_S = 15;
export const INGEST_MAX_BYTES = 10 * 1024 * 1024;
export const INGEST_POLL_TIMEOUT_S = 30;
export const INGEST_POLL_INTERVAL_S = 1.0;

export const VAD_THRESHOLD_DEFAULT = 0.85;
export const VAD_MIN_SPEECH_S_DEFAULT = 1.5;
export const VAD_MIN_SPEECH_MS = 800;
export const VAD_MIN_SILENCE_MS = 500;
export const VAD_PAD_MS = 200;
export const VAD_SAMPLE_RATE = 16000;
export const VAD_WINDOW_SAMPLES = 512;
// Silero v5 feeds each window with the trailing (window / 8) samples of the
// previous window prepended; the ONNX graph expects window + context samples.
export const VAD_CONTEXT_SAMPLES = VAD_WINDOW_SAMPLES / 8;

export const STATUS_POLL_INTERVAL_S = 5;
export const STATUS_PUSH_SEQ = 0;
export const KAFKA_TOPIC_HINT = 'transcription.jobs.v1';

export const MAX_LOG_LINES = 500;

export const TRANSFER_RETENTION_HOURS = 24;
export const TRANSFER_TICK_MS = 60000;
export const TRANSFER_CLEANUP_INTERVAL_MS = 60 * 60 * 1000;
export const UPLOAD_RETRY_DELAYS_MS = [5_000, 30_000, 120_000, 600_000, 1_800_000] as const;

export const HEALTH_PATH = '/health/ready';
export const HEALTH_TIMEOUT_MS = 4000;
export const HEALTH_POLL_OK_MS = 15000;
export const HEALTH_POLL_DOWN_MS = 60000;
export const HEALTH_UNSTABLE_FAILS = 2;
export const HEALTH_UNSTABLE_WINDOW_MS = 60000;
export const HEALTH_SLOW_MS = 3000;

export const BENCH_FIELDNAMES = [
  'ts',
  'file_id',
  'total_bytes',
  'total_frags',
  'mtu',
  'frag_size',
  'goodput_kBps',
  'median_rtt_ms',
  'p95_rtt_ms',
  'duplicates',
  'retries',
  'decrypt_fail',
  'crc_ok',
  'resume_from',
  'elapsed_s',
  'ingest_upload_id',
  'ingest_status',
  'ingest_error',
  'vad_status',
  'vad_speech_s',
] as const;

export const PKT_HELLO = 0x01;
export const PKT_HELLO_ACK = 0x02;
export const PKT_AUTH = 0x03;
export const PKT_AUTH_OK = 0x04;
export const PKT_READY_ACK = 0x05;
export const PKT_FILE_ANNOUNCE = 0x10;
export const PKT_FILE_ANNOUNCE_ACK = 0x11;
export const PKT_DATA = 0x12;
export const PKT_ACK = 0x13;
export const PKT_FILE_DONE = 0x14;
export const PKT_FILE_DONE_ACK = 0x15;
export const PKT_ERROR = 0x16;
export const PKT_RESUME_REQ = 0x17;
export const PKT_RESUME_RESP = 0x18;
export const PKT_KEEPALIVE = 0x19;
export const PKT_CMD = 0x20;
export const PKT_CMD_RESP = 0x21;
export const PKT_STATUS_REQ = 0x22;
export const PKT_STATUS_RESP = 0x23;
export const PKT_STORAGE_REQ = 0x24;
export const PKT_STORAGE_RESP = 0x25;
export const PKT_LIST_REQ = 0x26;
export const PKT_LIST_RESP = 0x27;
export const PKT_READY = 0x28;

export const CTRL_CMD_REC_START = 0x01;
export const CTRL_CMD_REC_STOP = 0x02;
export const CTRL_CMD_LED_SET = 0x10;
export const CTRL_CMD_LED_GET = 0x11;
export const CTRL_CMD_SYNC_SET = 0x12;
export const CTRL_CMD_SYNC_GET = 0x13;
export const CTRL_CMD_FILE_DELETE = 0x20;
export const CTRL_CMD_STORAGE_ERASE = 0x21;
export const CTRL_CMD_FILE_FETCH = 0x22;

export const CTRL_OK = 0x00;
export const CTRL_ERR_NOT_READY = 0x01;
export const CTRL_ERR_NO_SD = 0x02;
export const CTRL_ERR_BAD_ARG = 0x03;
export const CTRL_ERR_DENIED = 0x04;
export const CTRL_ERR_BUSY = 0x05;
export const CTRL_ERR_NOT_FOUND = 0x06;

export const CTRL_STATUS_LEN = 17;
export const CTRL_STORAGE_LEN = 20;
export const CTRL_BRIGHT_MIN = 5;

export const CTRL_ERASE_ARM = 0x01;
export const CTRL_ERASE_CONFIRM = 0x02;
export const CTRL_LIST_FLAG_PENDING = 0x01;
export const CTRL_LIST_FLAG_CRC = 0x02;
export const CTRL_LIST_FLAG_ACTIVE = 0x04;
