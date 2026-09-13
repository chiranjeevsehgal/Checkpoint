"""
Host-side checks for phone-anchored recording timestamps.
Covers: command ID, monotonic tick source, RAM manifest timing, FILE_DONE
trailer, and the control-packet threading contract.
Run: pytest firmware/tests/test_time_sync_logic.py -v
"""
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"


def test_time_cmd_id():
    h = (BASE / "control.h").read_text()
    assert "CTRL_CMD_TIME_SET = 0x14" in h
    print("PASS time cmd id")


def test_clock_module():
    clk_h = (BASE / "clock.h").read_text()
    assert "clock_ticks_us" in clk_h
    assert "clock_set_anchor" in clk_h
    assert "clock_resolve" in clk_h
    clk = (BASE / "clock.cpp").read_text()
    assert "esp_timer_get_time" in clk
    assert "millis(" not in clk, "clock must use esp_timer, not millis"
    assert "recorder_boot_id()" in clk
    print("PASS clock module")


def test_control_time_handler():
    cpp = (BASE / "control.cpp").read_text()
    assert '#include "clock.h"' in cpp
    assert "CTRL_CMD_TIME_SET" in cpp
    assert "clock_set_anchor(time_unix_s, clock_ticks_us())" in cpp
    on_pkt = cpp.split("bool control_on_packet")[1].split("void control_poll")[0]
    assert "s_time_req = true" in on_pkt
    assert "putUChar" not in on_pkt and "sd_lock" not in on_pkt
    print("PASS control time handler")


def test_recorder_records_start_tick():
    rec = (BASE / "recorder.cpp").read_text()
    assert "s_chunk_start_us = (uint64_t)esp_timer_get_time()" in rec
    assert "manifest_set_time(s_final_path, s_boot_id, s_chunk_start_us)" in rec
    assert "uint32_t recorder_boot_id()" in rec
    h = (BASE / "manifest.h").read_text()
    assert "time_boot_id" in h and "start_ticks_us" in h
    print("PASS recorder start tick")


def test_file_done_trailer():
    tr = (BASE / "transfer.cpp").read_text()
    assert '#include "clock.h"' in tr
    assert "uint8_t done_payload[40]" in tr
    assert "clock_resolve(job->time_boot_id, job->start_ticks_us" in tr
    assert "sizeof(done_payload)" in tr
    print("PASS file done trailer")


if __name__ == "__main__":
    test_time_cmd_id()
    test_clock_module()
    test_control_time_handler()
    test_recorder_records_start_tick()
    test_file_done_trailer()
    print("ALL PASS")
