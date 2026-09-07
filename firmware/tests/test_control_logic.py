"""
Host-side checks for BLE remote transport + LED/stealth control.
Covers: protocol enum parity, control chaining, NVS keys, UI stealth,
host client + GUI wiring.
Run: pytest firmware/tests/test_control_logic.py -v
"""
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"
HOST = Path(__file__).resolve().parent.parent / "host" / "checkpoint_client"


def test_protocol_control_enums():
    h = (BASE / "protocol.h").read_text()
    assert "PKT_CMD = 0x20" in h
    assert "PKT_CMD_RESP = 0x21" in h
    assert "PKT_STATUS_REQ = 0x22" in h
    assert "PKT_STATUS_RESP = 0x23" in h
    cpp = (BASE / "protocol.cpp").read_text()
    for name in ("CMD", "CMD_RESP", "STATUS_REQ", "STATUS_RESP"):
        assert name in cpp
    print("PASS protocol control enums")


def test_control_module():
    h = (BASE / "control.h").read_text()
    assert "control_init" in h
    assert "control_poll" in h
    assert "control_on_packet" in h
    assert "CTRL_CMD_REC_START" in h
    assert "CTRL_CMD_LED_SET" in h
    assert "CTRL_STATUS_LEN" in h
    cpp = (BASE / "control.cpp").read_text()
    assert "led_muted" in cpp and "led_bright" in cpp
    assert "Preferences" in cpp
    assert "recorder_start" in cpp and "recorder_stop" in cpp
    assert "ui_set_muted" in cpp and "ui_set_brightness" in cpp
    assert "PKT_CMD_RESP" in cpp and "PKT_STATUS_RESP" in cpp
    assert "PKT_ERROR" in cpp
    # NimBLE callback must stay fast: no delays or SD locks in on_packet path
    on_pkt = cpp.split("bool control_on_packet")[1].split("void control_poll")[0]
    assert "vTaskDelay" not in on_pkt
    assert "sd_lock" not in on_pkt
    assert "recorder_start" not in on_pkt
    # Cross-task flag handoff must be guarded (NimBLE task vs loop task)
    assert "portENTER_CRITICAL" in cpp and "portEXIT_CRITICAL" in cpp
    print("PASS control module")


def test_transfer_chains_control():
    tr = (BASE / "transfer.cpp").read_text()
    assert '#include "control.h"' in tr
    assert "control_on_packet" in tr
    assert "PKT_CMD" in tr and "PKT_STATUS_REQ" in tr
    # Single shared ble_on_packet callback must remain (no second registration)
    assert tr.count("ble_on_packet(") == 1
    print("PASS transfer chain")


def test_ui_stealth():
    h = (BASE / "ui.h").read_text()
    assert "ui_set_muted" in h and "ui_set_brightness" in h
    assert "ui_note_remote_action" in h
    ui = (BASE / "ui.cpp").read_text()
    assert "s_muted" in ui and "s_brightness" in ui
    assert "ui_stealth_active" in ui or "s_muted" in ui
    # ERROR/FATAL must stay visible when muted (safety over stealth)
    assert "LED_ERROR" in ui and "LED_FATAL" in ui
    # Button lockout covers recent remote action
    assert "s_remote_action_ms" in ui
    print("PASS ui stealth")


def test_ino_wiring():
    ino = (BASE / "checkpoint.ino").read_text()
    assert '#include "control.h"' in ino
    assert "control_init()" in ino
    assert "control_poll()" in ino
    print("PASS ino wiring")


def test_host_ingestion_client():
    cli = (HOST / "client - ingestion.py").read_text()
    for token in ("PKT_CMD = 0x20", "PKT_STATUS_REQ = 0x22",
                  "CTRL_CMD_REC_START", "CTRL_CMD_LED_SET",
                  "cmd_rec_start", "cmd_rec_stop",
                  "cmd_led_set", "req_status", "parse_status",
                  "PKT_CMD_RESP", "PKT_STATUS_RESP"):
        assert token in cli, f"missing {token}"
    print("PASS host ingestion client")


def test_host_gui():
    gui = (HOST / "gui.py").read_text()
    for token in ("Rec Start", "Rec Stop", "Apply LED", "LED muted",
                  "on_rec_start", "on_rec_stop", "on_led_apply",
                  "on_status_refresh", "rec_status", "cmd_resp"):
        assert token in gui, f"missing {token}"
    print("PASS host gui")


def test_simple_client_parity():
    cli = (HOST / "client.py").read_text()
    assert "PKT_CMD = 0x20" in cli
    assert "PKT_STATUS_RESP" in cli
    print("PASS simple client parity")


if __name__ == "__main__":
    test_protocol_control_enums()
    test_control_module()
    test_transfer_chains_control()
    test_ui_stealth()
    test_ino_wiring()
    test_host_ingestion_client()
    test_host_gui()
    test_simple_client_parity()
    print("ALL PASS")
