"""
Host-side checks for the BLE auto-sync kill-switch.
Covers: command IDs, NVS persistence, transfer gating without touching
the retry/delete path, STATUS flag parity, host client + GUI wiring.
Run: pytest firmware/tests/test_sync_logic.py -v
"""
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"
HOST = Path(__file__).resolve().parent.parent / "host" / "checkpoint_client"


def test_sync_cmd_ids():
    h = (BASE / "control.h").read_text()
    assert "CTRL_CMD_SYNC_SET = 0x12" in h
    assert "CTRL_CMD_SYNC_GET = 0x13" in h
    assert "CTRL_STATUS_LEN 17" in h
    assert "[16]=sync_enabled" in h
    cpp = (BASE / "control.cpp").read_text()
    assert "sync_enabled" in cpp
    assert '"sync_enabled"' in cpp  # NVS key
    assert "Preferences" in cpp
    print("PASS sync cmd ids + NVS")


def test_sync_gate_no_retry_accounting():
    # Finish-current-file semantics: the gate lives ONLY at the outer loop
    # head. The in-flight window loop must NOT consult the flag (no abort),
    # so the retry/delete path (failed=true) is unreachable via sync-off.
    tr = (BASE / "transfer.cpp").read_text()
    assert "control_sync_enabled()" in tr
    head, _, rest = tr.partition("void transfer_task")
    assert "control_sync_enabled" not in head
    # Outer idle branch gates; window loop keeps only the link check.
    assert "ble_send_raw" in tr  # sanity: file still read
    lines = tr.splitlines()
    window_checks = [l for l in lines if "ble_is_connected()) { failed = true; break; }" in l]
    assert window_checks, "window loop link check must remain"
    for l in window_checks:
        assert "sync" not in l, "in-flight loop must not abort on sync-off"
    print("PASS sync gate placement")


def test_sync_status_parity():
    cpp = (BASE / "control.cpp").read_text()
    assert "out[16] = s_sync_enabled" in cpp
    cli = (HOST / "client_app" / "config.py").read_text(encoding="utf-8")
    assert "CTRL_CMD_SYNC_SET = 0x12" in cli
    assert "CTRL_CMD_SYNC_GET = 0x13" in cli
    assert "CTRL_STATUS_LEN = 17" in cli
    ble = (HOST / "client_app" / "ble_client.py").read_text(encoding="utf-8")
    assert '"sync"' in ble or "'sync'" in ble
    print("PASS sync status parity")


def test_sync_latch_discipline():
    # Same threading contract as other commands: latch under critical
    # section, no SD/NVS/work in the NimBLE-side handler.
    cpp = (BASE / "control.cpp").read_text()
    on_pkt = cpp.split("bool control_on_packet")[1].split("void control_poll")[0]
    assert "s_sync_req = true" in on_pkt
    assert "s_sync_get_seq_valid" in on_pkt
    assert "putUChar" not in on_pkt
    assert "sd_lock" not in on_pkt
    print("PASS sync latch discipline")


def test_gui_sync_toggle():
    gui = ((HOST / "client_app" / "ui_cards.py").read_text(encoding="utf-8")
           + (HOST / "client_app" / "ui_main.py").read_text(encoding="utf-8"))
    assert "Auto-sync" in gui
    assert "on_sync_toggle" in gui
    assert "_sync_flow" in gui
    assert "sync_chk" in gui
    print("PASS gui sync toggle")


if __name__ == "__main__":
    test_sync_cmd_ids()
    test_sync_gate_no_retry_accounting()
    test_sync_status_parity()
    test_sync_latch_discipline()
    test_gui_sync_toggle()
    print("ALL PASS")
