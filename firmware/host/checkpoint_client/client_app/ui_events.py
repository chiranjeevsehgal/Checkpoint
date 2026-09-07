"""Maps BLE worker events onto transfer/device widgets. No BLE code here."""

import tkinter as tk
from collections.abc import Callable
from dataclasses import dataclass, field

from . import config as cfg


@dataclass
class ListState:
    start: int = 0
    total: int = 0
    count: int = 0


@dataclass
class UiRefs:
    tree: object = None
    prog: object = None
    prog_label: object = None
    dev_status_var: object = None
    led_muted_var: object = None
    bright_var: object = None
    sync_var: object = None
    storage_var: object = None
    storage_bar: object = None
    dev_tree: object = None
    list_page_var: object = None
    list_state: ListState = field(default_factory=ListState)
    log: Callable[[str], None] = lambda msg: None
    storage_refresh: Callable[[], None] = lambda: None


def format_bytes(n) -> str:
    try:
        n = int(n)
    except (TypeError, ValueError):
        return "?"
    if n >= 1 << 30:
        return f"{n / (1 << 30):.2f}GB"
    if n >= 1 << 20:
        return f"{n / (1 << 20):.1f}MB"
    if n >= 1 << 10:
        return f"{n / (1 << 10):.0f}KB"
    return f"{n}B"


def ctrl_status_text(status: int) -> str:
    return {
        cfg.CTRL_OK: "ok",
        cfg.CTRL_ERR_NOT_READY: "not-ready",
        cfg.CTRL_ERR_NO_SD: "no-sd",
        cfg.CTRL_ERR_BAD_ARG: "bad-arg",
        cfg.CTRL_ERR_DENIED: "denied",
        cfg.CTRL_ERR_BUSY: "busy",
        cfg.CTRL_ERR_NOT_FOUND: "not-found",
    }.get(int(status), f"0x{int(status):02x}")


def _set_tree_row(tree, iid: str, col: int, text: str) -> None:
    if tree.exists(iid):
        vals = list(tree.item(iid, "values"))
        vals[col] = text
        tree.item(iid, values=tuple(vals))


def _on_announce(refs: UiRefs, e: dict) -> None:
    fid = e.get("file_id", "?")
    if not refs.tree.exists(fid):
        refs.tree.insert("", tk.END, iid=fid, text=f"file_{fid}.ogg",
                         values=(f"{e.get('total_bytes', 0)}B", "0%", "—", "pending"))
    refs.prog_label.configure(text=f"active: file_{fid}.ogg")
    refs.prog["value"] = 0


def _on_progress(refs: UiRefs, e: dict) -> None:
    fid = e.get("file_id", "?")
    total = max(1, e.get("total_frags", 1))
    n = e.get("received", 0)
    pct = int(100 * n / total)
    _set_tree_row(refs.tree, fid, 1, f"{pct}% ({n}/{total})")
    refs.prog["value"] = pct
    refs.prog_label.configure(text=f"active: file_{fid}.ogg {pct}%")


def _on_file_done(refs: UiRefs, e: dict) -> None:
    fid = e.get("file_id", "?")
    _set_tree_row(refs.tree, fid, 1, "ok" if e.get("crc_ok") else "CRC FAIL")
    if e.get("ingest_status"):
        _set_tree_row(refs.tree, fid, 3, e["ingest_status"])
    if e.get("crc_ok"):
        refs.prog["value"] = 100


def _on_vad(refs: UiRefs, e: dict) -> None:
    fid = e.get("file_id", "?")
    _set_tree_row(refs.tree, fid, 2, f"{e.get('vad_status')} {e.get('vad_speech_s', '')}s".strip())


def _on_ingest(refs: UiRefs, e: dict) -> None:
    fid = e.get("file_id", "?")
    status = e.get("ingest_status", "")
    text = f"{status} {e.get('upload_id', '')[:8]}".strip()
    if e.get("ingest_error"):
        text = f"{status}: {e['ingest_error'][:60]}"
    _set_tree_row(refs.tree, fid, 3, text)
    if e.get("vad_status"):
        _set_tree_row(refs.tree, fid, 2,
                      f"{e['vad_status']} {e.get('vad_speech_s', '')}s".strip())


def _on_rec_status(refs: UiRefs, e: dict) -> None:
    rec = "ON" if e.get("recording") else "OFF"
    vad = "speech" if e.get("vad_speech") else ("active" if e.get("vad_active") else "idle")
    led = "muted" if e.get("muted") else f"bright={e.get('brightness')}"
    sync = f"sync:{'on' if e.get('sync', True) else 'off'}"
    refs.dev_status_var.set(
        f"rec: {rec} vad: {vad} pend: {e.get('pending', '?')} "
        f"chunks: {e.get('chunks', '?')} {led} {sync} lvl: {e.get('level_dbfs', '?')}dB")
    try:
        refs.led_muted_var.set(bool(e.get("muted", False)))
        refs.bright_var.set(int(e.get("brightness", 30)))
        refs.sync_var.set(bool(e.get("sync", True)))
    except (TypeError, ValueError):
        pass


def _on_cmd_resp(refs: UiRefs, e: dict) -> None:
    refs.log(f"[gui] device cmd=0x{e.get('cmd'):02x} status={e.get('status')}")
    if "muted" in e:
        try:
            refs.led_muted_var.set(bool(e.get("muted")))
            refs.bright_var.set(int(e.get("brightness", 30)))
        except (TypeError, ValueError):
            pass
    if "sync" in e:
        try:
            refs.sync_var.set(bool(e.get("sync")))
        except (TypeError, ValueError):
            pass
    if e.get("cmd") in (cfg.CTRL_CMD_FILE_DELETE, cfg.CTRL_CMD_STORAGE_ERASE):
        if "removed" in e:
            refs.log(f"[gui] erase removed={e.get('removed')} files")
        refs.storage_refresh()


def _on_storage(refs: UiRefs, e: dict) -> None:
    total, used = e.get("total", 0), e.get("used", 0)
    if total:
        pct = int(100 * used / total)
        refs.storage_var.set(
            f"SD: {format_bytes(used)} / {format_bytes(total)} "
            f"({pct}%) · {e.get('files', '?')} files · {e.get('pending', '?')} pending")
        refs.storage_bar["value"] = min(100, pct)
    else:
        refs.storage_var.set(
            f"SD total unknown · {format_bytes(used)} recordings · "
            f"{e.get('files', '?')} files · {e.get('pending', '?')} pending")
        refs.storage_bar["value"] = 0


def _on_file_list(refs: UiRefs, e: dict) -> None:
    entries = e.get("entries", [])
    refs.list_state.start = int(e.get("start", 0))
    refs.list_state.total = int(e.get("total", 0))
    refs.list_state.count = len(entries)
    for iid in refs.dev_tree.get_children():
        refs.dev_tree.delete(iid)
    for en in entries:
        flags = int(en.get("flags", 0))
        if flags & cfg.CTRL_LIST_FLAG_ACTIVE:
            state = "recording"
        elif flags & cfg.CTRL_LIST_FLAG_PENDING:
            state = "pending"
        else:
            state = "synced"
        refs.dev_tree.insert("", tk.END, iid=en["name"], text=en["name"],
                             values=(format_bytes(en.get("size", 0)), state))
    start, total, count = refs.list_state.start, refs.list_state.total, refs.list_state.count
    refs.list_page_var.set(f"{start + 1 if total else 0}–{start + count} of {total}")


def apply_event(refs: UiRefs, e: dict) -> None:
    kind = e.get("type")
    if kind == "announce":
        _on_announce(refs, e)
    elif kind == "progress":
        _on_progress(refs, e)
    elif kind == "file_done":
        _on_file_done(refs, e)
    elif kind == "vad":
        _on_vad(refs, e)
    elif kind == "ingest":
        _on_ingest(refs, e)
    elif kind == "rec_status":
        _on_rec_status(refs, e)
    elif kind == "cmd_resp":
        _on_cmd_resp(refs, e)
    elif kind == "storage":
        _on_storage(refs, e)
    elif kind == "file_list":
        _on_file_list(refs, e)
