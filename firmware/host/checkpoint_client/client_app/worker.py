"""Background worker: all BLE/VAD/upload work runs off the UI thread."""

import asyncio
import json
import threading
import time
from dataclasses import dataclass
from pathlib import Path

from . import config as cfg
from .audio_vad import vad_load_model, vad_prewarm
from .ble_client import CheckpointClient, find_device, supervise_link


@dataclass
class ConnectionSettings:
    device: str = cfg.DEVICE_NAME
    ingest: bool = True
    vad: bool = True
    threshold: float = cfg.VAD_THRESHOLD
    min_speech_s: float = cfg.VAD_MIN_SPEECH_S
    keep: bool = False
    bench: bool = False


class BleWorker:
    def __init__(self, on_log, on_event, on_status, on_connected=None):
        self._on_log = on_log
        self._on_event = on_event
        self._on_status = on_status
        self._on_connected = on_connected
        self.loop: asyncio.AbstractEventLoop | None = None
        self.thread: threading.Thread | None = None
        self.client: CheckpointClient | None = None
        self.bench_csv: Path | None = None
        self.stop_evt: asyncio.Event | None = None
        # Serializes instant-apply control writes so rapid successive
        # toggles can't overtake each other on the wire. Only ever
        # acquired on the worker loop thread.
        self._device_lock = asyncio.Lock()

    @property
    def ready(self) -> bool:
        return self.client is not None and self.loop is not None

    def ensure_loop(self) -> None:
        if self.thread and self.thread.is_alive():
            return
        self.loop = asyncio.new_event_loop()

        def run():
            asyncio.set_event_loop(self.loop)
            self.loop.run_forever()

        self.thread = threading.Thread(target=run, daemon=True)
        self.thread.start()

    def submit(self, coro):
        return asyncio.run_coroutine_threadsafe(coro, self.loop)

    def submit_serial(self, coro):
        """Submit a device-control write, serialized against other serial writes."""

        async def _run():
            async with self._device_lock:
                await coro

        return self.submit(_run())

    def stop_loop(self) -> None:
        try:
            if self.loop is not None:
                self.loop.call_soon_threadsafe(self.loop.stop)
        except Exception:
            pass

    async def connect_flow(self, settings: ConnectionSettings) -> None:
        loop = asyncio.get_event_loop()
        if settings.vad and settings.ingest:
            self._on_log("[gui] pre-warming VAD libraries …")
            try:
                await loop.run_in_executor(None, vad_prewarm)
            except Exception as e:
                self._on_log(f"[gui] VAD unavailable: {e}")
        self._on_log(f"[gui] scanning for '{settings.device}' …")
        address = await find_device(settings.device)
        self.bench_csv = None
        if settings.bench:
            self.bench_csv = cfg.BENCH_DIR / f"benchmark_{time.strftime('%Y%m%d_%H%M%S')}.csv"
            self._on_log(f"[gui] benchmark -> {self.bench_csv}")
        vad_model = None
        if settings.vad and settings.ingest:
            self._on_log("[gui] loading Silero VAD model … (first run downloads weights)")
            try:
                vad_model = await loop.run_in_executor(None, vad_load_model)
                self._on_log("[gui] VAD model loaded")
            except Exception as e:
                self._on_log(f"[gui] VAD load failed — fail-open: {e}")
                vad_model = None
        self.client = CheckpointClient(
            address, bench_csv=self.bench_csv,
            ingest_enabled=settings.ingest,
            ingest_base_url=cfg.INGEST_BASE_URL, ingest_user_id=cfg.INGEST_USER_ID,
            ingest_delete_after=(not settings.keep),
            ingest_poll_enabled=cfg.INGEST_POLL_ENABLED_DEFAULT,
            ingest_poll_timeout=cfg.INGEST_POLL_TIMEOUT_S,
            ingest_poll_interval=cfg.INGEST_POLL_INTERVAL_S,
            vad_enabled=settings.vad, vad_model=vad_model,
            vad_threshold=settings.threshold, vad_min_speech_s=settings.min_speech_s,
            on_event=self._on_event)
        self.client._gui_bench_csv = self.bench_csv
        self.stop_evt = asyncio.Event()
        last_poll = [time.monotonic() - cfg.STATUS_POLL_INTERVAL_S]

        async def _first_sync():
            if self._on_connected is not None:
                try:
                    self._on_connected()
                except Exception:
                    pass
            self._on_log("[gui] listening for file transfers …")
            try:
                info = await self.client.req_status()
                self._on_event({"type": "rec_status", **info})
            except Exception as e:
                self._on_log(f"[gui] initial status failed: {e}")
            try:
                sinfo = await self.client.req_storage()
                self._on_event({"type": "storage", **sinfo})
                linfo = await self.client.req_list(0)
                self._on_event({"type": "file_list", **linfo})
            except Exception as e:
                self._on_log(f"[gui] initial storage load failed: {e}")
            self._on_log(f"[gui] queue-wait until SUBMITTED (Kafka {cfg.KAFKA_TOPIC_HINT}); "
                         f"verify: docker compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh "
                         f"--topic {cfg.KAFKA_TOPIC_HINT} --bootstrap-server kafka:9092")

        async def _on_alive():
            self._on_status("listening", "green")

        async def _poll_tick():
            if self.client is None or self.stop_evt.is_set():
                return
            if time.monotonic() - last_poll[0] < cfg.STATUS_POLL_INTERVAL_S:
                return
            last_poll[0] = time.monotonic()
            try:
                info = await self.client.req_status()
                self._on_event({"type": "rec_status", **info})
            except Exception as e:
                self._on_log(f"[gui] status poll failed: {e}")

        def _is_stopped():
            return self.stop_evt is not None and self.stop_evt.is_set()

        try:
            await supervise_link(self.client, log=self._on_log,
                                 is_stopped=_is_stopped,
                                 on_ready=_first_sync, on_alive=_on_alive,
                                 tick=_poll_tick)
        except asyncio.CancelledError:
            pass
        self._on_status("reconnecting", "amber")

    async def disconnect_flow(self) -> None:
        if self.stop_evt is not None:
            self.stop_evt.set()
        client = self.client
        if client is not None:
            try:
                await client.disconnect_graceful(wait_pending_s=10.0)
                self._on_log("[gui] disconnected (bond kept — no re-pair needed)")
            except Exception as e:
                self._on_log(f"[gui] disconnect_graceful failed: {e}")
                try:
                    await client.disconnect()
                except Exception:
                    pass
            finally:
                try:
                    csv_path = getattr(client, "_gui_bench_csv", None)
                    if csv_path is not None:
                        jpath = csv_path.with_suffix(".json")
                        jpath.write_text(json.dumps(client.bench_rows(), indent=2))
                        self._on_log(f"[gui] bench saved {csv_path.name}")
                except Exception:
                    pass
        self.client = None
        self._on_status("idle", "grey")
