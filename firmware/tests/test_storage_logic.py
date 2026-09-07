"""
Host-side checks for BLE remote storage management.
Covers: protocol enum parity, sd_manager APIs + layering, manifest
accessor, transfer guard, control storage/list/delete/erase paths,
host client + GUI wiring.
Run: pytest firmware/tests/test_storage_logic.py -v
"""
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"
HOST = Path(__file__).resolve().parent.parent / "host" / "checkpoint_client"


def test_protocol_storage_enums():
    h = (BASE / "protocol.h").read_text()
    assert "PKT_STORAGE_REQ = 0x24" in h
    assert "PKT_STORAGE_RESP = 0x25" in h
    assert "PKT_LIST_REQ = 0x26" in h
    assert "PKT_LIST_RESP = 0x27" in h
    cpp = (BASE / "protocol.cpp").read_text()
    for name in ("STORAGE_REQ", "STORAGE_RESP", "LIST_REQ", "LIST_RESP"):
        assert name in cpp
    print("PASS protocol storage enums")


def test_sd_manager_storage_api():
    h = (BASE / "sd_manager.h").read_text()
    assert "sd_card_usage" in h
    assert "sd_delete_file" in h
    assert "sd_erase_recordings" in h
    assert "sd_file_exists" in h
    cpp = (BASE / "sd_manager.cpp").read_text()
    assert "totalBytes" in cpp  # capacity from SD_MMC (core 3.3.11)
    assert "REC_MANIFEST" in cpp  # manifest.json excluded from used/erase
    assert "sd_safe_delete_after_ack" in cpp  # original path intact
    # Layering: sd layer must not reach into recorder/transfer/manifest.
    assert "recorder.h" not in cpp
    assert "transfer.h" not in cpp
    assert "manifest_mark_done" not in cpp
    # Erase must enumerate-then-mutate (no mutation with dir handle open).
    assert "MAX_ERASE" in cpp
    print("PASS sd_manager storage api")


def test_manifest_get_all():
    h = (BASE / "manifest.h").read_text()
    assert "manifest_get_all" in h
    assert "manifest_entry_count" in h
    cpp = (BASE / "manifest.cpp").read_text()
    assert "manifest_get_all" in cpp
    print("PASS manifest get_all")


def test_transfer_guard():
    h = (BASE / "transfer.h").read_text()
    assert "transfer_is_transferring" in h
    tr = (BASE / "transfer.cpp").read_text()
    assert "transfer_is_transferring" in tr
    assert "PKT_STORAGE_REQ" in tr and "PKT_LIST_REQ" in tr
    assert tr.count("ble_on_packet(") == 1
    print("PASS transfer guard")


def test_control_storage():
    h = (BASE / "control.h").read_text()
    assert "CTRL_CMD_FILE_DELETE" in h
    assert "CTRL_CMD_STORAGE_ERASE" in h
    assert "CTRL_ERR_BUSY" in h
    assert "CTRL_ERR_NOT_FOUND" in h
    assert "CTRL_STORAGE_LEN" in h
    assert "CTRL_LIST_FLAG_ACTIVE" in h
    assert "CTRL_ERASE_ARM" in h and "CTRL_ERASE_CONFIRM" in h
    cpp = (BASE / "control.cpp").read_text()
    assert "control_build_storage" in cpp
    assert "control_pack_list" in cpp
    assert "control_do_file_delete" in cpp
    assert "control_do_erase" in cpp
    # Safety gates: erase refuses while recording/transferring, then rescans.
    assert "transfer_is_busy" in cpp
    assert "transfer_is_transferring" in cpp
    assert "recorder_current_file" in cpp
    assert "manifest_scan_and_recover" in cpp
    # NimBLE callback stays fast: no SD/FS work in the latch section.
    on_pkt = cpp.split("bool control_on_packet")[1].split("void control_poll")[0]
    assert "sd_lock" not in on_pkt
    assert "vTaskDelay" not in on_pkt
    assert "sd_delete_file" not in on_pkt
    assert "sd_erase_recordings" not in on_pkt
    print("PASS control storage")


def test_host_ingestion_storage():
    cli = (HOST / "client - ingestion.py").read_text()
    for token in ("PKT_STORAGE_REQ = 0x24", "PKT_LIST_REQ = 0x26",
                  "CTRL_CMD_FILE_DELETE", "CTRL_CMD_STORAGE_ERASE",
                  "CTRL_ERR_BUSY", "CTRL_ERR_NOT_FOUND",
                  "req_storage", "req_list",
                  "cmd_file_delete", "cmd_storage_erase",
                  "parse_storage", "parse_file_list",
                  "PKT_STORAGE_RESP", "PKT_LIST_RESP"):
        assert token in cli, f"missing {token}"
    print("PASS host ingestion storage")


def test_host_gui_storage():
    gui = (HOST / "gui.py").read_text()
    for token in ("Storage (BLE remote)", "on_storage_refresh",
                  "on_file_delete", "on_storage_erase",
                  "on_list_prev", "on_list_next",
                  "askyesno", "file_list", "\"storage\"",
                  "storage_bar", "dev_tree"):
        assert token in gui, f"missing {token}"
    print("PASS host gui storage")


def test_simple_client_storage_parity():
    cli = (HOST / "client.py").read_text()
    assert "PKT_STORAGE_REQ = 0x24" in cli
    assert "PKT_LIST_RESP" in cli
    assert "CTRL_CMD_FILE_DELETE" in cli
    print("PASS simple client storage parity")


if __name__ == "__main__":
    test_protocol_storage_enums()
    test_sd_manager_storage_api()
    test_manifest_get_all()
    test_transfer_guard()
    test_control_storage()
    test_host_ingestion_storage()
    test_host_gui_storage()
    test_simple_client_storage_parity()
    print("ALL PASS")
