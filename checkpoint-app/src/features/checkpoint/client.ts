import { ScanMode, type BleManager, type Characteristic, type Device } from 'react-native-ble-plx';

import { base64Decode, base64Encode } from './base64.ts';
import { BenchRecorder } from './bench.ts';
import {
  ACK_TIMEOUT_MS,
  ACK_UUID,
  BLE_FRAG_SIZE,
  COMPLETED_CACHE_SIZE,
  CONNECT_ATTEMPT_LIMIT,
  CRYPTO_TAG_BYTES,
  CTRL_ERR_NOT_READY,
  CTRL_UUID,
  DATA_UUID,
  DEVICE_NAME,
  INGEST_MAX_BYTES,
  MIN_MTU_REQUIRED,
  PKT_ACK,
  PKT_AUTH,
  PKT_CMD,
  PKT_DATA,
  PKT_ERROR,
  PKT_FILE_ANNOUNCE,
  PKT_FILE_DONE,
  PKT_FILE_DONE_ACK,
  PKT_FILE_ANNOUNCE_ACK,
  PKT_HELLO,
  PKT_HELLO_ACK,
  PKT_AUTH_OK,
  PKT_READY_ACK,
  PKT_CMD_RESP,
  PKT_STATUS_RESP,
  PKT_STORAGE_RESP,
  PKT_LIST_RESP,
  PKT_KEEPALIVE,
  PKT_LIST_REQ,
  PKT_READY,
  PKT_STATUS_REQ,
  PKT_STORAGE_REQ,
  READY_RETRIES,
  RECONNECT_DELAY_MS,
  RECONNECT_DELAY_MAX_MS,
  SERVICE_UUID,
} from './config.ts';
import {
  deleteCredential,
  isCredentialPending,
  loadCredential,
  markCredentialActive,
  saveCredential,
  setEnrolledDeviceId,
} from './credentials.ts';
import {
  CLIENT_DOMAIN,
  SERVER_DOMAIN,
  buildTranscript,
  bytesToHex,
  clientProof,
  constantTimeEqual,
  decryptFragment,
  deriveClientKeyV3,
  deriveFileKey,
  deriveSessionKeyV3,
  finishProof,
  newId,
} from './crypto.ts';
import { decodeUtf8, isOggOpus } from './ogg.ts';
import {
  buildFileDeletePayload,
  buildFileFetchPayload,
  buildLedSetPayload,
  buildListReqPayload,
  buildStorageErasePayload,
  buildSyncSetPayload,
  buildTimeSetPayload,
  parseCmdResp,
  parseFileDoneTime,
  parseFileList,
  parseStatus,
  parseStorage,
  type CmdResponse,
} from './parsers.ts';
import { describeScanError } from './permissionPolicy.ts';
import {
  buildAnnounceAckPayload,
  buildFileDoneAckPayload,
  buildFragAckPayload,
  buildPacket,
  crc32,
  packetName,
  parsePacket,
} from './protocol.ts';
import { growBackoffMs } from './retry.ts';
import {
  deletePart,
  deleteSaved,
  openPart,
  readPartBytes,
  readSavedBytes,
  readSidecar,
  saveCompleted,
  writeSidecar,
  type PartWriter,
} from './store.ts';
import {
  IncomingFile,
  contigOf,
  crcHex,
  fileIdHex,
  resumeFrom,
  shouldSendAck,
  validateSidecar,
} from './transfer.ts';
import type { CheckpointEvent, DeviceFileList, DeviceStatus, StorageInfo } from './types.ts';

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function viewOf(data: Uint8Array): DataView {
  return new DataView(data.buffer, data.byteOffset, data.byteLength);
}

export interface CompletedFile {
  fileIdHex: string;
  bytes: Uint8Array;
  totalBytes: number;
  preview?: boolean;
  recordedAt?: number;
}

interface PendingRoundtrip {
  resolve: (value: CmdResponse | DeviceStatus | StorageInfo | DeviceFileList) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

export interface CheckpointCallbacks {
  onEvent: (event: CheckpointEvent) => void;
  log: (line: string) => void;
  onFile?: (file: CompletedFile) => void;
}

interface CompletedEntry {
  crcHex: string;
  size: number;
  uri: string;
}

export class CheckpointClient {
  readonly bench = new BenchRecorder();
  linkState: 'up' | 'down' = 'down';
  linkLost = true;

  private device: Device | null = null;
  private disconnectSubscription: { remove: () => void } | null = null;
  private monitorSubscriptions: { remove: () => void }[] = [];
  private sessionId: number | null = null;
  private sessionKey: Uint8Array | null = null;
  private deviceId: Uint8Array | null = null;
  private deviceNonce: Uint8Array | null = null;
  private clientNonce: Uint8Array | null = null;
  private clientId: Uint8Array = newId();
  private clientKey: Uint8Array | null = null;
  private serverProof: Uint8Array | null = null;
  private authMode = 0;
  private authTranscript: Uint8Array | null = null;
  private mtu: number | null = null;
  private fragSize: number = BLE_FRAG_SIZE;

  private helloDone: boolean | null = null;
  private authDone: boolean | null = null;
  private readyDone: boolean | null = null;
  private lastError: number | null = null;

  private currentFile: IncomingFile | null = null;
  private partWriter: PartWriter | null = null;
  private completed = new Map<string, CompletedEntry>();
  private previewIds = new Set<string>();
  private seqGen = 1;
  private pending = new Map<number, PendingRoundtrip>();
  private stopRequested = false;
  private failedAttempts = 0;
  private reconnectDelayMs = RECONNECT_DELAY_MS;

  constructor(
    private readonly manager: BleManager,
    private readonly address: string,
    private readonly callbacks: CheckpointCallbacks,
  ) {}

  private emit(event: CheckpointEvent): void {
    try {
      this.callbacks.onEvent(event);
    } catch {
      /* listener errors must not break the link */
    }
  }

  private log(line: string): void {
    try {
      this.callbacks.log(line);
    } catch {
      /* ignore */
    }
  }

  reportResult(result: {
    fileId: string;
    uploadId?: string;
    ingestStatus: string;
    ingestError?: string;
    vadStatus: string;
    vadSpeechS?: string;
  }): void {
    this.bench.updateIngest(
      result.fileId,
      result.uploadId ?? '',
      result.ingestStatus,
      result.ingestError ?? '',
      result.vadStatus,
      result.vadSpeechS,
    );
    this.emit({
      type: 'ingest',
      fileId: result.fileId,
      uploadId: result.uploadId ?? '',
      ingestStatus: result.ingestStatus,
      ingestError: result.ingestError ?? '',
      vadStatus: result.vadStatus,
      vadSpeechS: result.vadSpeechS,
    });
  }

  private nextSeq(): number {
    const seq = this.seqGen;
    this.seqGen = (this.seqGen + 1) & 0xffff;
    if (this.seqGen === 0) this.seqGen = 1;
    return seq;
  }

  async scanForDevice(target: string, timeoutMs: number): Promise<Device> {
    const want = target || DEVICE_NAME;
    return new Promise<Device>((resolve, reject) => {
      let settled = false;
      let cancelPoll: ReturnType<typeof setInterval> | null = null;
      const seenIds = new Set<string>();
      let seenCount = 0;
      const cleanup = () => {
        if (cancelPoll !== null) clearInterval(cancelPoll);
        try {
          this.manager.stopDeviceScan().catch(() => {
            /* ignore */
          });
        } catch {
          /* ignore */
        }
      };
      const timer = setTimeout(() => {
        if (settled) return;
        settled = true;
        cleanup();
        reject(new Error(`Device '${want}' not found (saw ${seenCount} BLE device(s))`));
      }, timeoutMs);
      const finish = (action: () => void) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        cleanup();
        action();
      };
      cancelPoll = setInterval(() => {
        if (settled || !this.stopRequested) return;
        settled = true;
        clearTimeout(timer);
        cleanup();
        reject(new Error('Scan cancelled'));
      }, 200);
      try {
        this.manager
          .startDeviceScan(
            [SERVICE_UUID],
            { allowDuplicates: false, scanMode: ScanMode.LowLatency },
            (error, scanned) => {
              if (error) {
                const message = describeScanError(error.errorCode, error.message);
                this.log(`[ble] scan failed: ${message}`);
                finish(() => reject(new Error(message)));
                return;
              }
              if (!scanned) return;
              seenCount += 1;
              const id = scanned.id ?? '';
              const name = scanned.name ?? '';
              const localName = scanned.localName ?? '';
              const named =
                id.toUpperCase() === want.toUpperCase() || name === want || localName === want;
              // Scan is already filtered by SERVICE_UUID, so any hit advertises
              // our service; a missing name (common on some stacks when the
              // scan response is absent) must not veto the match.
              if (named || (name === '' && localName === '')) {
                this.log(`Found: ${name || '?'} [${scanned.id}]`);
                finish(() => resolve(scanned));
              } else if (!seenIds.has(id)) {
                seenIds.add(id);
                this.log(`[ble] ignoring non-target device: ${name || '?'} [${id}]`);
              }
            },
          )
          .catch((error: unknown) => {
            finish(() => {
              reject(error instanceof Error ? error : new Error('Scan failed'));
            });
          });
      } catch (error) {
        finish(() => {
          reject(error instanceof Error ? error : new Error('Scan failed'));
        });
      }
    });
  }

  async connect(device: Device): Promise<void> {
    this.device = await device.connect();
    await this.device.discoverAllServicesAndCharacteristics();
    try {
      await this.device.requestMTU(517);
    } catch {
      /* iOS negotiates MTU itself; HELLO_ACK is authoritative */
    }
    this.linkLost = false;
    this.clearSubscriptions();
    this.disconnectSubscription = this.device.onDisconnected(() => {
      this.handleLinkLost();
    });
    const onNotify = (error: unknown, characteristic: Characteristic | null) => {
      if (error || !characteristic?.value) return;
      try {
        const packet = parsePacket(base64Decode(characteristic.value));
        if (packet) void this.handlePacket(packet);
      } catch {
        /* ignore malformed notify */
      }
    };
    this.monitorSubscriptions.push(
      this.device.monitorCharacteristicForService(SERVICE_UUID, CTRL_UUID, onNotify),
      this.device.monitorCharacteristicForService(SERVICE_UUID, DATA_UUID, onNotify),
    );
    this.log(`Connected to ${device.id}`);
  }

  private clearSubscriptions(): void {
    this.disconnectSubscription?.remove();
    this.disconnectSubscription = null;
    for (const subscription of this.monitorSubscriptions) subscription.remove();
    this.monitorSubscriptions = [];
  }

  async disconnect(): Promise<void> {
    const device = this.device;
    this.device = null;
    this.clearSubscriptions();
    this.linkState = 'down';
    this.closePart();
    if (device) {
      try {
        await device.cancelConnection();
      } catch {
        /* already gone */
      }
    }
    this.emit({ type: 'link', state: 'down' });
  }

  private handleLinkLost(): void {
    this.clearSubscriptions();
    this.device = null;
    this.linkState = 'down';
    this.sessionKey = null;
    this.sessionId = null;
    this.currentFile = null;
    this.previewIds.clear();
    this.closePart();
    for (const [, waiter] of this.pending) {
      clearTimeout(waiter.timer);
      waiter.reject(new Error('BLE disconnected'));
    }
    this.pending.clear();
    this.linkLost = true;
    this.emit({ type: 'link', state: 'down' });
    this.log('[ble] link disappeared');
  }

  private async writeCtrl(
    type: number,
    seq: number,
    payload: Uint8Array = new Uint8Array(0),
  ): Promise<void> {
    if (!this.device) throw new Error('Not connected');
    await this.device.writeCharacteristicWithResponseForService(
      SERVICE_UUID,
      CTRL_UUID,
      base64Encode(buildPacket(type, seq, payload)),
    );
  }

  private async writeAck(type: number, seq: number, payload: Uint8Array): Promise<boolean> {
    if (!this.device) return false;
    const encoded = base64Encode(buildPacket(type, seq, payload));
    try {
      await this.device.writeCharacteristicWithoutResponseForService(
        SERVICE_UUID,
        ACK_UUID,
        encoded,
      );
      return true;
    } catch {
      try {
        await this.device.writeCharacteristicWithResponseForService(
          SERVICE_UUID,
          ACK_UUID,
          encoded,
        );
        return true;
      } catch {
        return false;
      }
    }
  }

  private async ctrlRoundtrip(
    type: number,
    payload: Uint8Array,
    timeoutMs = 5000,
  ): Promise<CmdResponse | DeviceStatus | StorageInfo | DeviceFileList> {
    const seq = this.nextSeq();
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(seq);
        reject(new Error('Command timed out'));
      }, timeoutMs);
      this.pending.set(seq, { resolve, reject, timer });
      this.writeCtrl(type, seq, payload).catch((error: unknown) => {
        clearTimeout(timer);
        this.pending.delete(seq);
        reject(error instanceof Error ? error : new Error('Write failed'));
      });
    });
  }

  private completePending(
    seq: number,
    value: CmdResponse | DeviceStatus | StorageInfo | DeviceFileList,
  ): void {
    const waiter = this.pending.get(seq);
    if (!waiter) return;
    clearTimeout(waiter.timer);
    this.pending.delete(seq);
    waiter.resolve(value);
  }

  private async waitForFlag(
    get: () => boolean | null,
    timeoutMs: number,
    timeoutMessage: string,
  ): Promise<boolean> {
    const start = Date.now();
    for (;;) {
      const value = get();
      if (value !== null) return value;
      if (Date.now() - start > timeoutMs) throw new Error(timeoutMessage);
      await sleep(50);
    }
  }

  private lastErrorCode(): number | null {
    return this.lastError;
  }

  async doHandshake(claimKey: Uint8Array | null): Promise<void> {
    const enroll = claimKey !== null;
    const helloSeq = this.nextSeq();
    this.helloDone = null;
    this.lastError = null;
    await this.writeCtrl(PKT_HELLO, helloSeq, new Uint8Array([enroll ? 1 : 0]));
    const helloOk = await this.waitForFlag(() => this.helloDone, ACK_TIMEOUT_MS, 'HELLO timed out');
    if (!helloOk) {
      const code = this.lastErrorCode();
      if (code === 0x02) {
        throw new Error('HELLO rejected: version mismatch (error 0x02)');
      }
      if (code === 0x01) {
        throw new Error(
          'HELLO rejected: link not encrypted (error 0x01) — pair/bond first, then retry (enroll mode only)',
        );
      }
      throw new Error(
        code !== null
          ? `HELLO rejected with error 0x${code.toString(16).padStart(2, '0')}`
          : 'HELLO rejected',
      );
    }
    if (enroll && this.authMode !== 1) {
      throw new Error(
        'Enrollment window is not active. Hold the Checkpoint button for 5 seconds and retry.',
      );
    }
    if (!this.deviceId || this.sessionId === null || !this.deviceNonce) {
      throw new Error('AUTH without HELLO_ACK challenge');
    }
    this.clientNonce = newId();
    let authKey: Uint8Array;
    if (enroll && claimKey) {
      this.authMode = 1;
      authKey = deriveClientKeyV3(
        claimKey,
        this.deviceNonce,
        this.clientNonce,
        this.deviceId,
        this.clientId,
      );
    } else {
      const found = await loadCredential(this.deviceId);
      if (!found) {
        throw new Error(
          'Unknown Checkpoint — no stored credential. Hold the button 5s and enroll with the claim key.',
        );
      }
      this.clientId = found.clientId;
      this.clientKey = found.clientKey;
      this.authMode = 0;
      authKey = found.clientKey;
    }
    const transcript = buildTranscript(
      this.deviceId,
      this.clientId,
      this.sessionId,
      this.deviceNonce,
      this.clientNonce,
      this.authMode,
    );
    const proof = clientProof(authKey, transcript);
    const expectSession = deriveSessionKeyV3(
      authKey,
      this.deviceNonce,
      this.clientNonce,
      this.deviceId,
      this.clientId,
      this.sessionId,
    );
    this.authTranscript = transcript;
    this.authDone = null;
    this.lastError = null;
    const authPayload = new Uint8Array(
      this.clientId.length + this.clientNonce.length + proof.length,
    );
    authPayload.set(this.clientId, 0);
    authPayload.set(this.clientNonce, this.clientId.length);
    authPayload.set(proof, this.clientId.length + this.clientNonce.length);
    await this.writeCtrl(PKT_AUTH, this.nextSeq(), authPayload);
    const authOk = await this.waitForFlag(() => this.authDone, ACK_TIMEOUT_MS, 'AUTH timed out');
    if (!authOk) {
      const code = this.lastErrorCode();
      throw new Error(
        code !== null
          ? `AUTH rejected with error 0x${code.toString(16).padStart(2, '0')}`
          : 'AUTH rejected',
      );
    }
    const expectServer = finishProof(expectSession, SERVER_DOMAIN, transcript);
    if (!this.serverProof || !constantTimeEqual(expectServer, this.serverProof)) {
      throw new Error('Server proof mismatch — possible MITM');
    }
    this.sessionKey = expectSession;
    if (enroll) {
      await saveCredential(this.deviceId, this.clientId, authKey, true);
    }
    const wasPending = enroll ? true : await isCredentialPending(this.deviceId);
    for (let attempt = 1; attempt <= READY_RETRIES; attempt++) {
      this.readyDone = null;
      this.lastError = null;
      await this.sendReady();
      try {
        const readyOk = await this.waitForFlag(
          () => this.readyDone,
          ACK_TIMEOUT_MS,
          'READY timed out',
        );
        if (readyOk) break;
        if (attempt === READY_RETRIES) {
          if (enroll && this.deviceId) await deleteCredential(this.deviceId);
          const code = this.lastErrorCode();
          throw new Error(
            code !== null
              ? `READY rejected with error 0x${code.toString(16).padStart(2, '0')}`
              : 'READY rejected',
          );
        }
      } catch (error) {
        if (attempt === READY_RETRIES) throw error;
        this.log(`  READY_ACK lost (attempt ${attempt}/${READY_RETRIES}) — retrying ...`);
      }
    }
    if (this.deviceId && wasPending) await markCredentialActive(this.deviceId);
    if (this.deviceId) await setEnrolledDeviceId(bytesToHex(this.deviceId));
    this.clientKey = enroll ? authKey : this.clientKey;
    this.authTranscript = null;
    this.linkState = 'up';
    this.emit({ type: 'link', state: 'up' });
    this.log(
      `Handshake complete. session_id=${this.sessionId}, mtu=${this.mtu}, frag_size=${this.fragSize}`,
    );
  }

  private async sendReady(): Promise<void> {
    if (!this.sessionKey || !this.authTranscript || this.sessionId === null) {
      throw new Error('READY without session key');
    }
    const finish = finishProof(this.sessionKey, CLIENT_DOMAIN, this.authTranscript);
    const payload = new Uint8Array(4 + finish.length);
    new DataView(payload.buffer).setUint32(0, this.sessionId >>> 0, true);
    payload.set(finish, 4);
    await this.writeCtrl(PKT_READY, this.nextSeq(), payload);
  }

  private parseHelloAck(payload: Uint8Array): boolean {
    if (payload.length !== 44) return false;
    const view = viewOf(payload);
    if (view.getUint8(0) !== 3) return false;
    const mtu = view.getUint16(5, true);
    if (mtu < MIN_MTU_REQUIRED) return false;
    this.sessionId = view.getUint32(1, true);
    this.mtu = mtu;
    this.deviceId = payload.slice(11, 27);
    this.deviceNonce = payload.slice(27, 43);
    this.authMode = payload[43]!;
    this.fragSize = BLE_FRAG_SIZE;
    this.sessionKey = null;
    return true;
  }

  private async handlePacket(packet: {
    type: number;
    seq: number;
    payload: Uint8Array;
  }): Promise<void> {
    const name = packetName(packet.type);
    if (packet.type === PKT_HELLO_ACK) {
      this.helloDone = this.parseHelloAck(packet.payload);
      return;
    }
    if (packet.type === PKT_AUTH_OK) {
      if (packet.payload.length !== 32) {
        this.authDone = false;
      } else {
        this.serverProof = packet.payload.slice(0, 32);
        this.authDone = true;
      }
      return;
    }
    if (packet.type === PKT_READY_ACK) {
      if (packet.payload.length !== 4) {
        this.readyDone = false;
        return;
      }
      const sid = viewOf(packet.payload).getUint32(0, true);
      this.readyDone = sid === this.sessionId;
      return;
    }
    if (packet.type === PKT_ERROR) {
      this.lastError = packet.payload.length > 0 ? packet.payload[0]! : null;
      this.helloDone ??= false;
      this.authDone ??= false;
      this.readyDone ??= false;
      this.log(`  [!] Device ERROR: ${name} payload=${bytesToHex(packet.payload)}`);
      return;
    }
    if (packet.type === PKT_FILE_ANNOUNCE) {
      await this.handleAnnounce(packet.seq, packet.payload);
      return;
    }
    if (packet.type === PKT_DATA) {
      await this.handleData(packet.seq, packet.payload);
      return;
    }
    if (packet.type === PKT_FILE_DONE) {
      await this.handleFileDone(packet.seq, packet.payload);
      return;
    }
    if (packet.type === PKT_KEEPALIVE) return;
    if (packet.type === PKT_CMD_RESP) {
      const payload = packet.payload;
      if (payload.length < 2) return;
      this.completePending(packet.seq, parseCmdResp(payload[0]!, payload));
      return;
    }
    if (packet.type === PKT_STATUS_RESP) {
      const info = parseStatus(packet.payload);
      if (!info) return;
      this.completePending(packet.seq, info);
      this.emit({ type: 'rec_status', ...info });
      return;
    }
    if (packet.type === PKT_STORAGE_RESP) {
      const info = parseStorage(packet.payload);
      if (!info) return;
      this.completePending(packet.seq, info);
      this.emit({ type: 'storage', ...info });
      return;
    }
    if (packet.type === PKT_LIST_RESP) {
      const info = parseFileList(packet.payload);
      if (!info) return;
      this.completePending(packet.seq, info);
      this.emit({ type: 'file_list', ...info });
    }
  }

  private closePart(): void {
    try {
      this.partWriter?.close();
    } catch {
      /* ignore */
    }
    this.partWriter = null;
  }

  private async handleAnnounce(pktSeq: number, payload: Uint8Array): Promise<void> {
    if (payload.length < 21) return;
    const view = viewOf(payload);
    const total = view.getUint32(1, true);
    const totalFrags = view.getUint16(5, true);
    const fileCrc = view.getUint32(7, true);
    const deviceStartSeq = view.getUint16(11, true);
    const fileId = view.getBigUint64(13, true);
    const idHex = fileIdHex(fileId);
    // Preview fetches append flags + path; normal announces stay 21 bytes.
    const preview = payload.length >= 22 && (payload[21]! & 0x01) === 1;
    const previewPath = preview && payload.length > 22 ? decodeUtf8(payload.slice(22)) : undefined;
    this.log(
      `  FILE_ANNOUNCE: total=${total}B frags=${totalFrags} crc=${crcHex(fileCrc)} file_id=${idHex}${preview ? ` preview=${previewPath ?? '?'}` : ''}`,
    );
    if (preview) this.previewIds.add(idHex);
    else this.previewIds.delete(idHex);
    let key: Uint8Array | null = null;
    if (this.sessionKey && this.sessionId !== null) {
      key = deriveFileKey(this.sessionKey, this.sessionId, fileId);
    }
    const incoming = new IncomingFile(
      fileId,
      total,
      totalFrags,
      fileCrc,
      key,
      this.sessionId ?? 0,
      this.fragSize,
    );
    this.currentFile = incoming;
    let resume = 0;
    if (
      !preview &&
      totalFrags > 0 &&
      deviceStartSeq === totalFrags &&
      (await this.completedOk(fileId, fileCrc, total))
    ) {
      resume = totalFrags;
      for (let i = 0; i < totalFrags; i++) incoming.received.add(i);
      incoming.contigSeq = totalFrags - 1;
      this.log(`  [resume] ${idHex} already completed — confirming`);
    } else {
      const partBytes = preview ? null : await readPartBytes(idHex);
      const sidecar = preview ? null : await readSidecar(idHex);
      const valid = validateSidecar(
        sidecar,
        crcHex(fileCrc),
        total,
        totalFrags,
        this.fragSize,
        partBytes?.length ?? 0,
      );
      if (valid && partBytes) {
        incoming.buffer.set(partBytes.slice(0, total), 0);
        for (const seq of valid) incoming.received.add(seq);
        incoming.contigSeq = contigOf(incoming.received);
        resume = resumeFrom(valid, totalFrags);
        if (resume > 0) {
          this.log(`  [resume] ${idHex} continuing at ${resume}/${totalFrags}`);
        }
      }
      if (resume === 0) deletePart(idHex);
      this.closePart();
      this.partWriter = openPart(idHex, total);
    }
    if (!preview) this.bench.resetFile(fileId, total, totalFrags, resume);
    const sent = await this.writeAck(
      PKT_FILE_ANNOUNCE_ACK,
      this.nextSeq(),
      buildAnnounceAckPayload(pktSeq, resume),
    );
    if (!sent) {
      this.log('  [!] ANNOUNCE_ACK write failed — keeping state for firmware retry');
      return;
    }
    this.emit({
      type: 'announce',
      fileId: idHex,
      totalBytes: total,
      totalFrags,
      preview,
      path: previewPath,
    });
  }

  private async handleData(seq: number, raw: Uint8Array): Promise<void> {
    const file = this.currentFile;
    if (!file?.key) return;
    if (raw.length < CRYPTO_TAG_BYTES) return;
    const plain = decryptFragment(file.key, file.sessionId, file.fileId, seq, raw);
    let ackSeq: number | null = null;
    if (plain === null) {
      this.bench.decryptFail += 1;
      if (file.contigSeq >= 0 && file.received.size % 8 === 0) {
        ackSeq = file.contigSeq;
      }
    } else {
      if (file.received.has(seq)) this.bench.duplicates += 1;
      if (!file.addFragment(seq, plain)) return;
      try {
        this.partWriter?.write(seq, plain, file.fragSize);
      } catch {
        this.closePart();
      }
      const count = file.received.size;
      if (count % 20 === 0 || count === file.totalFrags) {
        const progressId = fileIdHex(file.fileId);
        this.emit({
          type: 'progress',
          fileId: progressId,
          received: count,
          totalFrags: file.totalFrags,
          preview: this.previewIds.has(progressId),
        });
      }
      if (shouldSendAck(count, file.contigSeq, file.totalFrags)) {
        ackSeq = file.contigSeq;
        writeSidecar(fileIdHex(file.fileId), {
          crc: crcHex(file.expectedCrc),
          total: file.totalBytes,
          totalFrags: file.totalFrags,
          fragSize: file.fragSize,
          received: [...file.received].sort((a, b) => a - b),
        });
      }
    }
    if (ackSeq !== null) {
      await this.writeAck(PKT_ACK, this.nextSeq(), buildFragAckPayload(ackSeq));
    }
    this.bench.noteDataArrival();
  }

  private async completedOk(fileId: bigint, fileCrc: number, total: number): Promise<boolean> {
    const entry = this.completed.get(fileIdHex(fileId));
    if (!entry) return false;
    if (entry.crcHex !== crcHex(fileCrc) || entry.size !== total) return false;
    const saved = await readSavedBytes(entry.uri);
    return saved !== null && crc32(saved) === fileCrc >>> 0;
  }

  private rememberCompleted(fileId: bigint, fileCrc: number, total: number, uri: string): void {
    this.completed.set(fileIdHex(fileId), {
      crcHex: crcHex(fileCrc),
      size: total,
      uri,
    });
    while (this.completed.size > COMPLETED_CACHE_SIZE) {
      const oldest = this.completed.keys().next();
      if (oldest.done) break;
      this.completed.delete(oldest.value);
    }
  }

  private async handleFileDone(pktSeq: number, payload: Uint8Array): Promise<void> {
    if (payload.length < 16) return;
    const view = viewOf(payload);
    const fileId = view.getBigUint64(0, true);
    const fileCrc = view.getUint32(8, true);
    const total = view.getUint32(12, true);
    const idHex = fileIdHex(fileId);
    const preview = this.previewIds.has(idHex);
    const recordedAt = parseFileDoneTime(payload) ?? undefined;
    const file = this.currentFile;
    let ok = false;
    let fromCache = false;
    let savedNow = false;
    let data: Uint8Array | null = null;
    if (file?.fileId === fileId) {
      data = file.data();
      const actual = crc32(data);
      ok = actual === fileCrc >>> 0 && file.received.size === file.totalFrags;
      if (!ok && (await this.completedOk(fileId, fileCrc, total))) {
        ok = true;
        fromCache = true;
        this.log(`  duplicate FILE_DONE for completed ${idHex} — re-ACKing`);
      }
      if (ok && !fromCache && data) {
        const ext = isOggOpus(data) ? '.ogg' : '.wav';
        const uri = saveCompleted(idHex, ext, data, {
          file_id: idHex,
          total,
          total_frags: file.totalFrags,
          expected_crc: crcHex(fileCrc),
          actual_crc: crcHex(actual),
          mtu: this.mtu ?? 0,
          frag_size: file.fragSize,
          duplicates: this.bench.duplicates,
          decrypt_fail: this.bench.decryptFail,
          ...(recordedAt ? { recorded_at: new Date(recordedAt).toISOString() } : {}),
        });
        if (uri) {
          this.rememberCompleted(fileId, fileCrc, total, uri);
          savedNow = true;
        } else {
          this.log('  [!] saving completed file failed');
        }
      }
    }
    const sent = await this.writeAck(
      PKT_FILE_DONE_ACK,
      this.nextSeq(),
      buildFileDoneAckPayload(pktSeq, ok),
    );
    if (!sent) {
      this.log('  [!] FILE_DONE_ACK write failed — keeping state for firmware retry');
      return;
    }
    if (ok) {
      deletePart(idHex);
      this.closePart();
    }
    const isOgg = data !== null && isOggOpus(data);
    let ingestStatus = '';
    let vadStatus = '';
    if (ok && savedNow && data) {
      if (!isOgg) {
        ingestStatus = 'skipped-wav';
        vadStatus = 'skipped';
      } else if (total > INGEST_MAX_BYTES) {
        ingestStatus = 'skipped-too-large';
        vadStatus = 'skipped';
      } else {
        ingestStatus = 'pending';
        vadStatus = 'pending';
      }
    }
    if (!preview) {
      this.bench.finalize(ok, total, this.mtu ?? 0, this.fragSize, ingestStatus, vadStatus);
    }
    this.emit({
      type: 'file_done',
      fileId: idHex,
      crcOk: ok,
      totalBytes: total,
      ingestStatus,
      vadStatus,
      preview,
      recordedAt,
    });
    if (ok && !fromCache && data && (ingestStatus === 'pending' || preview)) {
      try {
        this.callbacks.onFile?.({
          fileIdHex: idHex,
          bytes: data,
          totalBytes: total,
          preview,
          recordedAt,
        });
      } catch {
        /* listener errors must not break the link */
      }
    }
    if (preview) this.previewIds.delete(idHex);
    if (this.currentFile?.fileId === fileId) this.currentFile = null;
  }

  async cmdRecStart(): Promise<number> {
    const res = (await this.ctrlRoundtrip(PKT_CMD, new Uint8Array([0x01]))) as CmdResponse;
    return res.status ?? CTRL_ERR_NOT_READY;
  }

  async cmdRecStop(): Promise<number> {
    const res = (await this.ctrlRoundtrip(PKT_CMD, new Uint8Array([0x02]))) as CmdResponse;
    return res.status ?? CTRL_ERR_NOT_READY;
  }

  async cmdLedSet(muted: boolean, brightness: number): Promise<number> {
    const res = (await this.ctrlRoundtrip(
      PKT_CMD,
      buildLedSetPayload(muted, brightness),
    )) as CmdResponse;
    return res.status ?? CTRL_ERR_NOT_READY;
  }

  async cmdLedGet(): Promise<CmdResponse> {
    return (await this.ctrlRoundtrip(PKT_CMD, new Uint8Array([0x11]))) as CmdResponse;
  }

  async cmdSyncSet(enabled: boolean): Promise<number> {
    const res = (await this.ctrlRoundtrip(PKT_CMD, buildSyncSetPayload(enabled))) as CmdResponse;
    return res.status ?? CTRL_ERR_NOT_READY;
  }

  async cmdSyncGet(): Promise<CmdResponse> {
    return (await this.ctrlRoundtrip(PKT_CMD, new Uint8Array([0x13]))) as CmdResponse;
  }

  async cmdTimeSet(unixSeconds: number): Promise<number> {
    const res = (await this.ctrlRoundtrip(
      PKT_CMD,
      buildTimeSetPayload(unixSeconds),
    )) as CmdResponse;
    return res.status ?? CTRL_ERR_NOT_READY;
  }

  async reqStatus(): Promise<DeviceStatus> {
    const res = await this.ctrlRoundtrip(PKT_STATUS_REQ, new Uint8Array(0));
    if (!('recording' in res)) throw new Error('Bad STATUS_RESP');
    return res as DeviceStatus;
  }

  async reqStorage(): Promise<StorageInfo> {
    const res = await this.ctrlRoundtrip(PKT_STORAGE_REQ, new Uint8Array(0));
    if (!('total' in res)) throw new Error('Bad STORAGE_RESP');
    return res as StorageInfo;
  }

  async reqList(start = 0): Promise<DeviceFileList> {
    const res = await this.ctrlRoundtrip(PKT_LIST_REQ, buildListReqPayload(start));
    if (!('entries' in res)) throw new Error('Bad LIST_RESP');
    return res as DeviceFileList;
  }

  async cmdFileDelete(path: string): Promise<number> {
    const res = (await this.ctrlRoundtrip(PKT_CMD, buildFileDeletePayload(path))) as CmdResponse;
    return res.status ?? CTRL_ERR_NOT_READY;
  }

  async cmdFileFetch(path: string): Promise<number> {
    const res = (await this.ctrlRoundtrip(PKT_CMD, buildFileFetchPayload(path))) as CmdResponse;
    return res.status ?? CTRL_ERR_NOT_READY;
  }

  async cmdStorageErase(step: number): Promise<CmdResponse> {
    return (await this.ctrlRoundtrip(
      PKT_CMD,
      buildStorageErasePayload(step),
      10000,
    )) as CmdResponse;
  }

  async supervise(
    target: string,
    claimKey: Uint8Array | null,
    hooks: {
      stopped: () => boolean;
      onReady?: () => Promise<void>;
      onAlive?: () => void | Promise<void>;
      onTick?: () => Promise<void>;
    },
  ): Promise<void> {
    this.stopRequested = false;
    this.failedAttempts = 0;
    this.reconnectDelayMs = RECONNECT_DELAY_MS;
    let readyOnce = false;
    const retryOrGiveUp = async (message: string): Promise<boolean> => {
      this.log(message);
      if (!readyOnce) {
        this.failedAttempts += 1;
        if (this.failedAttempts >= CONNECT_ATTEMPT_LIMIT) {
          this.log(
            `[ble] giving up after ${CONNECT_ATTEMPT_LIMIT} attempts — tap Connect to retry`,
          );
          return true;
        }
      }
      const delay = this.reconnectDelayMs;
      this.reconnectDelayMs = growBackoffMs(this.reconnectDelayMs, RECONNECT_DELAY_MAX_MS);
      return this.sleepOrStopped(delay, hooks.stopped);
    };
    while (!hooks.stopped()) {
      let device: Device | null = null;
      try {
        device = await this.scanForDevice(target, 8000);
      } catch (error) {
        if (hooks.stopped() || this.stopRequested) return;
        const reason = error instanceof Error ? error.message : 'unknown';
        if (await retryOrGiveUp(`[ble] scan error: ${reason}`)) return;
        continue;
      }
      if (!device) {
        if (await retryOrGiveUp('[ble] Checkpoint offline — waiting for power/advertising...'))
          return;
        continue;
      }
      try {
        await this.connect(device);
        await this.doHandshake(claimKey);
        if (!this.device || !(await this.device.isConnected())) {
          throw new Error('BLE disappeared during handshake');
        }
        this.reconnectDelayMs = RECONNECT_DELAY_MS;
      } catch (error) {
        this.linkState = 'down';
        try {
          await this.disconnect();
        } catch {
          /* ignore */
        }
        if (hooks.stopped() || this.stopRequested) return;
        const reason = error instanceof Error ? error.message : 'unknown';
        if (await retryOrGiveUp(`[ble] setup failed: ${reason}`)) return;
        continue;
      }
      if (!readyOnce) {
        readyOnce = true;
        await hooks.onReady?.();
      }
      await hooks.onAlive?.();
      for (;;) {
        if (hooks.stopped()) return;
        if (this.linkLost) break;
        if (!this.device || !(await this.device.isConnected().catch(() => false))) {
          break;
        }
        try {
          await hooks.onTick?.();
        } catch {
          if (!this.device || !(await this.device.isConnected().catch(() => false))) {
            break;
          }
        }
        await sleep(500);
      }
      if (hooks.stopped()) return;
      this.log('[ble] link disappeared — rediscovering...');
      try {
        await this.disconnect();
      } catch {
        /* ignore */
      }
    }
  }

  private async sleepOrStopped(ms: number, stopped: () => boolean): Promise<boolean> {
    let waited = 0;
    while (waited < ms) {
      if (stopped() || this.stopRequested) return true;
      await sleep(500);
      waited += 500;
    }
    return stopped() || this.stopRequested;
  }

  requestStop(): void {
    this.stopRequested = true;
  }

  static deleteLocalCopy(fileIdHexValue: string): void {
    deleteSaved(fileIdHexValue);
  }
}
