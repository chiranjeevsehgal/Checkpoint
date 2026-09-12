"""BLE sync client. Behavior-identical move of `client - ingestion.py` core."""

import asyncio
import json
import os
import struct
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path

from bleak import BleakClient, BleakScanner

from . import config as cfg
from .audio_vad import vad_has_speech, vad_load_model
from .bench import BenchRecorder
from . import credentials as creds
from .crypto import (
    CLIENT_DOM,
    SERVER_DOM,
    build_transcript,
    client_proof,
    decrypt_fragment,
    derive_client_key_v3,
    derive_file_key,
    derive_session_key_v3,
    finish_proof,
)
from .ingestion import IngestionUploader
from .protocol import PKT_NAMES, Packet, crc32, proto_build, proto_parse


@dataclass
class IncomingFile:
    file_id: int
    total_bytes: int
    total_frags: int
    expected_crc: int
    key: bytes
    session_id: int
    frag_size: int = cfg.BLE_FRAG_SIZE_GUESS
    buffer: bytearray = field(default_factory=bytearray)
    received_frags: set = field(default_factory=set)
    contig_seq: int = -1

    def add_fragment(self, seq: int, data: bytes):
        offset = seq * self.frag_size
        if offset + len(data) > len(self.buffer):
            self.buffer.extend(b"\x00" * (offset + len(data) - len(self.buffer)))
        self.buffer[offset:offset + len(data)] = data
        self.received_frags.add(seq)
        while (self.contig_seq + 1) in self.received_frags:
            self.contig_seq += 1

    def is_complete(self) -> bool:
        if not self.received_frags:
            return False
        if len(self.received_frags) < self.total_frags:
            return False
        return len(bytes(self.buffer[:self.total_bytes])) == self.total_bytes and \
            all(i in self.received_frags for i in range(self.total_frags))


class CheckpointClient:
    def __init__(self, address: str, bench_csv: Path | None = None,
                 ingest_enabled: bool = cfg.INGEST_ENABLED_DEFAULT,
                 ingest_base_url: str = cfg.INGEST_BASE_URL,
                 ingest_user_id: str = cfg.INGEST_USER_ID,
                 ingest_delete_after: bool = cfg.INGEST_DELETE_AFTER_DEFAULT,
                 ingest_poll_enabled: bool = cfg.INGEST_POLL_ENABLED_DEFAULT,
                 ingest_poll_timeout: float = cfg.INGEST_POLL_TIMEOUT_S,
                 ingest_poll_interval: float = cfg.INGEST_POLL_INTERVAL_S,
                 vad_enabled: bool = cfg.VAD_ENABLED_DEFAULT,
                 vad_model=None,
                 vad_threshold: float = cfg.VAD_THRESHOLD,
                 vad_min_speech_s: float = cfg.VAD_MIN_SPEECH_S,
                 auto_rebond: bool = cfg.BLE_AUTO_REBOND_DEFAULT,
                 client_id: bytes | None = None,
                 claim_key: bytes | None = None,
                 enroll: bool = False,
                 on_event=None):
        self.address = address
        self.client: BleakClient | None = None
        self.session_id: int | None = None
        self.session_key: bytes | None = None
        self.master_key: bytes | None = None
        self.device_id: bytes | None = None
        self.device_nonce: bytes | None = None
        self.client_nonce: bytes | None = None
        self.server_proof: bytes | None = None
        self.auth_mode: int = 0
        self.client_id: bytes = bytes(client_id) if client_id else __import__("secrets").token_bytes(16)
        self.client_key: bytes | None = None
        self.claim_key: bytes | None = bytes(claim_key) if claim_key else None
        self.enroll: bool = bool(enroll or (claim_key is not None))
        self._was_pending: bool = False
        self.mtu: int | None = None
        self.chunk_sec: int | None = None
        self.frag_size: int = cfg.BLE_FRAG_SIZE_GUESS
        self.hello_acked = asyncio.Event()
        self.auth_ok_event = asyncio.Event()
        self.ready_ack_event = asyncio.Event()
        self.error_event = asyncio.Event()
        self.last_error: int | None = None

        self.current_file: IncomingFile | None = None
        self.file_done_event = asyncio.Event()
        self.announce_event = asyncio.Event()
        self.link_lost = asyncio.Event()
        self.link_lost.set()  # no link yet; supervisor clears on each cycle
        self.link_state = "down"
        self._loop: asyncio.AbstractEventLoop | None = None
        self.completed: dict[int, dict] = {}
        self._part_fh = None
        self._part_id: int | None = None
        self._seq_gen = 1
        self._ctrl_pending: dict[int, asyncio.Future] = {}
        self._data_lock = asyncio.Lock()
        self._ack_lock = asyncio.Lock()
        self._ctrl_tx_lock = asyncio.Lock()
        self._file_state_lock = asyncio.Lock()

        self.ingest_enabled = ingest_enabled
        self.ingest_delete_after = ingest_delete_after
        self.uploader = IngestionUploader(
            base_url=ingest_base_url, user_id=ingest_user_id,
            poll_enabled=ingest_poll_enabled,
            poll_timeout=ingest_poll_timeout,
            poll_interval=ingest_poll_interval)
        if self.ingest_enabled:
            qw = f"queue-wait={self.uploader.poll_enabled} timeout={self.uploader.poll_timeout}s"
            print(f"[ingest] enabled -> {self.uploader.base_url} "
                  f"user={self.uploader.user_id[:8]}... "
                  f"delete_after={self.ingest_delete_after} {qw} topic={cfg.KAFKA_TOPIC_HINT}")
        else:
            print("[ingest] disabled — files stay in ./received/")

        self.vad_enabled = vad_enabled
        self.vad_model = vad_model
        self.vad_threshold = vad_threshold
        self.vad_min_speech_s = vad_min_speech_s
        if self.vad_enabled:
            state = "loaded" if self.vad_model is not None else "MISSING-fail-open"
            print(f"[vad] enabled ({state}) threshold={self.vad_threshold} "
                  f"min_speech={self.vad_min_speech_s}s")
        else:
            print("[vad] disabled — all OGG go straight to ingestion")

        self.bench_csv = bench_csv
        self.on_event = on_event
        self.bench = BenchRecorder(bench_csv, on_event=self._emit)
        self.auto_rebond = auto_rebond

    def next_seq(self) -> int:
        seq = self._seq_gen
        self._seq_gen = (self._seq_gen + 1) & 0xFFFF
        if self._seq_gen == 0:
            self._seq_gen = 1  # 0 is reserved for unsolicited STATUS pushes
        return seq

    def _emit(self, evt: dict):
        cb = getattr(self, "on_event", None)
        if cb is None:
            return
        try:
            cb(evt)
        except Exception:
            pass

    def bench_rows(self) -> list[dict]:
        return self.bench.rows()

    async def _ingest_in_background(self, data: bytes, out_path: Path,
                                    meta_path: Path, file_id: int):
        file_hex = f"{file_id:016x}"
        filename = out_path.name
        # Stable identity is the 64-bit file UID: session ids change per
        # reconnect, so the backend can deduplicate re-uploads.
        idem_key = file_hex
        vad_status = "disabled"
        vad_speech = 0.0
        if self.vad_enabled:
            if self.vad_model is None:
                vad_status = "model-missing-fail-open"
                print(f"  [vad] {filename}: model unavailable — fail-open to upload")
            else:
                print(f"  [vad] checking {filename} ({len(data)}B) "
                      f"threshold={self.vad_threshold} min={self.vad_min_speech_s}s ...")
                try:
                    loop = asyncio.get_event_loop()
                    vad_status, vad_speech, segments = await loop.run_in_executor(
                        None, vad_has_speech, data, self.vad_model,
                        self.vad_threshold, self.vad_min_speech_s)
                except Exception as e:
                    vad_status, vad_speech, segments = "error", 0.0, [{"error": str(e)[:200]}]
                print(f"VAD,{file_hex},{vad_status},{vad_speech:.2f}")
                self._emit({"type": "vad", "file_id": file_hex, "vad_status": vad_status,
                            "vad_speech_s": round(vad_speech, 2)})
                if isinstance(segments, list) and segments \
                        and isinstance(segments[0], dict) and "error" in segments[0]:
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
                    self.bench.update_ingest(file_hex, "", "skipped-no-speech", msg,
                                             vad_status, f"{vad_speech:.2f}")
                    try:
                        meta = {}
                        if meta_path.exists():
                            meta = json.loads(meta_path.read_text())
                        meta.update({"vad_status": vad_status,
                                     "vad_speech_s": round(vad_speech, 2),
                                     "ingest_status": "skipped-no-speech"})
                        meta_path.write_text(json.dumps(meta, indent=2))
                    except Exception:
                        pass
                    if self.ingest_delete_after:
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
                data, filename, "audio/ogg", idem_key)
            if status == "SUBMITTED":
                print(f"  [ingest] OK {filename} -> upload_id={upload_id} "
                      f"status=SUBMITTED (queued to Kafka {cfg.KAFKA_TOPIC_HINT})")
                print(f"INGEST,{file_hex},{upload_id},SUBMITTED,,")
                self.bench.update_ingest(file_hex, upload_id, status, "",
                                         vad_status, f"{vad_speech:.2f}")
            else:
                msg = (f"queued-timeout after {self.uploader.poll_timeout:.0f}s, "
                       "Kafka publish pending (upload durable, check outbox)")
                print(f"  [ingest] OK {filename} -> upload_id={upload_id} status=READY ({msg})")
                print(f"INGEST,{file_hex},{upload_id},READY,{msg},")
                self.bench.update_ingest(file_hex, upload_id, "READY", msg,
                                         vad_status, f"{vad_speech:.2f}")
            try:
                meta = {}
                if meta_path.exists():
                    meta = json.loads(meta_path.read_text())
                meta.update({"ingest_upload_id": upload_id, "ingest_status": status,
                             "kafka_topic": cfg.KAFKA_TOPIC_HINT,
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
            self.bench.update_ingest(file_hex, "", "failed", err,
                                     vad_status, f"{vad_speech:.2f}")

    async def rediscover(self, timeout: float = 3.0):
        """Rescan for Checkpoint. Returns a fresh BLEDevice, or None if offline.

        A hard power-cycle leaves no graceful disconnect and the cached
        address may be stale, so every reconnect rediscovers first.
        """
        print(f"[ble] scanning for {self.address} / {cfg.DEVICE_NAME} ...")
        try:
            found = await BleakScanner.discover(timeout=timeout, return_adv=True)
            devices = [item[0] for item in found.values()]
        except TypeError:
            devices = await BleakScanner.discover(timeout=timeout)
        want = (self.address or "").upper()
        named = None
        for device in devices:
            addr = (device.address or "").upper()
            if addr and addr == want:
                print(f"[ble] rediscovered {device.name} [{device.address}]")
                return device
            if (device.name or "") == cfg.DEVICE_NAME and named is None:
                named = device
        if named is not None:
            print(f"[ble] rediscovered {cfg.DEVICE_NAME} [{named.address}]")
            self.address = named.address
            return named
        return None

    async def connect(self, device=None):
        print(f"Connecting to {self.address} ...")
        try:
            self._loop = asyncio.get_running_loop()
        except RuntimeError:
            self._loop = None
        new_client = BleakClient(device if device is not None else self.address,
                                 disconnected_callback=self._on_link_lost)
        # Install the new generation BEFORE clearing the old event: late
        # callbacks from the previous client then fail the identity check.
        self.client = new_client
        self.link_state = "down"
        self.link_lost.clear()
        try:
            await new_client.connect(timeout=10.0)
        except Exception:
            if self.client is new_client:
                self.client = None
            self.link_state = "down"
            raise
        print(f"Connected. is_connected={self.client.is_connected} address={self.address}")
        # v3: normal mode never pairs automatically. Pairing happens only
        # inside enroll() with physical button + claim key.
        bonded = await self._windows_bond_present()
        print(f"  Windows bond present: {bonded}")
        if bonded is False and not self.enroll:
            try:
                await self.client.disconnect()
            except Exception:
                pass
            raise RuntimeError(
                "No BLE bond in normal mode — refusing to auto-pair. "
                "Hold the Checkpoint button 5s, then run enroll with the claim key.")
        if bonded is False and self.enroll:
            print("No Windows bond — pairing for enrollment ...")
            try:
                result = await self.client.pair()
                print(f"  pair result={result}")
            except Exception as e:
                print(f"  enroll pair failed: {e}")
                try:
                    await self.client.disconnect()
                except Exception:
                    pass
                raise
        await self.client.start_notify(cfg.CTRL_UUID, self._on_ctrl_indicate)
        await self.client.start_notify(cfg.DATA_UUID, self._on_data_notify)
        print("Subscribed to ctrl + data characteristics.")
        try:
            mtu = getattr(self.client, "mtu_size", None)
            if mtu:
                print(f"  Bleak MTU hint: {mtu}")
        except Exception:
            pass

    async def _windows_bond_present(self) -> bool | None:
        """True/False whether Windows holds a bond record for this peer.

        None when unknown (non-Windows or probe error) — caller then uses
        the on-demand path. Uses the WinRT API directly from our MAC address
        (Bleak's internals move between versions, so they are not touched).
        Best-effort — never raises.
        """
        try:
            from winrt.windows.devices.bluetooth import BluetoothLEDevice
            addr = int(self.address.replace(":", ""), 16)
            dev = await BluetoothLEDevice.from_bluetooth_address_async(addr)
            if dev is None:
                return False
            return bool(dev.device_information.pairing.is_paired)
        except Exception:
            return None

    async def disconnect(self):
        if self.client and self.client.is_connected:
            await self.client.disconnect()

    def _on_link_lost(self, client=None):
        print(f"[ble] disconnect callback callback_client={id(client)} "
              f"active_client={id(self.client)} stale={client is not self.client}")
        # Ignore delayed callbacks from a superseded BleakClient generation.
        if client is not None and client is not self.client:
            print("[ble] ignoring stale disconnect callback")
            return
        # Bleak may call this off the event loop (WinRT thread) — hop to it.
        if self._loop is not None:
            try:
                self._loop.call_soon_threadsafe(self._handle_link_lost, client)
                return
            except RuntimeError:
                pass
        self._handle_link_lost(client)

    def _handle_link_lost(self, client=None):
        # Recheck: self.client may have changed while marshalling to the loop.
        if client is not None and client is not self.client:
            return
        self.link_state = "down"
        self._teardown_session()
        self.link_lost.set()
        self._emit({"type": "link", "state": "down"})

    def _teardown_session(self):
        """Fresh-restart teardown for a dead link.

        Fails pending commands, drops in-memory transfer/handshake state so
        the next cycle rescans and rebuilds everything from scratch. Disk
        state (.part files, completed cache, saved outputs) survives for
        resume and duplicate-DONE re-ACK after reconnect.
        """
        for fut in list(self._ctrl_pending.values()):
            if not fut.done():
                try:
                    fut.set_exception(ConnectionError("BLE disconnected"))
                except Exception:
                    pass
                try:
                    # The waiter may never await (link dropped mid-write),
                    # so retrieve here or asyncio logs "never retrieved".
                    fut.exception()
                except Exception:
                    pass
        self._ctrl_pending.clear()
        self.hello_acked.clear()
        self.auth_ok_event.clear()
        self.ready_ack_event.clear()
        self.error_event.clear()
        self.last_error = None
        self.session_id = None
        self.session_key = None
        self.master_key = None
        self.device_id = None
        self.device_nonce = None
        self.client_nonce = None
        self.server_proof = None
        self.auth_mode = 0
        self._pending_enroll_key = None
        self._expect_session = None
        self._auth_transcript = None
        self._was_pending = False
        self.mtu = None
        self.chunk_sec = None
        self.current_file = None
        self._close_part()

    async def disconnect_graceful(self, wait_pending_s: float = 10.0):
        try:
            if self.client and self.client.is_connected:
                for uuid in (cfg.CTRL_UUID, cfg.DATA_UUID):
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
                pending = [r for r in self.bench.rows() if r.get("ingest_status") == "pending"]
                if pending:
                    import asyncio as _aio
                    for _ in range(int(budget * 10)):
                        await _aio.sleep(0.1)
                        if not any(r.get("ingest_status") == "pending"
                                   for r in self.bench.rows()):
                            break
        except Exception:
            pass
        await self.disconnect()
        try:
            self.bench.rewrite_csv()
        except Exception:
            pass

    async def write_ctrl(self, ptype: int, seq: int, payload: bytes = b""):
        async with self._ctrl_tx_lock:
            await self.client.write_gatt_char(cfg.CTRL_UUID, proto_build(ptype, seq, payload),
                                              response=True)

    async def write_ack(self, ptype: int, seq: int, payload: bytes = b"",
                        required: bool = False) -> bool:
        pkt = proto_build(ptype, seq, payload)
        async with self._ack_lock:
            try:
                await self.client.write_gatt_char(cfg.ACK_UUID, pkt, response=False)
                return True
            except Exception as e:
                try:
                    await self.client.write_gatt_char(cfg.ACK_UUID, pkt, response=True)
                    return True
                except Exception as e2:
                    print(f"  [!] ACK write failed seq={seq}: {e} / {e2}")
                    if required:
                        print("  [!] critical ACK lost (announce/done) — state kept")
                    return False

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
        res = await self._ctrl_roundtrip(cfg.PKT_CMD, bytes([cfg.CTRL_CMD_REC_START]), timeout)
        return int(res.get("status", cfg.CTRL_ERR_NOT_READY))

    async def cmd_rec_stop(self, timeout: float = 5.0) -> int:
        res = await self._ctrl_roundtrip(cfg.PKT_CMD, bytes([cfg.CTRL_CMD_REC_STOP]), timeout)
        return int(res.get("status", cfg.CTRL_ERR_NOT_READY))

    async def cmd_led_set(self, muted: bool, brightness: int, timeout: float = 5.0) -> int:
        bright = max(0, min(255, int(brightness)))
        if not muted and bright < cfg.CTRL_BRIGHT_MIN:
            bright = cfg.CTRL_BRIGHT_MIN
        res = await self._ctrl_roundtrip(
            cfg.PKT_CMD, bytes([cfg.CTRL_CMD_LED_SET, 0x01 if muted else 0x00, bright]), timeout)
        return int(res.get("status", cfg.CTRL_ERR_NOT_READY))

    async def cmd_led_get(self, timeout: float = 5.0) -> dict:
        return await self._ctrl_roundtrip(cfg.PKT_CMD, bytes([cfg.CTRL_CMD_LED_GET]), timeout)

    async def cmd_sync_set(self, enabled: bool, timeout: float = 5.0) -> int:
        res = await self._ctrl_roundtrip(
            cfg.PKT_CMD, bytes([cfg.CTRL_CMD_SYNC_SET, 0x01 if enabled else 0x00]), timeout)
        return int(res.get("status", cfg.CTRL_ERR_NOT_READY))

    async def cmd_sync_get(self, timeout: float = 5.0) -> dict:
        return await self._ctrl_roundtrip(cfg.PKT_CMD, bytes([cfg.CTRL_CMD_SYNC_GET]), timeout)

    async def req_status(self, timeout: float = 5.0) -> dict:
        return await self._ctrl_roundtrip(cfg.PKT_STATUS_REQ, b"", timeout)

    async def req_storage(self, timeout: float = 5.0) -> dict:
        return await self._ctrl_roundtrip(cfg.PKT_STORAGE_REQ, b"", timeout)

    async def req_list(self, start: int = 0, timeout: float = 5.0) -> dict:
        return await self._ctrl_roundtrip(
            cfg.PKT_LIST_REQ, struct.pack("<H", max(0, start) & 0xFFFF), timeout)

    async def cmd_file_delete(self, path: str, timeout: float = 5.0) -> int:
        res = await self._ctrl_roundtrip(
            cfg.PKT_CMD, bytes([cfg.CTRL_CMD_FILE_DELETE]) + path.encode("utf-8"), timeout)
        return int(res.get("status", cfg.CTRL_ERR_NOT_READY))

    async def cmd_storage_erase(self, step: int, timeout: float = 10.0) -> dict:
        return await self._ctrl_roundtrip(
            cfg.PKT_CMD, bytes([cfg.CTRL_CMD_STORAGE_ERASE, step & 0xFF]), timeout)

    async def cmd_file_fetch(self, path: str, timeout: float = 5.0) -> int:
        res = await self._ctrl_roundtrip(
            cfg.PKT_CMD, bytes([cfg.CTRL_CMD_FILE_FETCH]) + path.encode("utf-8"), timeout)
        return int(res.get("status", cfg.CTRL_ERR_NOT_READY))

    @staticmethod
    def parse_status(payload: bytes) -> dict:
        if len(payload) < 16:
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
            "sync": bool(payload[16]) if len(payload) >= 17 else True,
        }

    @staticmethod
    def parse_storage(payload: bytes) -> dict:
        if len(payload) < cfg.CTRL_STORAGE_LEN:
            return {}
        total, used = struct.unpack("<QQ", payload[0:16])
        files, pending = struct.unpack("<HH", payload[16:20])
        return {"total": total, "used": used, "files": files, "pending": pending}

    @staticmethod
    def parse_file_list(payload: bytes) -> dict:
        if len(payload) < 5:
            return {}
        start, total = struct.unpack("<HH", payload[0:4])
        count = payload[4]
        entries = []
        off = 5
        for _ in range(count):
            if off + 1 > len(payload):
                break
            namelen = payload[off]
            off += 1
            if off + namelen + 4 + 1 > len(payload):
                break
            try:
                name = payload[off:off + namelen].decode("utf-8")
            except UnicodeDecodeError:
                break
            off += namelen
            size = struct.unpack("<I", payload[off:off + 4])[0]
            flags = payload[off + 4]
            off += 5
            entries.append({"name": name, "size": size, "flags": flags})
        return {"start": start, "total": total, "entries": entries}

    async def _rebond(self) -> bool:
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
            await self._connect_with_retry()
        except Exception as e:
            print(f"  [bond] reconnect after unpair failed: {e}")
            return False
        print("  Waiting 2s for encryption to settle before HELLO retry...")
        await asyncio.sleep(2.0)
        return True

    async def clear_stale_bond(self, log=print):
        """Drop the Windows bond so the next cycle pairs fresh.

        Last resort for deterministic setup failure: BLE connects but the
        handshake never completes, which means host and device disagree
        about bonding state. The next supervisor cycle then takes the
        new-peer path (pair before HELLO) automatically.
        """
        log("[ble] clearing possibly-stale bond ...")
        current = self.client
        self.client = None
        if current is not None:
            if hasattr(current, "unpair"):
                try:
                    await current.unpair()
                    log("[ble] bond cleared")
                except Exception as e:
                    log(f"[ble] unpair failed (continuing): {e}")
            try:
                await current.disconnect()
            except Exception:
                pass
        self.link_state = "down"

    async def _connect_with_retry(self, tries: int = 3, wait_s: float = 2.0) -> None:
        """Reconnect, tolerating the device mid-reboot or mid-advertise flap."""
        last: Exception | None = None
        for n in range(1, tries + 1):
            try:
                await self.connect()
                return
            except Exception as e:
                last = e
                print(f"  connect try {n}/{tries} failed ({e}) — retrying in {wait_s:.0f}s ...")
                await asyncio.sleep(wait_s)
        raise RuntimeError(f"Reconnect failed: {last}")

    async def _hello_exchange(self, label: str) -> str:
        """Send one v3 HELLO and wait for the result.

        Returns 'acked' or 'error'. Never reconnects: supervise_link() is the
        only reconnect owner.
        """
        self.hello_acked.clear()
        self.error_event.clear()
        self.last_error = None
        seq = self.next_seq()
        print(f"Sending HELLO (seq={seq}) {label} ...")
        payload = bytes([0x01 if self.enroll else 0x00])
        try:
            await self.write_ctrl(cfg.PKT_HELLO, seq, payload)
        except Exception as e:
            raise ConnectionError(f"HELLO write failed: {e}") from e
        try:
            await asyncio.wait_for(self._wait_for_handshake_result(),
                                   timeout=cfg.ACK_TIMEOUT_S)
        except asyncio.TimeoutError:
            raise ConnectionError("HELLO timed out") from None
        return "acked" if self.hello_acked.is_set() else "error"

    def _print_handshake_complete(self):
        self.link_state = "up"
        self._emit({"type": "link", "state": "up"})
        print(f"Handshake complete. session_id={self.session_id:#010x}, "
              f"mtu={self.mtu} chunk_sec={self.chunk_sec} frag_size={self.frag_size}, "
              f"key={'present' if self.session_key else 'ABSENT (unencrypted transfer!)'}")

    async def _auth_exchange(self) -> None:
        import secrets
        if self.device_id is None or self.session_id is None:
            raise RuntimeError("AUTH without HELLO_ACK challenge")
        if self.enroll:
            if self.claim_key is None:
                raise RuntimeError("Enrollment requires the claim key from USB export")
            self.auth_mode = 1
            self.client_nonce = secrets.token_bytes(16)
            auth_key = derive_client_key_v3(
                self.claim_key, self.device_nonce, self.client_nonce,
                self.device_id, self.client_id)
            transcript = build_transcript(
                self.device_id, self.client_id, self.session_id,
                self.device_nonce, self.client_nonce, self.auth_mode)
            proof = client_proof(auth_key, transcript)
            expect_session = derive_session_key_v3(
                auth_key, self.device_nonce, self.client_nonce,
                self.device_id, self.client_id, self.session_id)
            self._pending_enroll_key = bytes(auth_key)
        else:
            found = creds.load_client_credential(self.device_id)
            if found is None:
                raise RuntimeError(
                    "Unknown Checkpoint — no stored credential. "
                    "Hold the button 5s and enroll with the claim key.")
            stored_id, stored_key = found
            self.client_id = bytes(stored_id)
            self.client_key = bytes(stored_key)
            self.auth_mode = 0
            self.client_nonce = secrets.token_bytes(16)
            transcript = build_transcript(
                self.device_id, self.client_id, self.session_id,
                self.device_nonce, self.client_nonce, self.auth_mode)
            proof = client_proof(self.client_key, transcript)
            expect_session = derive_session_key_v3(
                self.client_key, self.device_nonce, self.client_nonce,
                self.device_id, self.client_id, self.session_id)
        self._expect_session = bytes(expect_session)
        self._auth_transcript = bytes(transcript)
        self.auth_ok_event.clear()
        self.error_event.clear()
        self.last_error = None
        await self.write_ctrl(cfg.PKT_AUTH, self.next_seq(),
                              bytes(self.client_id) + bytes(self.client_nonce) + bytes(proof))
        try:
            await asyncio.wait_for(self._wait_for_auth_result(), timeout=cfg.ACK_TIMEOUT_S)
        except asyncio.TimeoutError:
            raise ConnectionError("AUTH timed out") from None
        if not self.auth_ok_event.is_set():
            raise RuntimeError(f"AUTH rejected with error 0x{self.last_error:02x}"
                               if self.last_error is not None else "AUTH rejected")
        import hmac as _hmac
        expect_server = finish_proof(self._expect_session, SERVER_DOM, self._auth_transcript)
        if not _hmac.compare_digest(expect_server, bytes(self.server_proof or b"")):
            raise RuntimeError("Server proof mismatch — possible MITM")
        self.session_key = bytes(self._expect_session)
        self.master_key = bytes(self.session_key)
        if self.enroll:
            creds.save_client_credential(
                self.device_id, self.client_id, self._pending_enroll_key, pending=True)
        else:
            self._was_pending = creds.is_pending(self.device_id)

    async def do_handshake(self):
        outcome = await self._hello_exchange("attempt 1/1")
        if outcome != "acked":
            if self.last_error == 0x02:
                raise RuntimeError("HELLO rejected: version mismatch (error 0x02) — "
                                   f"check PROTO_VER={cfg.PROTO_VER}")
            if self.last_error == 0x01:
                raise RuntimeError("HELLO rejected: link not encrypted (error 0x01) — "
                                   "pair/bond first, then retry (enroll mode only)")
            raise RuntimeError(f"HELLO rejected with error 0x{self.last_error:02x}"
                               if self.last_error is not None else "HELLO rejected")
        if self.enroll and self.auth_mode != 1:
            raise RuntimeError(
                "Enrollment window is not active. "
                "Hold the Checkpoint button for 5 seconds and retry.")
        await self._auth_exchange()
        for attempt in range(1, 4):
            self.ready_ack_event.clear()
            self.error_event.clear()
            self.last_error = None
            await self._send_ready()
            try:
                await asyncio.wait_for(self._wait_for_ready_ack(), timeout=cfg.ACK_TIMEOUT_S)
            except asyncio.TimeoutError:
                if attempt == 3:
                    raise ConnectionError("READY timed out") from None
                print(f"  READY_ACK lost (attempt {attempt}/3) — retrying ...")
                continue
            if self.ready_ack_event.is_set():
                break
            if self.enroll:
                creds.delete_credential(self.device_id)
            if self.last_error is not None:
                raise RuntimeError(f"READY rejected with error 0x{self.last_error:02x}")
            raise RuntimeError("READY rejected")
        if self.enroll:
            pending = getattr(self, "_pending_enroll_key", None)
            if pending is None:
                raise RuntimeError("Enrollment completed without pending key")
            creds.mark_active(self.device_id)
            self.client_key = bytes(pending)
            self.enroll = False
            self.claim_key = None
        elif getattr(self, "_was_pending", False):
            creds.mark_active(self.device_id)
            self._was_pending = False
        self._pending_enroll_key = None
        self._expect_session = None
        self._auth_transcript = None
        self._print_handshake_complete()

    async def enroll(self, claim_key: bytes) -> None:
        """Explicit enrollment: pairs (bond) then runs the v3 claim handshake."""
        self.claim_key = bytes(claim_key)
        self.enroll = True
        if self.client is not None and hasattr(self.client, "pair"):
            try:
                await self.client.pair()
            except Exception as e:
                print(f"  enroll pair note: {e}")
        await self.do_handshake()
        self.enroll = False

    async def _wait_for_handshake_result(self):
        while not self.hello_acked.is_set() and not self.error_event.is_set():
            await asyncio.sleep(0.05)

    async def _wait_for_auth_result(self):
        while not self.auth_ok_event.is_set() and not self.error_event.is_set():
            await asyncio.sleep(0.05)

    async def _wait_for_ready_ack(self):
        while not self.ready_ack_event.is_set() and not self.error_event.is_set():
            await asyncio.sleep(0.05)

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

        if pkt.type == cfg.PKT_HELLO_ACK:
            if self._parse_hello_ack(pkt.payload):
                self.hello_acked.set()
            else:
                self.error_event.set()

        elif pkt.type == cfg.PKT_FILE_ANNOUNCE:
            await self._handle_announce(pkt)

        elif pkt.type == cfg.PKT_FILE_DONE:
            await self._handle_file_done(pkt)

        elif pkt.type == cfg.PKT_RESUME_RESP:
            print(f"  RESUME_RESP len={len(pkt.payload)}")

        elif pkt.type == cfg.PKT_KEEPALIVE:
            pass

        elif pkt.type == cfg.PKT_CMD_RESP:
            p = pkt.payload
            if len(p) >= 2:
                cmd, status = p[0], p[1]
                result: dict = {"cmd": cmd, "status": status}
                if cmd == cfg.CTRL_CMD_LED_GET and len(p) >= 4:
                    result.update({"muted": bool(p[2]), "brightness": p[3]})
                if cmd == cfg.CTRL_CMD_SYNC_GET and len(p) >= 3:
                    result.update({"sync": bool(p[2])})
                if cmd == cfg.CTRL_CMD_STORAGE_ERASE and len(p) >= 4:
                    result.update({"removed": struct.unpack("<H", p[2:4])[0]})
                print(f"  CMD_RESP cmd=0x{cmd:02x} status={status}")
                self._ctrl_complete(pkt.seq, result)
                self._emit({"type": "cmd_resp", "cmd": cmd, "status": status,
                            **({} if "muted" not in result else
                               {"muted": result["muted"], "brightness": result["brightness"]}),
                            **({} if "sync" not in result else {"sync": result["sync"]}),
                            **({} if "removed" not in result else {"removed": result["removed"]})})

        elif pkt.type == cfg.PKT_STATUS_RESP:
            info = self.parse_status(pkt.payload)
            if info:
                kind = "push" if pkt.seq == cfg.STATUS_PUSH_SEQ else "resp"
                print(f"  STATUS_{kind} rec={info['recording']} "
                      f"vad={info['vad_active']}/{info['vad_speech']} "
                      f"muted={info['muted']} bright={info['brightness']} "
                      f"pend={info['pending']} sync={info['sync']}")
                self._ctrl_complete(pkt.seq, info)
                self._emit({"type": "rec_status", **info})

        elif pkt.type == cfg.PKT_STORAGE_RESP:
            info = self.parse_storage(pkt.payload)
            if info:
                print(f"  STORAGE_RESP total={info['total']} used={info['used']} "
                      f"files={info['files']} pend={info['pending']}")
                self._ctrl_complete(pkt.seq, info)
                self._emit({"type": "storage", **info})

        elif pkt.type == cfg.PKT_LIST_RESP:
            info = self.parse_file_list(pkt.payload)
            if info:
                print(f"  LIST_RESP start={info['start']} total={info['total']} "
                      f"entries={len(info['entries'])}")
                self._ctrl_complete(pkt.seq, info)
                self._emit({"type": "file_list", **info})

        elif pkt.type == cfg.PKT_ERROR:
            code = pkt.payload[0] if pkt.payload else None
            self.last_error = code
            self.error_event.set()
            detail = "pairing required (0x01)" if code == 0x01 else \
                "version mismatch (0x02)" if code == 0x02 else \
                "auth required (0x03)" if code == 0x03 else \
                f"0x{code:02x}" if code is not None else "empty"
            print(f"  [!] Device PKT_ERROR: {detail} payload={pkt.payload!r}")

        elif pkt.type == cfg.PKT_AUTH_OK:
            if len(pkt.payload) != 32:
                print("  [!] AUTH_OK wrong length — rejecting")
                self.error_event.set()
            else:
                self.server_proof = bytes(pkt.payload[:32])
                self.auth_ok_event.set()

        elif pkt.type == cfg.PKT_READY_ACK:
            if len(pkt.payload) != 4:
                print("  [!] READY_ACK wrong length — rejecting")
                self.error_event.set()
                return
            sid = struct.unpack("<I", pkt.payload)[0]
            if sid != self.session_id:
                print("  [!] READY_ACK session mismatch — rejecting")
                self.error_event.set()
                return
            self.ready_ack_event.set()

    def _parse_hello_ack(self, payload: bytes) -> bool:
        if len(payload) < 44:
            print("  [!] HELLO_ACK payload too short (want 44)")
            return False
        proto_ver = payload[0]
        if proto_ver != cfg.PROTO_VER:
            print(f"  [!] HELLO_ACK proto_ver mismatch device={proto_ver} client={cfg.PROTO_VER}")
            return False
        self.session_id = struct.unpack("<I", payload[1:5])[0]
        self.mtu = struct.unpack("<H", payload[5:7])[0]
        self.chunk_sec = struct.unpack("<I", payload[7:11])[0]
        self.device_id = bytes(payload[11:27])
        self.device_nonce = bytes(payload[27:43])
        self.auth_mode = payload[43]
        self.frag_size = cfg.BLE_FRAG_SIZE_GUESS
        if (self.mtu or 0) < cfg.MIN_MTU_REQUIRED:
            print(f"  [!] Unsupported MTU {self.mtu} (<{cfg.MIN_MTU_REQUIRED}); refusing transfer")
            return False
        print(f"  proto_ver={proto_ver} mtu={self.mtu} chunk_sec={self.chunk_sec} "
              f"frag_size={self.frag_size} mode={self.auth_mode} "
              f"device={self.device_id.hex()[:8]}...")
        # v3: no key in HELLO_ACK by design. Session key is derived locally
        # after AUTH. Any key bytes here would be a protocol violation.
        if len(payload) != 44:
            print("  [!] HELLO_ACK wrong length — rejecting (possible v2 peer)")
            return False
        self.session_key = None
        self.master_key = None
        return True

    async def _send_ready(self) -> None:
        if self.session_key is None or getattr(self, "_auth_transcript", None) is None:
            raise RuntimeError("READY without session key")
        client_finish = finish_proof(self.session_key, CLIENT_DOM, self._auth_transcript)
        await self.write_ctrl(cfg.PKT_READY, self.next_seq(),
                              struct.pack("<I", self.session_id or 0) + bytes(client_finish))

    def _part_paths(self, file_id: int):
        hex8 = f"{file_id:016x}"
        return (cfg.OUTPUT_DIR / f"file_{hex8}.part",
                cfg.OUTPUT_DIR / f"file_{hex8}.part.json")

    def _close_part(self):
        fh = self._part_fh
        self._part_fh = None
        self._part_id = None
        if fh is not None:
            try:
                fh.close()
            except Exception:
                pass

    def _discard_part(self, file_id: int):
        self._close_part()
        part, side = self._part_paths(file_id)
        for path in (part, side):
            try:
                path.unlink(missing_ok=True)
            except Exception:
                pass

    def _open_part(self, file_id: int, total: int):
        self._close_part()
        try:
            cfg.OUTPUT_DIR.mkdir(exist_ok=True)
            part, _side = self._part_paths(file_id)
            if not part.exists() or part.stat().st_size != total:
                with open(part, "wb") as fh:
                    fh.truncate(total)
            self._part_fh = open(part, "r+b")
            self._part_id = file_id
        except Exception as e:
            print(f"  [!] part file unavailable: {e} — continuing without resume")
            self._part_fh = None
            self._part_id = None

    def _load_resume_state(self, file_id: int, file_crc: int,
                           total: int, total_frags: int):
        """Returns (resume_from, part_bytes, received) from a previous attempt.

        Only bytes actually present on disk are trusted — never prefills zeros.
        """
        part, side = self._part_paths(file_id)
        try:
            meta = json.loads(side.read_text())
        except Exception:
            return 0, b"", set()
        if (meta.get("crc") != f"{file_crc:08x}" or meta.get("total") != total
                or meta.get("total_frags") != total_frags
                or meta.get("frag_size") != cfg.BLE_FRAG_SIZE_GUESS):
            return 0, b"", set()
        try:
            part_bytes = part.read_bytes()
        except Exception:
            return 0, b"", set()
        available = len(part_bytes) // cfg.BLE_FRAG_SIZE_GUESS
        received = {s for s in meta.get("received", [])
                    if isinstance(s, int) and 0 <= s < total_frags and s < available}
        contig = -1
        while contig + 1 in received:
            contig += 1
        resume = contig + 1
        if resume <= 0 or resume >= total_frags:
            return 0, b"", set()
        return resume, part_bytes, received

    def _write_part_fragment(self, file_id: int, seq: int, plain: bytes):
        if self._part_fh is None or self._part_id != file_id:
            return
        try:
            self._part_fh.seek(seq * cfg.BLE_FRAG_SIZE_GUESS)
            self._part_fh.write(plain)
        except Exception:
            self._close_part()

    def _save_part_meta(self, file_id: int, file_crc: int,
                        total: int, total_frags: int, received: set):
        try:
            _part, side = self._part_paths(file_id)
            if self._part_fh is not None:
                self._part_fh.flush()
                # Survive laptop power loss, not just process exit.
                os.fsync(self._part_fh.fileno())
            side.write_text(json.dumps({
                "crc": f"{file_crc:08x}", "total": total,
                "total_frags": total_frags,
                "frag_size": cfg.BLE_FRAG_SIZE_GUESS,
                "received": sorted(received),
            }))
        except Exception:
            pass

    def _completed_ok(self, file_id: int, file_crc: int, total: int) -> bool:
        rec = self.completed.get(file_id)
        if not rec or rec.get("crc") != f"{file_crc:08x}" or rec.get("size") != total:
            return False
        try:
            return crc32(Path(rec["path"]).read_bytes()) == file_crc
        except Exception:
            return False

    def _remember_completed(self, file_id: int, file_crc: int, total: int, path: Path):
        self.completed[file_id] = {"crc": f"{file_crc:08x}", "size": total,
                                   "path": str(path)}
        while len(self.completed) > 16:
            self.completed.pop(next(iter(self.completed)))

    async def _handle_announce(self, pkt: Packet):
        async with self._file_state_lock:
            await self._handle_announce_locked(pkt)

    async def _handle_announce_locked(self, pkt: Packet):
        p = pkt.payload
        if len(p) < 21:
            print("  [!] FILE_ANNOUNCE payload too short")
            return
        total = struct.unpack("<I", p[1:5])[0]
        total_frags = struct.unpack("<H", p[5:7])[0]
        file_crc = struct.unpack("<I", p[7:11])[0]
        device_start_seq = struct.unpack("<H", p[11:13])[0]
        file_id = struct.unpack("<Q", p[13:21])[0]
        print(f"  FILE_ANNOUNCE: total={total}B frags={total_frags} "
              f"crc={file_crc:#010x} file_id={file_id:#018x} "
              f"device_offered_resume={device_start_seq}")

        key = None
        if self.session_key:
            key = derive_file_key(self.session_key, self.session_id, file_id)

        self.current_file = IncomingFile(
            file_id=file_id, total_bytes=total, total_frags=total_frags,
            expected_crc=file_crc, key=key, session_id=self.session_id,
            frag_size=self.frag_size)
        self.file_done_event.clear()
        async with self._data_lock:
            resume_from = 0
            if total_frags > 0 and device_start_seq == total_frags \
                    and self._completed_ok(file_id, file_crc, total):
                # Host already verified this file (lost FILE_DONE_ACK case).
                resume_from = total_frags
                self.current_file.received_frags = set(range(total_frags))
                self.current_file.contig_seq = total_frags - 1
                print(f"  [resume] {file_id:016x} already completed — confirming")
            else:
                resume_from, part_bytes, received = self._load_resume_state(
                    file_id, file_crc, total, total_frags)
                if resume_from > 0:
                    self.current_file.buffer = bytearray(total)
                    self.current_file.buffer[:len(part_bytes)] = part_bytes
                    self.current_file.received_frags = set(received)
                    contig = -1
                    while contig + 1 in self.current_file.received_frags:
                        contig += 1
                    self.current_file.contig_seq = contig
                    print(f"  [resume] {file_id:016x} continuing at {resume_from}/{total_frags}")
                else:
                    self.current_file.contig_seq = -1
                    self._discard_part(file_id)
                self._open_part(file_id, total)
        self.bench.reset_file(file_id, total, total_frags, resume_from)

        sent = await self.write_ack(cfg.PKT_FILE_ANNOUNCE_ACK, self.next_seq(),
                                    struct.pack("<HH", pkt.seq, resume_from),
                                    required=True)
        if not sent:
            print("  [!] ANNOUNCE_ACK write failed — keeping state for firmware retry")
            return
        print(f"  -> FILE_ANNOUNCE_ACK sent (resume_from={resume_from})")
        self._emit({"type": "announce", "file_id": f"{file_id:016x}",
                    "total_bytes": total, "total_frags": total_frags})

    async def _handle_data_packet(self, pkt: Packet):
        ack_seq: int | None = None
        async with self._data_lock:
            if pkt.type != cfg.PKT_DATA:
                return
            f = self.current_file
            if not f:
                print("  [!] DATA received with no active file — ignoring")
                return
            if not f.key:
                print("  [!] No key available — cannot decrypt fragment")
                return
            seq = pkt.seq
            raw = pkt.payload
            if len(raw) < cfg.CRYPTO_TAG_BYTES:
                print(f"  [!] DATA fragment too short (seq={seq})")
                return
            plain = decrypt_fragment(
                f.key, f.session_id,
                f.file_id, seq, len(raw) - cfg.CRYPTO_TAG_BYTES, raw)
            if plain is None:
                self.bench.decrypt_fail += 1
                # Duplicate cumulative ACK so firmware retransmits the hole.
                contig = f.contig_seq
                if contig >= 0 and len(f.received_frags) % 8 == 0:
                    ack_seq = contig
                # Fall through to send below (outside data lock).
            else:
                if seq in f.received_frags:
                    self.bench.duplicates += 1
                f.add_fragment(seq, plain)
                self._write_part_fragment(f.file_id, seq, plain)
                self.bench.t_send[seq] = time.perf_counter()
                n = len(f.received_frags)
                if n % 20 == 0 or n == f.total_frags:
                    print(f"  progress: {n}/{f.total_frags} fragments")
                    self._emit({"type": "progress", "file_id": f"{f.file_id:016x}",
                                "received": n, "total_frags": f.total_frags})
                # Cumulative ACK: highest contiguous seq, at least once per 8-frag window.
                contig = f.contig_seq
                if contig >= 0 and (n % 8 == 0 or (contig + 1) % 8 == 0
                                    or n == f.total_frags):
                    ack_seq = contig
                    self._save_part_meta(f.file_id, f.expected_crc, f.total_bytes,
                                         f.total_frags, f.received_frags)
        if ack_seq is not None:
            await self.write_ack(cfg.PKT_ACK, self.next_seq(),
                                 struct.pack("<HB", ack_seq & 0xFFFF, 0x00))
        self.bench.note_data_arrival()

    async def _handle_file_done(self, pkt: Packet):
        async with self._file_state_lock:
            await self._handle_file_done_locked(pkt)

    async def _handle_file_done_locked(self, pkt: Packet):
        p = pkt.payload
        if len(p) < 16:
            print("  [!] FILE_DONE payload too short")
            return
        file_id = struct.unpack("<Q", p[0:8])[0]
        file_crc = struct.unpack("<I", p[8:12])[0]
        total = struct.unpack("<I", p[12:16])[0]

        f = self.current_file
        ok = False
        data = b""
        out_path: Path | None = None
        meta_path: Path | None = None
        is_ogg = False
        from_cache = False
        if f and f.file_id == file_id:
            data = bytes(f.buffer[:total])
            actual_crc = crc32(data)
            ok = (actual_crc == file_crc) and (len(f.received_frags) == f.total_frags)
            if not ok and self._completed_ok(file_id, file_crc, total):
                # Duplicate FILE_DONE for an already-verified file (the
                # success ACK was lost). Re-ACK without re-saving.
                print(f"  duplicate FILE_DONE for completed {file_id:016x} — re-ACKing")
                ok = True
                from_cache = True
            if ok and not from_cache:
                cfg.OUTPUT_DIR.mkdir(exist_ok=True)
                ext = ".ogg" if data[:4] == b"OggS" else ".wav"
                is_ogg = (ext == ".ogg")
                out_path = cfg.OUTPUT_DIR / f"file_{file_id:016x}{ext}"
                meta_path = cfg.OUTPUT_DIR / f"file_{file_id:016x}.json"
                # Durable before FILE_DONE_ACK: the ACK means stored, not cached.
                with open(out_path, "wb") as fh:
                    fh.write(data)
                    fh.flush()
                    os.fsync(fh.fileno())
                print(f"  File complete and CRC verified -> {out_path} ({total} bytes)")
                try:
                    meta_path.write_text(json.dumps({
                        "file_id": f"{file_id:016x}", "total": total,
                        "total_frags": f.total_frags,
                        "expected_crc": f"{file_crc:08x}",
                        "actual_crc": f"{actual_crc:08x}",
                        "mtu": self.mtu, "frag_size": self.frag_size,
                        "duplicates": self.bench.duplicates,
                        "decrypt_fail": self.bench.decrypt_fail,
                    }, indent=2))
                except Exception:
                    pass
                if not is_ogg:
                    print(f"  [ingest] skip {out_path.name}: WAV not accepted "
                          "(audio/ogg only) — kept on disk")
                elif total > cfg.INGEST_MAX_BYTES:
                    print(f"  [ingest] skip {out_path.name}: {total}B > "
                          f"{cfg.INGEST_MAX_BYTES}B max — kept on disk")
                elif not self.ingest_enabled:
                    print(f"  [ingest] disabled — kept {out_path.name} on disk")
            else:
                print(f"  [!] CRC mismatch or missing fragments "
                      f"(expected crc={file_crc:#010x}, got={actual_crc:#010x}, "
                      f"frags {len(f.received_frags)}/{f.total_frags})")
        else:
            print("  [!] FILE_DONE for unknown/mismatched file_id")

        sent = await self.write_ack(cfg.PKT_FILE_DONE_ACK, self.next_seq(),
                                      struct.pack("<HBBB", pkt.seq, 0x01 if ok else 0x00, 0, 0),
                                      required=True)
        if not sent:
            print("  [!] FILE_DONE_ACK write failed — keeping state for firmware retry")
            return
        print(f"  -> FILE_DONE_ACK sent (status={'ok' if ok else 'fail'})")
        async with self._data_lock:
            if ok and out_path is not None:
                self._remember_completed(file_id, file_crc, total, out_path)
            if ok:
                self._discard_part(file_id)  # transfer over: resume state obsolete
            else:
                self._close_part()  # keep .part for the firmware retry
        ingest_init_status = ""
        vad_init_status = ""
        if ok and out_path is not None:
            if not is_ogg:
                ingest_init_status, vad_init_status = "skipped-wav", "skipped"
            elif total > cfg.INGEST_MAX_BYTES:
                ingest_init_status, vad_init_status = "skipped-too-large", "skipped"
            elif not self.ingest_enabled:
                ingest_init_status, vad_init_status = "disabled", "disabled"
            else:
                ingest_init_status = "pending"
                vad_init_status = "pending" if self.vad_enabled else "disabled"
        try:
            self.bench.finalize(ok, total, self.mtu or 0, self.frag_size,
                                ingest_status=ingest_init_status, vad_status=vad_init_status)
        except Exception as e:
            print(f"  [!] bench finalize failed: {e}")
        self._emit({"type": "file_done", "file_id": f"{file_id:016x}",
                    "crc_ok": ok, "total_bytes": total,
                    "ingest_status": ingest_init_status, "vad_status": vad_init_status})

        if ok and out_path is not None and meta_path is not None \
                and ingest_init_status == "pending":
            try:
                asyncio.create_task(
                    self._ingest_in_background(bytes(data), out_path, meta_path, file_id))
            except Exception as e:
                print(f"  [!] ingest schedule failed: {e}")

        if self.current_file is f and f.file_id == file_id:
            self.current_file = None
        self.file_done_event.set()


async def _sleep_or_stopped(is_stopped, delay: float) -> bool:
    waited = 0.0
    while waited < delay:
        if is_stopped():
            return True
        await asyncio.sleep(0.5)
        waited += 0.5
    return is_stopped()


async def supervise_link(client, *, log, is_stopped, on_ready=None,
                         on_alive=None, tick=None) -> None:
    """Single reconnect supervisor: OFFLINE -> DISCOVER -> CONNECT -> READY
    -> MONITOR. Returns when is_stopped() is true. Steady-state reconnect
    decisions live only here — not in individual call sites."""
    ready_once = False
    setup_fails = 0
    while not is_stopped():
        # 1. DISCOVER: no pairing/recovery until the device is visible.
        try:
            device = await client.rediscover(timeout=3.0)
        except Exception as e:
            log(f"[ble] scan error: {e}")
            if await _sleep_or_stopped(is_stopped, 2.0):
                return
            continue
        if device is None:
            log("[ble] Checkpoint offline — waiting for power/advertising...")
            if await _sleep_or_stopped(is_stopped, 2.0):
                return
            continue
        # 2+3. CONNECT + HANDSHAKE on the fresh BLEDevice. The device was
        # just seen advertising, so repeated failure here (either phase)
        # means host and device disagree about bonding — escalate.
        try:
            await client.connect(device)
            await client.do_handshake()
            if not (client.client and client.client.is_connected):
                raise ConnectionError("BLE disappeared during handshake")
        except asyncio.CancelledError:
            raise
        except Exception as e:
            client.link_state = "down"
            try:
                await client.disconnect()
            except Exception:
                pass
            setup_fails += 1
            log(f"[ble] setup failed: {e}")
            if setup_fails >= 3:
                setup_fails = 0
                if getattr(client, "enroll", False):
                    try:
                        await client.clear_stale_bond(log)
                    except Exception:
                        pass
                else:
                    log("[ble] not clearing bond in normal mode (use enroll or USB recovery)")
            if await _sleep_or_stopped(is_stopped, 2.0):
                return
            continue
        setup_fails = 0
        # 4. READY (once) + per-cycle alive notification.
        if not ready_once:
            ready_once = True
            if on_ready is not None:
                await on_ready()
        if on_alive is not None:
            await on_alive()
        # 5. MONITOR the live connection. Never clear link_lost here: a
        # disconnect between handshake and this loop must not be erased.
        while not is_stopped():
            if client.link_lost.is_set():
                break
            if not (client.client and client.client.is_connected):
                break
            if tick is not None:
                try:
                    await tick()
                except Exception:
                    # A failed status poll can also reveal a dead link.
                    if not (client.client and client.client.is_connected):
                        break
            await asyncio.sleep(0.5)
        if is_stopped():
            return
        log("[ble] link disappeared — rediscovering...")
        try:
            await client.disconnect()
        except Exception:
            pass


async def find_device(name_or_addr: str | None) -> str:
    target = name_or_addr or cfg.DEVICE_NAME
    print(f"Scanning for '{target}' ...")
    devices = await BleakScanner.discover(timeout=8.0)
    for d in devices:
        if d.name == target or d.address == target:
            print(f"Found: {d.name} [{d.address}]")
            return d.address
    raise RuntimeError(f"Device '{target}' not found. Nearby devices: "
                       f"{[(d.name, d.address) for d in devices]}")


def parse_claim_hex(claim_hex: str) -> bytes:
    text = claim_hex.strip()
    idx = text.lower().find("checkpoint://claim?")
    if idx >= 0:
        query = text[idx:].split("?", 1)[1]
        fields = dict(part.split("=", 1) for part in query.split("&") if "=" in part)
        text = fields.get("key", "")
    try:
        claim_key = bytes.fromhex(text.strip())
    except ValueError:
        raise RuntimeError("--claim must be 64 hex chars (32 bytes)")
    if len(claim_key) == 16:
        raise RuntimeError(
            "That looks like the device id (16 bytes), not the claim key. "
            "Paste the `claim` line (64 hex chars) or the full checkpoint://claim?... URI.")
    if len(claim_key) != 32:
        raise RuntimeError("--claim must be 64 hex chars (32 bytes)")
    return claim_key


def build_cli_parser():
    import argparse
    parser = argparse.ArgumentParser(
        prog="client.py", description="Checkpoint BLE sync client (protocol v3)")
    parser.add_argument("device", nargs="?",
                        help="BLE address or advertised name (scanned when omitted)")
    parser.add_argument("--bench", action="store_true")
    parser.add_argument("--csv", action="store_true")
    parser.add_argument("--ingest", dest="ingest", action="store_true", default=None)
    parser.add_argument("--no-ingest", dest="ingest", action="store_false")
    parser.add_argument("--ingest-url", default=cfg.INGEST_BASE_URL)
    parser.add_argument("--user-id", default=cfg.INGEST_USER_ID)
    parser.add_argument("--no-queue-wait", action="store_true")
    parser.add_argument("--queue-wait-timeout", type=float, default=cfg.INGEST_POLL_TIMEOUT_S)
    parser.add_argument("--vad", dest="vad", action="store_true", default=None)
    parser.add_argument("--no-vad", dest="vad", action="store_false")
    parser.add_argument("--vad-threshold", type=float, default=cfg.VAD_THRESHOLD)
    parser.add_argument("--min-speech", type=float, default=cfg.VAD_MIN_SPEECH_S)
    parser.add_argument("--keep", action="store_true")
    parser.add_argument("--no-rebond", action="store_true")
    parser.add_argument("--enroll", action="store_true")
    parser.add_argument("--claim", default=None,
                        help="64-hex claim key from USB `auth export` (prompted securely if omitted)")
    return parser


def parse_cli_args(argv=None):
    if argv is None:
        argv = sys.argv[1:]
    # --cli is consumed by __main__ dispatch; never a client option.
    argv = [a for a in argv if a != "--cli"]
    opts = build_cli_parser().parse_args(argv)
    ingest_enabled = cfg.INGEST_ENABLED_DEFAULT if opts.ingest is None else opts.ingest
    vad_enabled = cfg.VAD_ENABLED_DEFAULT if opts.vad is None else opts.vad
    return {
        "device": opts.device,
        "bench": bool(opts.bench or opts.csv),
        "ingest_enabled": ingest_enabled,
        "ingest_url": opts.ingest_url.rstrip("/"),
        "ingest_user": opts.user_id,
        "ingest_poll_enabled": cfg.INGEST_POLL_ENABLED_DEFAULT and not opts.no_queue_wait,
        "ingest_poll_timeout": opts.queue_wait_timeout,
        "ingest_poll_interval": cfg.INGEST_POLL_INTERVAL_S,
        "vad_enabled": vad_enabled,
        "vad_threshold": opts.vad_threshold,
        "vad_min_speech": opts.min_speech,
        "ingest_delete": cfg.INGEST_DELETE_AFTER_DEFAULT and not opts.keep,
        "auto_rebond": cfg.BLE_AUTO_REBOND_DEFAULT and not opts.no_rebond,
        "enroll": bool(opts.enroll or opts.claim),
        "claim_hex": opts.claim,
    }


async def cli_async_main() -> None:
    cli = parse_cli_args()
    bench_flag = cli["bench"]
    ingest_enabled = cli["ingest_enabled"]
    ingest_url = cli["ingest_url"]
    ingest_user = cli["ingest_user"]
    ingest_poll_enabled = cli["ingest_poll_enabled"]
    ingest_poll_timeout = cli["ingest_poll_timeout"]
    ingest_poll_interval = cli["ingest_poll_interval"]
    vad_enabled = cli["vad_enabled"]
    vad_threshold = cli["vad_threshold"]
    vad_min_speech = cli["vad_min_speech"]
    ingest_delete = cli["ingest_delete"]
    auto_rebond = cli["auto_rebond"]
    enroll = cli["enroll"]
    claim_key = None
    if cli["claim_hex"]:
        claim_key = parse_claim_hex(cli["claim_hex"])
        enroll = True
    arg = cli["device"]
    bench_csv = None
    if bench_flag:
        bench_csv = cfg.BENCH_DIR / f"benchmark_{time.strftime('%Y%m%d_%H%M%S')}.csv"
        print(f"Benchmark capture -> {bench_csv}")
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
    if enroll and claim_key is None:
        import getpass
        try:
            entered = getpass.getpass("Claim key (from USB `auth export`, input hidden): ")
        except (EOFError, KeyboardInterrupt):
            raise RuntimeError("Enrollment cancelled — no claim key given")
        if not entered.strip():
            raise RuntimeError("--enroll requires a claim key from `auth export` over USB")
        claim_key = parse_claim_hex(entered)
    client = CheckpointClient(
        address, bench_csv=bench_csv, ingest_enabled=ingest_enabled,
        ingest_base_url=ingest_url, ingest_user_id=ingest_user,
        ingest_delete_after=ingest_delete, ingest_poll_enabled=ingest_poll_enabled,
        ingest_poll_timeout=ingest_poll_timeout, ingest_poll_interval=ingest_poll_interval,
        vad_enabled=vad_enabled, vad_model=vad_model,
        vad_threshold=vad_threshold, vad_min_speech_s=vad_min_speech,
        auto_rebond=auto_rebond, claim_key=claim_key, enroll=enroll)
    async def _on_alive():
        print("\nListening for file transfers. Press Ctrl+C to stop.\n")

    try:
        await supervise_link(client, log=print, is_stopped=lambda: False,
                             on_alive=_on_alive)
    except KeyboardInterrupt:
        print("\nShutting down...")
    finally:
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
                    if not any(r.get("ingest_status") == "pending"
                               for r in client.bench.rows()):
                        break
        except Exception:
            pass
        await client.disconnect()
        if bench_csv:
            try:
                client.bench.rewrite_csv()
                print(f"Benchmark csv saved: {bench_csv} ({len(client.bench_rows())} files)")
                jpath = bench_csv.with_suffix(".json")
                jpath.write_text(json.dumps(client.bench_rows(), indent=2))
                print(f"Benchmark json saved: {jpath}")
            except Exception as e:
                print(f"  [!] bench save failed: {e}")
        else:
            client.bench.close()


def cli_main() -> None:
    asyncio.run(cli_async_main())
