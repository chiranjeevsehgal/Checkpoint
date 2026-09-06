"""Checkpoint BLE client — minimal Tkinter bootstrap GUI.

Reuses the existing application logic in `client - ingestion.py`
(CheckpointClient, VAD gate, ingestion uploader) instead of rebuilding it.
Ingest column: READY = complete accepted, Kafka publish pending;
SUBMITTED = queued to Kafka topic transcription.jobs.v1 (queue-wait poll).

Run:
    python gui.py
    pip install -r requirements.txt   # bleak, cryptography, torch, ...

Notes:
- All BLE/VAD/upload work runs on a background worker thread with its own
  asyncio loop, so the Tk UI never freezes.
- Graceful shutdown: stops notifications, waits for pending uploads, then
  disconnects WITHOUT unpairing, so the OS bond is kept and the next
  Connect does not require re-pairing.
"""

import asyncio
import importlib.util
import json
import queue
import sys
import threading
import time
import tkinter as tk
from pathlib import Path
from tkinter import ttk

HERE = Path(__file__).resolve().parent
CLIENT_PATH = HERE / "client - ingestion.py"

_spec = importlib.util.spec_from_file_location("checkpoint_client_ingest", str(CLIENT_PATH))
cli = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(cli)


# ---------------------------------------------------------------------------
# stdout -> UI queue (thread-safe log panel)
# ---------------------------------------------------------------------------

class QueueWriter:
    def __init__(self, q: queue.Queue, tag: str = "log"):
        self._q = q
        self._tag = tag
        self._buf = ""

    def write(self, s: str):
        self._buf += s
        while "\n" in self._buf:
            line, self._buf = self._buf.split("\n", 1)
            if line.strip():
                try:
                    self._q.put_nowait((self._tag, line[:2000]))
                except queue.Full:
                    pass

    def flush(self):
        if self._buf.strip():
            try:
                self._q.put_nowait((self._tag, self._buf.strip()[:2000]))
            except queue.Full:
                pass
            self._buf = ""


# ---------------------------------------------------------------------------
# App
# ---------------------------------------------------------------------------

class App:
    POLL_MS = 100
    MAX_LOG_LINES = 1000

    def __init__(self, root: tk.Tk):
        self.root = root
        root.title("Checkpoint BLE client")
        root.geometry("760x580")
        root.minsize(640, 480)

        self.q: queue.Queue = queue.Queue()
        self._orig_stdout = sys.stdout
        sys.stdout = QueueWriter(self.q)  # type: ignore

        # Worker thread + asyncio loop (all BLE lives here)
        self.loop: asyncio.AbstractEventLoop | None = None
        self.thread: threading.Thread | None = None
        self.client = None
        self.listener_task = None
        self.stop_evt: asyncio.Event | None = None
        self.connected = False
        self.busy = False
        self.closing = False

        self._build_widgets()
        self._set_status("idle", "grey")
        self.root.after(self.POLL_MS, self._drain)
        self.root.protocol("WM_DELETE_WINDOW", self.on_close)

    # -- widgets ------------------------------------------------------
    def _build_widgets(self):
        top = ttk.Frame(self.root, padding=8)
        top.pack(fill=tk.X)

        ttk.Label(top, text="Device:").pack(side=tk.LEFT)
        self.device_var = tk.StringVar(value=cli.DEVICE_NAME)
        ttk.Entry(top, textvariable=self.device_var, width=22).pack(side=tk.LEFT, padx=4)

        self.btn_connect = ttk.Button(top, text="Connect", command=self.on_connect)
        self.btn_connect.pack(side=tk.LEFT, padx=4)
        self.btn_disc = ttk.Button(top, text="Disconnect", command=self.on_disconnect,
                                   state=tk.DISABLED)
        self.btn_disc.pack(side=tk.LEFT)

        self.dot = tk.Label(top, text="\u25cf", fg="grey", font=("TkDefaultFont", 14))
        self.dot.pack(side=tk.LEFT, padx=8)
        self.status_var = tk.StringVar(value="idle")
        ttk.Label(top, textvariable=self.status_var).pack(side=tk.LEFT)

        opts = ttk.LabelFrame(self.root, text="Options", padding=6)
        opts.pack(fill=tk.X, padx=8)
        self.ingest_var = tk.BooleanVar(value=True)
        self.vad_var = tk.BooleanVar(value=True)
        self.bench_var = tk.BooleanVar(value=True)
        self.keep_var = tk.BooleanVar(value=False)
        ttk.Checkbutton(opts, text="Ingest", variable=self.ingest_var).pack(side=tk.LEFT, padx=4)
        ttk.Checkbutton(opts, text="VAD", variable=self.vad_var).pack(side=tk.LEFT)
        ttk.Checkbutton(opts, text="Bench CSV", variable=self.bench_var).pack(side=tk.LEFT, padx=4)
        ttk.Checkbutton(opts, text="Keep files", variable=self.keep_var).pack(side=tk.LEFT)
        ttk.Label(opts, text="Thr:").pack(side=tk.LEFT, padx=(8, 0))
        self.thr_var = tk.StringVar(value=str(cli.VAD_THRESHOLD))
        ttk.Entry(opts, textvariable=self.thr_var, width=6).pack(side=tk.LEFT)
        ttk.Label(opts, text="Min(s):").pack(side=tk.LEFT, padx=(8, 0))
        self.min_var = tk.StringVar(value=str(cli.VAD_MIN_SPEECH_S))
        ttk.Entry(opts, textvariable=self.min_var, width=6).pack(side=tk.LEFT)

        files = ttk.LabelFrame(self.root, text="Audio items", padding=6)
        files.pack(fill=tk.BOTH, expand=False, padx=8, pady=(4, 0))
        cols = ("size", "ble", "vad", "ingest")
        self.tree = ttk.Treeview(files, columns=cols, height=7, show="tree headings")
        self.tree.heading("#0", text="File")
        self.tree.heading("size", text="Size")
        self.tree.heading("ble", text="BLE")
        self.tree.heading("vad", text="VAD")
        self.tree.heading("ingest", text="Ingest")
        self.tree.column("#0", width=180)
        self.tree.column("size", width=80)
        self.tree.column("ble", width=110)
        self.tree.column("vad", width=130)
        self.tree.column("ingest", width=150)
        self.tree.pack(fill=tk.X)
        prow = ttk.Frame(files)
        prow.pack(fill=tk.X, pady=(4, 0))
        self.prog_label = ttk.Label(prow, text="active: —")
        self.prog_label.pack(side=tk.LEFT)
        self.prog = ttk.Progressbar(prow, mode="determinate", maximum=100)
        self.prog.pack(side=tk.LEFT, fill=tk.X, expand=True, padx=8)

        logs = ttk.LabelFrame(self.root, text="Logs", padding=6)
        logs.pack(fill=tk.BOTH, expand=True, padx=8, pady=4)
        self.log = tk.Text(logs, height=12, wrap=tk.NONE, state=tk.DISABLED)
        sb = ttk.Scrollbar(logs, command=self.log.yview)
        self.log.configure(yscrollcommand=sb.set)
        self.log.pack(side=tk.LEFT, fill=tk.BOTH, expand=True)
        sb.pack(side=tk.RIGHT, fill=tk.Y)

    # -- status/log ---------------------------------------------------
    def _set_status(self, text: str, color: str):
        self.status_var.set(text)
        try:
            self.dot.configure(fg=color)
        except tk.TclError:
            pass

    def _log(self, line: str):
        self.log.configure(state=tk.NORMAL)
        self.log.insert(tk.END, line + "\n")
        lines = int(self.log.index("end-1c").split(".")[0])
        if lines > self.MAX_LOG_LINES:
            self.log.delete("1.0", f"{lines - self.MAX_LOG_LINES}.0")
        self.log.see(tk.END)
        self.log.configure(state=tk.DISABLED)

    def _drain(self):
        if self.closing:
            return
        try:
            while True:
                tag, payload = self.q.get_nowait()
                if tag == "log":
                    self._log(payload)
                elif tag == "event":
                    self._apply_event(payload)
                elif tag == "status":
                    text, color = payload
                    self._set_status(text, color)
        except queue.Empty:
            pass
        self.root.after(self.POLL_MS, self._drain)

    # -- audio-item state ---------------------------------------------
    def _apply_event(self, e: dict):
        kind = e.get("type")
        fid = e.get("file_id", "?")
        if kind == "announce":
            if not self.tree.exists(fid):
                self.tree.insert("", tk.END, iid=fid, text=f"file_{fid}.ogg",
                                 values=(f"{e.get('total_bytes', 0)}B", "0%",
                                         e.get("vad_status", "pending") if "vad_status" in e else "—",
                                         "pending"))
            self.prog_label.configure(text=f"active: file_{fid}.ogg")
            self.prog["value"] = 0
        elif kind == "progress":
            total = max(1, e.get("total_frags", 1))
            n = e.get("received", 0)
            pct = int(100 * n / total)
            if self.tree.exists(fid):
                vals = list(self.tree.item(fid, "values"))
                vals[1] = f"{pct}% ({n}/{total})"
                self.tree.item(fid, values=tuple(vals))
            self.prog["value"] = pct
            self.prog_label.configure(text=f"active: file_{fid}.ogg {pct}%")
        elif kind == "file_done":
            ok = e.get("crc_ok")
            txt = "ok" if ok else "CRC FAIL"
            if self.tree.exists(fid):
                vals = list(self.tree.item(fid, "values"))
                vals[1] = txt
                if e.get("ingest_status"):
                    vals[3] = e["ingest_status"]
                self.tree.item(fid, values=tuple(vals))
            if ok:
                self.prog["value"] = 100
        elif kind == "vad":
            txt = f"{e.get('vad_status')} {e.get('vad_speech_s', '')}s"
            if self.tree.exists(fid):
                vals = list(self.tree.item(fid, "values"))
                vals[2] = txt.strip()
                self.tree.item(fid, values=tuple(vals))
        elif kind == "ingest":
            st = e.get("ingest_status", "")
            extra = e.get("upload_id", "")[:8]
            txt = f"{st} {extra}".strip()
            if e.get("ingest_error"):
                txt = f"{st}: {e['ingest_error'][:60]}"
            if self.tree.exists(fid):
                vals = list(self.tree.item(fid, "values"))
                vals[3] = txt
                if e.get("vad_status"):
                    vals[2] = f"{e['vad_status']} {e.get('vad_speech_s', '')}s".strip()
                self.tree.item(fid, values=tuple(vals))

    # -- worker plumbing ----------------------------------------------
    def _ensure_loop(self):
        if self.thread and self.thread.is_alive():
            return
        self.loop = asyncio.new_event_loop()

        def run():
            asyncio.set_event_loop(self.loop)
            self.loop.run_forever()

        self.thread = threading.Thread(target=run, daemon=True)
        self.thread.start()

    def _submit(self, coro):
        return asyncio.run_coroutine_threadsafe(coro, self.loop)

    def _on_event(self, e: dict):
        try:
            self.q.put_nowait(("event", e))
        except queue.Full:
            pass

    def _set_buttons(self, connected: bool, busy: bool):
        self.connected = connected
        self.busy = busy
        self.btn_connect.configure(state=tk.DISABLED if (connected or busy) else tk.NORMAL)
        self.btn_disc.configure(state=tk.NORMAL if connected else tk.DISABLED)

    # -- connect / disconnect ------------------------------------------
    def on_connect(self):
        if self.connected or self.busy:
            return
        self._ensure_loop()
        self._set_buttons(False, True)
        self._set_status("connecting…", "orange")
        fut = self._submit(self._connect_flow(
            device=self.device_var.get().strip() or cli.DEVICE_NAME,
            ingest=self.ingest_var.get(), vad=self.vad_var.get(),
            thr=self._parse_float(self.thr_var.get(), cli.VAD_THRESHOLD),
            min_s=self._parse_float(self.min_var.get(), cli.VAD_MIN_SPEECH_S),
            keep=self.keep_var.get(), bench=self.bench_var.get(),
        ))
        fut.add_done_callback(self._connect_done)

    @staticmethod
    def _parse_float(s: str, default: float) -> float:
        try:
            return float(s)
        except (TypeError, ValueError):
            return default

    def _connect_done(self, fut):
        try:
            fut.result()
        except Exception as e:
            self.q.put(("log", f"[gui] connect failed: {e}"))
            self.q.put(("status", ("error", "red")))
            self.root.after(0, lambda: self._set_buttons(False, False))

    async def _connect_flow(self, device, ingest, vad, thr, min_s, keep, bench):
        loop = asyncio.get_event_loop()
        # Prewarm torch DLLs BEFORE any WinRT BLE activity: a scan first
        # poisons c10.dll loading afterwards (WinError 1114). Import-only.
        if vad and ingest:
            self.q.put(("log", "[gui] pre-warming VAD libraries …"))
            try:
                await loop.run_in_executor(None, cli.vad_prewarm)
            except Exception as e:
                self.q.put(("log", f"[gui] VAD unavailable: {e}"))
        self.q.put(("log", f"[gui] scanning for '{device}' …"))
        address = await cli.find_device(device)
        bench_csv = None
        if bench:
            bench_csv = cli.BENCH_DIR / f"benchmark_{time.strftime('%Y%m%d_%H%M%S')}.csv"
            self.q.put(("log", f"[gui] benchmark -> {bench_csv}"))
        vad_model = None
        if vad and ingest:
            self.q.put(("log", "[gui] loading Silero VAD model … (first run downloads weights)"))
            try:
                vad_model = await loop.run_in_executor(None, cli.vad_load_model)
                self.q.put(("log", "[gui] VAD model loaded"))
            except Exception as e:
                self.q.put(("log", f"[gui] VAD load failed — fail-open: {e}"))
                vad_model = None
        self.client = cli.CheckpointClient(
            address, bench_csv=bench_csv, ingest_enabled=ingest,
            ingest_base_url=cli.INGEST_BASE_URL, ingest_user_id=cli.INGEST_USER_ID,
            ingest_delete_after=(not keep),
            ingest_poll_enabled=cli.INGEST_POLL_ENABLED_DEFAULT,
            ingest_poll_timeout=cli.INGEST_POLL_TIMEOUT_S,
            ingest_poll_interval=cli.INGEST_POLL_INTERVAL_S,
            vad_enabled=vad, vad_model=vad_model,
            vad_threshold=thr, vad_min_speech_s=min_s, on_event=self._on_event,
        )
        # stash for graceful close
        self.client._gui_bench_csv = bench_csv
        await self.client.connect()
        await self.client.do_handshake()
        self.stop_evt = asyncio.Event()
        self.q.put(("status", ("listening", "green")))
        self.root.after(0, lambda: self._set_buttons(True, False))
        self.q.put(("log", "[gui] listening for file transfers …"))
        self.q.put(("log", f"[gui] queue-wait until SUBMITTED (Kafka {cli.KAFKA_TOPIC_HINT}); verify: docker compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh --topic {cli.KAFKA_TOPIC_HINT} --bootstrap-server kafka:9092"))
        self.listener_task = asyncio.current_task()
        try:
            while not self.stop_evt.is_set():
                await asyncio.sleep(0.5)
        except asyncio.CancelledError:
            pass

    def on_disconnect(self):
        if not self.connected or self.busy:
            return
        self._set_buttons(True, True)
        self._set_status("disconnecting…", "orange")
        fut = self._submit(self._disconnect_flow())
        fut.add_done_callback(self._disconnect_done)

    def _disconnect_done(self, fut):
        try:
            fut.result()
        except Exception as e:
            self.q.put(("log", f"[gui] disconnect error: {e}"))
        self.root.after(0, lambda: (self._set_buttons(False, False),
                                    self._set_status("idle", "grey")))

    async def _disconnect_flow(self):
        # Graceful: stop notifications, drain uploads, disconnect (bond kept).
        if self.stop_evt is not None:
            self.stop_evt.set()
        cli_mod = self.client
        if cli_mod is not None:
            try:
                await cli_mod.disconnect_graceful(wait_pending_s=10.0)
                self.q.put(("log", "[gui] disconnected (bond kept — no re-pair needed)"))
            except Exception as e:
                self.q.put(("log", f"[gui] disconnect_graceful failed: {e}"))
                try:
                    await cli_mod.disconnect()
                except Exception:
                    pass
            finally:
                try:
                    csv_path = getattr(cli_mod, "_gui_bench_csv", None)
                    if csv_path is not None:
                        jpath = csv_path.with_suffix(".json")
                        jpath.write_text(json.dumps(cli_mod.bench_rows(), indent=2))
                        self.q.put(("log", f"[gui] bench saved {csv_path.name}"))
                except Exception:
                    pass
        self.client = None
        self.q.put(("status", ("idle", "grey")))

    # -- close ----------------------------------------------------------
    def on_close(self):
        if self.closing:
            return
        self.closing = True
        self._log("[gui] shutting down …")

        def finish():
            try:
                sys.stdout = self._orig_stdout
            except Exception:
                pass
            try:
                if self.loop is not None:
                    self.loop.call_soon_threadsafe(self.loop.stop)
            except Exception:
                pass
            self.root.destroy()

        if self.connected and self.loop is not None:
            # graceful disconnect in worker, then destroy (bond preserved)
            async def _close_flow():
                try:
                    await self._disconnect_flow()
                finally:
                    self.root.after(0, finish)

            try:
                asyncio.run_coroutine_threadsafe(_close_flow(), self.loop).result(timeout=15)
            except Exception as e:
                self._log(f"[gui] close timeout/error: {e}")
                finish()
        else:
            finish()


def main():
    root = tk.Tk()
    try:
        style = ttk.Style()
        if "vista" in style.theme_names():
            style.theme_use("vista")
        elif "clam" in style.theme_names():
            style.theme_use("clam")
    except Exception:
        pass
    App(root)
    root.mainloop()


if __name__ == "__main__":
    main()
