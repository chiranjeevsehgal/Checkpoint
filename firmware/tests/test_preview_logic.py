"""
Host-side checks for the Storage preview fetch command (CTRL_CMD_FILE_FETCH).
Covers: command registration, transfer fetch API, and the preview invariants
that keep the SD file + manifest untouched.
Run: pytest firmware/tests/test_preview_logic.py -v
"""
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"
HOST = Path(__file__).resolve().parent.parent / "host" / "checkpoint_client"


def read(name: str) -> str:
    return (BASE / name).read_text(encoding="utf-8", errors="ignore")


def test_control_command_registered():
    h = read("control.h")
    assert "CTRL_CMD_FILE_FETCH = 0x22" in h
    cpp = read("control.cpp")
    assert "control_do_file_fetch" in cpp
    assert "transfer_request_fetch" in cpp
    assert "CTRL_CMD_FILE_FETCH" in cpp
    # NimBLE callback stays fast: no SD/blocking work in the latch section.
    on_pkt = cpp.split("bool control_on_packet")[1].split("void control_poll")[0]
    assert "sd_lock" not in on_pkt
    assert "vTaskDelay" not in on_pkt


def test_transfer_fetch_api():
    h = read("transfer.h")
    assert "transfer_request_fetch" in h
    tr = read("transfer.cpp")
    assert "take_fetch" in tr
    assert "s_fetch_path" in tr
    # Preview announces append a flag + path; normal announces stay 21 bytes.
    assert "ann[21] = 0x01" in tr
    assert "ann, 21" in tr


def test_preview_keeps_file_and_manifest():
    tr = read("transfer.cpp")
    assert "if (gone && !preview) manifest_mark_done" in tr
    assert "if (!preview) manifest_update_seq" in tr
    assert "if (!preview) {" in tr  # delete/mark_done guard on the success tail
    assert "if (preview) {" in tr  # failure paths drop the preview instead of retrying


def test_host_parity():
    cfg = (HOST / "client_app" / "config.py").read_text(encoding="utf-8")
    cli = (HOST / "client_app" / "ble_client.py").read_text(encoding="utf-8")
    assert "CTRL_CMD_FILE_FETCH = 0x22" in cfg
    assert "async def cmd_file_fetch" in cli


if __name__ == "__main__":
    test_control_command_registered()
    test_transfer_fetch_api()
    test_preview_keeps_file_and_manifest()
    test_host_parity()
    print("ALL PASS")
