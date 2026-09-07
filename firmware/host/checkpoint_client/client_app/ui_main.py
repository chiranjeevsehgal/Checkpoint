"""ttkbootstrap main window. Same features as the original gui.py."""

import asyncio
import queue
import sys
import tkinter as tk
from tkinter import messagebox

import ttkbootstrap as ttk

from . import config as cfg
from . import ui_cards
from .ui_events import ListState, UiRefs, apply_event, ctrl_status_text
from .worker import BleWorker, ConnectionSettings


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


class CheckpointWindow:
    POLL_MS = 100
    MAX_LOG_LINES = 1000

    def __init__(self, root):
        self.root = root
        root.title("Checkpoint BLE client")
        root.geometry("860x680")
        root.minsize(720, 540)

        self.q: queue.Queue = queue.Queue()
        self._orig_stdout = sys.stdout
        sys.stdout = QueueWriter(self.q)  # type: ignore

        self.worker = BleWorker(
            on_log=lambda msg: self._put("log", msg),
            on_event=lambda e: self._put("event", e),
            on_status=lambda text, color: self._put("status", (text, color)),
            on_connected=lambda: self._put("connected", True))
        self.connected = False
        self.busy = False
        self.closing = False
        self._echo_guard = False
        self._bright_after_id: str | None = None
        self.BRIGHT_DEBOUNCE_MS = 500

        self._build_widgets()
        self.refs = UiRefs(
            tree=self.tree, prog=self.prog, prog_label=self.prog_label,
            dev_status_var=self.dev_status_var, led_muted_var=self.led_muted_var,
            bright_var=self.bright_var, sync_var=self.sync_var,
            storage_var=self.storage_var, storage_bar=self.storage_bar,
            dev_tree=self.dev_tree, list_page_var=self.list_page_var,
            list_state=ListState(),
            log=lambda msg: self._put("log", msg),
            storage_refresh=self._storage_refresh_soon)
        self._set_status("idle", "grey")
        self.root.after(self.POLL_MS, self._drain)
        self.root.protocol("WM_DELETE_WINDOW", self.on_close)

    def _put(self, tag, payload):
        try:
            self.q.put_nowait((tag, payload))
        except queue.Full:
            pass

    def _build_widgets(self):
        self.device_var = tk.StringVar(value=cfg.DEVICE_NAME)
        self.btn_connect, self.btn_disc, self.dot, self.status_var = \
            ui_cards.build_connection_bar(self.root, self.device_var,
                                          self.on_connect, self.on_disconnect)
        self.ingest_var = tk.BooleanVar(value=True)
        self.vad_var = tk.BooleanVar(value=True)
        self.bench_var = tk.BooleanVar(value=True)
        self.keep_var = tk.BooleanVar(value=False)
        self.thr_var = tk.StringVar(value=str(cfg.VAD_THRESHOLD))
        self.min_var = tk.StringVar(value=str(cfg.VAD_MIN_SPEECH_S))
        ui_cards.build_options_card(self.root, self.ingest_var, self.vad_var,
                                    self.bench_var, self.keep_var,
                                    self.thr_var, self.min_var)
        self.dev_status_var = tk.StringVar(value="rec: —")
        self.led_muted_var = tk.BooleanVar(value=False)
        self.bright_var = tk.IntVar(value=30)
        self.sync_var = tk.BooleanVar(value=True)
        self.dev_btns = ui_cards.build_device_card(
            self.root,
            {"rec_start": self.on_rec_start, "rec_stop": self.on_rec_stop,
             "status": self.on_status_refresh, "led_toggle": self.on_led_toggle,
             "sync_toggle": self.on_sync_toggle,
             "bright_slide": self.on_bright_slide,
             "bright_release": self.on_bright_release},
            self.dev_status_var, self.led_muted_var, self.bright_var, self.sync_var)
        self.storage_var = tk.StringVar(value="SD: —")
        self.list_page_var = tk.StringVar(value="")
        self.stor_btns = ui_cards.build_storage_card(
            self.root,
            {"refresh": self.on_storage_refresh, "delete": self.on_file_delete,
             "erase": self.on_storage_erase, "next": self.on_list_next,
             "prev": self.on_list_prev},
            self.storage_var, self.list_page_var)
        self.storage_bar = self.stor_btns["bar"]
        self.dev_tree = self.stor_btns["dev_tree"]
        self.tree, self.prog, self.prog_label = ui_cards.build_transfers_card(self.root)
        self.log = ui_cards.build_log_card(self.root)

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
                try:
                    self._dispatch(tag, payload)
                except Exception as e:
                    try:
                        self._log(f"[gui] ui update failed: {e}")
                    except Exception:
                        pass
        except queue.Empty:
            pass
        self.root.after(self.POLL_MS, self._drain)

    def _dispatch(self, tag, payload):
        if tag == "log":
            self._log(payload)
        elif tag == "event":
            if payload.get("type") in ("rec_status", "cmd_resp"):
                self._echo_guard = True
                try:
                    apply_event(self.refs, payload)
                finally:
                    self._echo_guard = False
            else:
                apply_event(self.refs, payload)
        elif tag == "status":
            self._set_status(*payload)
        elif tag == "connected":
            self._set_buttons(True, False)

    def _set_buttons(self, connected: bool, busy: bool):
        self.connected = connected
        self.busy = busy
        self.btn_connect.configure(state="disabled" if (connected or busy) else "normal")
        self.btn_disc.configure(state="normal" if connected else "disabled")
        dev_state = "normal" if connected else "disabled"
        for w in (self.dev_btns["rec_start"], self.dev_btns["rec_stop"],
                  self.dev_btns["status"], self.dev_btns["led_chk"],
                  self.dev_btns["bright"], self.dev_btns["sync_chk"],
                  self.stor_btns["refresh"], self.stor_btns["delete"],
                  self.stor_btns["erase"], self.stor_btns["prev"],
                  self.stor_btns["next"]):
            try:
                w.configure(state=dev_state)
            except tk.TclError:
                pass

    def _device_ready(self) -> bool:
        return self.connected and self.worker.ready and self.worker.loop is not None

    def _device_done(self, action: str, fut):
        try:
            fut.result()
        except Exception as e:
            self._put("log", f"[gui] {action} failed: {e}")

    def _device_refresh_soon(self):
        if self._device_ready():
            try:
                fut = self.worker.submit(self.worker.client.req_status())
                fut.add_done_callback(lambda f: self._device_done("status", f))
            except RuntimeError:
                pass

    def on_rec_start(self):
        if not self._device_ready():
            return
        fut = self.worker.submit(self._rec_start_flow())
        fut.add_done_callback(lambda f: self._device_done("rec-start", f))

    def on_rec_stop(self):
        if not self._device_ready():
            return
        fut = self.worker.submit(self._rec_stop_flow())
        fut.add_done_callback(lambda f: self._device_done("rec-stop", f))

    def on_status_refresh(self):
        if not self._device_ready():
            return
        fut = self.worker.submit(self.worker.client.req_status())
        fut.add_done_callback(lambda f: self._device_done("status", f))

    def on_led_toggle(self):
        if not self._device_ready():
            return
        try:
            muted = bool(self.led_muted_var.get())
            bright = int(self.bright_var.get())
        except (TypeError, ValueError):
            self._put("log", "[gui] LED toggle: bad brightness value")
            return
        self._cancel_bright_debounce()
        fut = self.worker.submit_serial(self._led_flow(muted, bright))
        fut.add_done_callback(lambda f: self._device_done("led-apply", f))

    def on_bright_slide(self, value):
        if self._echo_guard or not self._device_ready():
            return
        self._schedule_bright_apply()

    def on_bright_release(self, _evt=None):
        if self._echo_guard or not self._device_ready():
            return
        self._cancel_bright_debounce()
        try:
            muted = bool(self.led_muted_var.get())
            bright = int(float(self.bright_var.get()))
        except (TypeError, ValueError):
            return
        fut = self.worker.submit_serial(self._led_flow(muted, bright))
        fut.add_done_callback(lambda f: self._device_done("led-apply", f))

    def _schedule_bright_apply(self):
        self._cancel_bright_debounce()
        self._bright_after_id = self.root.after(
            self.BRIGHT_DEBOUNCE_MS, self._send_debounced_bright)

    def _cancel_bright_debounce(self):
        if self._bright_after_id is not None:
            try:
                self.root.after_cancel(self._bright_after_id)
            except tk.TclError:
                pass
            self._bright_after_id = None

    def _send_debounced_bright(self):
        self._bright_after_id = None
        if self.closing or self._echo_guard or not self._device_ready():
            return
        try:
            muted = bool(self.led_muted_var.get())
            bright = int(float(self.bright_var.get()))
        except (TypeError, ValueError):
            return
        fut = self.worker.submit_serial(self._led_flow(muted, bright))
        fut.add_done_callback(lambda f: self._device_done("led-apply", f))

    async def _rec_start_flow(self):
        status = await self.worker.client.cmd_rec_start()
        self._put("log", f"[gui] rec-start status={status}")
        self._device_refresh_soon()

    async def _rec_stop_flow(self):
        status = await self.worker.client.cmd_rec_stop()
        self._put("log", f"[gui] rec-stop status={status}")
        self._device_refresh_soon()

    async def _led_flow(self, muted: bool, bright: int):
        status = await self.worker.client.cmd_led_set(muted, bright)
        self._put("log", f"[gui] led-apply muted={muted} bright={bright} status={status}")
        self._device_refresh_soon()

    def on_sync_toggle(self):
        if not self._device_ready():
            return
        try:
            enabled = bool(self.sync_var.get())
        except (TypeError, ValueError):
            self._put("log", "[gui] sync toggle: bad checkbox value")
            return
        fut = self.worker.submit_serial(self._sync_flow(enabled))
        fut.add_done_callback(lambda f: self._device_done("sync-apply", f))

    async def _sync_flow(self, enabled: bool):
        status = await self.worker.client.cmd_sync_set(enabled)
        self._put("log", f"[gui] sync-apply enabled={enabled} status={status}")
        self._device_refresh_soon()

    def _storage_refresh_soon(self):
        if self._device_ready():
            try:
                fut = self.worker.submit(self.worker.client.req_storage())
                fut.add_done_callback(lambda f: self._device_done("storage", f))
                fut2 = self.worker.submit(
                    self.worker.client.req_list(max(0, self.refs.list_state.start)))
                fut2.add_done_callback(lambda f: self._device_done("file-list", f))
            except RuntimeError:
                pass

    def on_storage_refresh(self):
        if not self._device_ready():
            return
        self.refs.list_state.start = 0
        fut = self.worker.submit(self.worker.client.req_storage())
        fut.add_done_callback(lambda f: self._device_done("storage", f))
        fut2 = self.worker.submit(self.worker.client.req_list(0))
        fut2.add_done_callback(lambda f: self._device_done("file-list", f))

    def on_list_prev(self):
        if not self._device_ready():
            return
        start = max(0, self.refs.list_state.start - max(1, self.refs.list_state.count))
        fut = self.worker.submit(self.worker.client.req_list(start))
        fut.add_done_callback(lambda f: self._device_done("file-list", f))

    def on_list_next(self):
        if not self._device_ready():
            return
        start = self.refs.list_state.start + max(1, self.refs.list_state.count)
        if self.refs.list_state.total and start >= self.refs.list_state.total:
            return
        fut = self.worker.submit(self.worker.client.req_list(start))
        fut.add_done_callback(lambda f: self._device_done("file-list", f))

    def on_file_delete(self):
        if not self._device_ready():
            return
        sel = self.dev_tree.selection()
        if not sel:
            self._put("log", "[gui] delete: no device file selected")
            return
        path = sel[0]
        if not messagebox.askyesno("Delete file",
                                    f"Delete {path} from the pendant?\nThis cannot be undone."):
            return
        fut = self.worker.submit(self._delete_flow(path))
        fut.add_done_callback(lambda f: self._device_done("file-delete", f))

    def on_storage_erase(self):
        if not self._device_ready():
            return
        if not messagebox.askyesno("Erase all recordings",
                                    "Erase ALL recordings from the pendant SD card?\n"
                                    "This cannot be undone."):
            return
        fut = self.worker.submit(self._erase_flow())
        fut.add_done_callback(lambda f: self._device_done("storage-erase", f))

    async def _delete_flow(self, path: str):
        status = await self.worker.client.cmd_file_delete(path)
        self._put("log", f"[gui] file-delete {path} status={ctrl_status_text(status)}")
        self._storage_refresh_soon()

    async def _erase_flow(self):
        arm = await self.worker.client.cmd_storage_erase(cfg.CTRL_ERASE_ARM)
        arm_status = int(arm.get("status", cfg.CTRL_ERR_NOT_READY))
        if arm_status != cfg.CTRL_OK:
            self._put("log", f"[gui] erase arm refused status={ctrl_status_text(arm_status)}")
            return
        res = await self.worker.client.cmd_storage_erase(cfg.CTRL_ERASE_CONFIRM)
        status = int(res.get("status", cfg.CTRL_ERR_NOT_READY))
        self._put("log", f"[gui] erase confirm status={ctrl_status_text(status)} "
                         f"removed={res.get('removed', '?')}")
        self.refs.list_state.start = 0
        self._storage_refresh_soon()

    def on_connect(self):
        if self.connected or self.busy:
            return
        self.worker.ensure_loop()
        self._set_buttons(False, True)
        self._set_status("connecting…", "orange")
        fut = self.worker.submit(self.worker.connect_flow(ConnectionSettings(
            device=self.device_var.get().strip() or cfg.DEVICE_NAME,
            ingest=self.ingest_var.get(), vad=self.vad_var.get(),
            threshold=self._parse_float(self.thr_var.get(), cfg.VAD_THRESHOLD),
            min_speech_s=self._parse_float(self.min_var.get(), cfg.VAD_MIN_SPEECH_S),
            keep=self.keep_var.get(), bench=self.bench_var.get())))
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
            self._put("log", f"[gui] connect failed: {e}")
            self._put("status", ("error", "red"))
            self.root.after(0, lambda: self._set_buttons(False, False))

    def on_disconnect(self):
        if not self.connected or self.busy:
            return
        self._cancel_bright_debounce()
        self._set_buttons(True, True)
        self._set_status("disconnecting…", "orange")
        fut = self.worker.submit(self.worker.disconnect_flow())
        fut.add_done_callback(self._disconnect_done)

    def _disconnect_done(self, fut):
        try:
            fut.result()
        except Exception as e:
            self._put("log", f"[gui] disconnect error: {e}")
        self.root.after(0, lambda: (self._set_buttons(False, False),
                                    self._set_status("idle", "grey")))

    def on_close(self):
        if self.closing:
            return
        self.closing = True
        self._cancel_bright_debounce()
        self._log("[gui] shutting down …")

        def finish():
            try:
                sys.stdout = self._orig_stdout
            except Exception:
                pass
            self.worker.stop_loop()
            self.root.destroy()

        if self.connected and self.worker.loop is not None:
            async def _close_flow():
                try:
                    await self.worker.disconnect_flow()
                finally:
                    self.root.after(0, finish)

            try:
                asyncio.run_coroutine_threadsafe(
                    _close_flow(), self.worker.loop).result(timeout=15)
            except Exception as e:
                self._log(f"[gui] close timeout/error: {e}")
                finish()
        else:
            finish()


def main():
    root = ttk.Window(themename="flatly")
    CheckpointWindow(root)
    root.mainloop()


if __name__ == "__main__":
    main()
