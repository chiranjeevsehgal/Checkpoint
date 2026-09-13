"""
Host-side checks for deep-sleep power management.
Covers: POWER config, wake-hold validation, sleep gating, LED indication,
USB test hook and checkpoint wiring.
Run: pytest firmware/tests/test_power_sleep.py -v
"""
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"


def read(name: str) -> str:
    return (BASE / name).read_text(encoding="utf-8", errors="ignore")


def test_config_power_constants():
    cfg = read("config.h")
    for key in ("POWER_AUTO_SLEEP_ENABLE", "POWER_IDLE_SLEEP_MS", "POWER_WAKE_HOLD_MS",
                "POWER_WAKE_RELEASE_CAP_MS", "POWER_WAKE_LED_FLASHES",
                "POWER_SLEEP_LED_FLASHES", "POWER_LED_B"):
        assert key in cfg, f"missing {key}"
    # GPIO1 button remains the single wake source
    assert "#define HW_BUTTON_GPIO 1" in cfg
    print("PASS power config")


def test_power_module_api():
    hdr = read("power.h")
    for fn in ("power_init", "power_poll", "power_sleep_now"):
        assert fn in hdr, f"missing {fn} in power.h"
    print("PASS power api")


def test_power_uses_deep_sleep():
    cpp = read("power.cpp")
    # ESP32-S3 has no deep-sleep GPIO API; EXT1 on an RTC pin is the wake path.
    assert "esp_sleep_enable_ext1_wakeup_io" in cpp
    assert "ESP_EXT1_WAKEUP_ANY_LOW" in cpp
    assert "rtc_gpio_pullup_en" in cpp
    assert "esp_sleep_get_wakeup_cause" in cpp
    assert "esp_deep_sleep_start" in cpp
    print("PASS deep sleep calls")


def test_power_wake_hold_validation():
    cpp = read("power.cpp")
    assert "POWER_WAKE_HOLD_MS" in cpp
    assert "POWER_WAKE_RELEASE_CAP_MS" in cpp
    assert "digitalRead(HW_BUTTON_GPIO)" in cpp
    assert "pinMode(HW_BUTTON_GPIO, INPUT_PULLUP)" in cpp
    # short release must re-enter sleep
    idx = cpp.find("POWER_WAKE_HOLD_MS")
    seg = cpp[idx: idx + 400]
    assert "power_sleep_now" in seg, "early release must sleep again"
    print("PASS wake hold")


def test_power_sleep_gating():
    cpp = read("power.cpp")
    assert "recorder_is_recording" in cpp
    assert "transfer_is_busy" in cpp
    assert "ble_is_connected" in cpp
    assert "ble_enrollment_active" in cpp
    assert "control_sync_enabled" in cpp
    assert "manifest_pending_count" in cpp
    # two clean reads guard the mutex-timeout false zero
    assert "s_synced_confirm" in cpp
    print("PASS sleep gating")


def test_power_clean_shutdown():
    cpp = read("power.cpp")
    assert "recorder_stop" in cpp
    assert "ble_disconnect" in cpp
    assert "sd_end" in cpp
    print("PASS clean shutdown")


def test_ui_power_indication():
    hdr = read("ui.h")
    assert "ui_flash_blue" in hdr
    assert "ui_signal_sleeping" in hdr
    ui = read("ui.cpp")
    assert "OVERLAY_SLEEP" in ui
    assert "POWER_SLEEP_LED_FLASHES" in ui
    assert "POWER_LED_FLASH_ON_MS" in ui
    assert "ui_blue_on" in ui  # shared blink timing, no duplication
    print("PASS ui indication")


def test_ino_wiring_power():
    ino = read("checkpoint.ino")
    assert '#include "power.h"' in ino
    assert "power_init()" in ino
    assert "power_poll()" in ino
    assert "ESP_RST_DEEPSLEEP" in ino
    print("PASS ino wiring")


def test_usb_power_sleep_hook():
    auth = read("auth.cpp")
    assert '"power sleep"' in auth
    assert "power_sleep_now" in auth
    print("PASS usb hook")


if __name__ == "__main__":
    test_config_power_constants()
    test_power_module_api()
    test_power_uses_deep_sleep()
    test_power_wake_hold_validation()
    test_power_sleep_gating()
    test_power_clean_shutdown()
    test_ui_power_indication()
    test_ino_wiring_power()
    test_usb_power_sleep_hook()
    print("ALL POWER SLEEP PASS")
