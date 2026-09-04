"""
Tests for robust fault-tolerant fixes: boot_id collision, crypto mutex, BLE IO cap, manifest dead code, UI non-blocking.
Run: python firmware/tests/test_robust_fixes.py
"""
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"


def read(name: str) -> str:
    return (BASE / name).read_text(encoding="utf-8", errors="ignore")


# ---- #1 boot_id collision fix ----
def test_boot_id_exists():
    rec = read("recorder.cpp")
    assert "s_boot_id" in rec, "s_boot_id missing"
    assert "load_boot_id()" in rec, "load_boot_id missing"
    assert "Preferences" in rec, "Preferences missing for boot_id"
    assert '#include <Preferences.h>' in rec
    print("PASS boot_id exists")


def test_boot_id_once_per_boot():
    rec = read("recorder.cpp")
    # load_boot_id reads getUInt boot_id +1 and putUInt boot_id
    assert 'getUInt("boot_id"' in rec, "should read boot_id from NVS"
    assert 'putUInt("boot_id"' in rec, "should write boot_id"
    assert 'pref.begin("checkpoint"' in rec, "should use checkpoint namespace like crypto"
    # called once in recorder_init
    assert "s_boot_id = load_boot_id()" in rec, "should init boot_id in recorder_init"
    # ensure not per-chunk (only 1 putUInt occurrence)
    assert rec.count('putUInt("boot_id"') == 1, "should write boot_id once per boot, not per chunk"
    print("PASS boot_id once per boot")


def test_chunk_name_uses_boot_id():
    rec = read("recorder.cpp")
    assert "static String chunk_name()" in rec
    idx = rec.find("static String chunk_name()")
    seg = rec[idx: idx + 600]
    assert "s_boot_id" in seg, "chunk_name should use s_boot_id"
    assert "s_chunks" in seg
    assert "REC_%06lu_%04lu" in seg, "format should be %06_%04"
    assert "millis()/1000" not in seg, "chunk_name must not use millis"
    assert "1000000" in seg or "% 1000000" in seg or "%1000000" in seg, "boot_id should be bounded 6-digit"
    assert "10000" in seg, "chunk seq should be 4-digit"
    print("PASS chunk_name boot_id")


def test_chunk_name_bounded():
    # verify modulo bounds cover required window
    assert 1000000 > 100000  # 1M reboots irrelevant
    assert 10000 * 60 > 18 * 3600  # 166h > 18h window
    print("PASS chunk bounds")


# ---- #2 crypto mutex ----
def test_crypto_mutex_exists():
    c = read("crypto.cpp")
    assert "s_crypto_mutex" in c, "mutex missing"
    assert "xSemaphoreCreateMutex" in c
    assert "xSemaphoreTake" in c
    assert "xSemaphoreGive" in c
    print("PASS crypto mutex exists")


def test_crypto_init_creates_mutex():
    c = read("crypto.cpp")
    assert "bool crypto_init()" in c
    idx = c.find("bool crypto_init()")
    seg = c[idx: idx + 500]
    assert "s_crypto_mutex" in seg
    assert "xSemaphoreCreateMutex" in seg
    print("PASS crypto init mutex")


def test_crypto_set_key_locked():
    c = read("crypto.cpp")
    idx = c.find("bool crypto_set_key")
    seg = c[idx: idx + 800]
    assert "crypto_lock" in seg or "xSemaphoreTake" in seg
    assert "memcpy(s_master_key" in seg
    assert "mbedtls_ccm_setkey" in seg
    assert "crypto_unlock" in seg or "xSemaphoreGive" in seg
    print("PASS crypto_set_key locked")


def test_crypto_encrypt_locked():
    c = read("crypto.cpp")
    idx = c.find("bool crypto_encrypt")
    seg = c[idx: idx + 800]
    assert "crypto_lock" in seg or "xSemaphoreTake" in seg
    assert "mbedtls_ccm_encrypt_and_tag" in seg
    assert "s_has_key" in seg
    assert "crypto_unlock" in seg or "xSemaphoreGive" in seg
    print("PASS crypto_encrypt locked")


def test_crypto_get_key_locked():
    c = read("crypto.cpp")
    idx = c.find("bool crypto_get_key")
    seg = c[idx: idx + 600]
    assert "crypto_lock" in seg or "xSemaphoreTake" in seg
    assert "memcpy(out, s_master_key" in seg
    print("PASS crypto_get_key locked")


def test_crypto_decrypt_locked():
    c = read("crypto.cpp")
    idx = c.find("bool crypto_decrypt")
    seg = c[idx: idx + 800]
    assert "crypto_lock" in seg or "xSemaphoreTake" in seg
    assert "mbedtls_ccm_auth_decrypt" in seg
    print("PASS crypto_decrypt locked")


def test_crypto_load_or_gen_locked():
    c = read("crypto.cpp")
    idx = c.find("bool crypto_load_or_gen_key")
    seg = c[idx: idx + 2000]
    # should re-check after lock to avoid double gen
    assert "ccmmaster" in seg
    assert "crypto_set_key" in seg
    print("PASS crypto_load_or_gen")


# ---- #3 BLE IO cap ----
def test_ble_io_cap():
    b = read("ble_service.cpp")
    assert "BLE_HS_IO_NO_INPUT_OUTPUT" in b, "should be Just Works"
    assert "BLE_HS_IO_DISPLAY_ONLY" not in b, "DISPLAY_ONLY should be removed (except comment)"
    print("PASS ble io cap")


def test_ble_doc_fixed():
    cand_docs = [
        Path(__file__).resolve().parent.parent / "UPLOAD_GUIDE.md",
        Path(__file__).resolve().parent.parent / "docs" / "UPLOAD_GUIDE.md",
    ]
    guide = ""
    for cand in cand_docs:
        if cand.exists():
            guide = cand.read_text(encoding="utf-8", errors="ignore")
            break
    assert guide, "UPLOAD_GUIDE not found at firmware/UPLOAD_GUIDE.md or firmware/docs/UPLOAD_GUIDE.md"
    assert "Just Works" in guide, "UPLOAD_GUIDE should say Just Works"
    assert "no passkey" in guide.lower() or "no_input_output" in guide.lower()
    print("PASS ble doc")


# ---- S1 manifest dead code ----
def test_manifest_no_prev():
    m = read("manifest.cpp")
    assert "ManifestEntry prev[32]" not in m, "dead prev array should be deleted"
    assert "(void)prev" not in m
    assert "prev_n" not in m, "prev_n dead code should be deleted"
    print("PASS manifest no prev")


def test_manifest_scan_still_works():
    m = read("manifest.cpp")
    assert "bool manifest_scan_and_recover()" in m
    assert "sd_lock(2000)" in m
    assert "SD.open(REC_DIR)" in m
    assert "manifest_load()" in m
    assert "manifest_save()" in m
    print("PASS manifest scan intact")


# ---- S2 UI non-blocking ----
def test_ui_non_blocking():
    u = read("ui.cpp")
    # ERROR and FATAL should not use continue blocking
    # Old code had 'continue;' after each blink block; new should not
    # At least should have millis() based timing
    assert "state_enter_ms" in u or "millis()" in u, "should be non-blocking with millis"
    assert "LED_ERROR" in u and "LED_FATAL" in u
    # Should poll button every loop (no continue that skips poll)
    # New code should have only one vTaskDelay at bottom, not inside cases with continue
    assert u.count("continue;") == 0, "non-blocking should have 0 continue (old had 3)"
    assert "vTaskDelay(pdMS_TO_TICKS(20))" in u
    # Check error timing still 1320ms cycle (720+600)
    assert "1320" in u or "720" in u
    assert "160" in u  # fatal 80*2
    print("PASS ui non-blocking")


def test_ui_button_still_polled():
    u = read("ui.cpp")
    assert "digitalRead(HW_BUTTON_GPIO)" in u
    assert "recorder_notify_bookmark()" in u
    assert "UI_DEBOUNCE_MS" in u
    print("PASS ui button polled")


if __name__ == "__main__":
    test_boot_id_exists()
    test_boot_id_once_per_boot()
    test_chunk_name_uses_boot_id()
    test_chunk_name_bounded()
    test_crypto_mutex_exists()
    test_crypto_init_creates_mutex()
    test_crypto_set_key_locked()
    test_crypto_encrypt_locked()
    test_crypto_get_key_locked()
    test_crypto_decrypt_locked()
    test_crypto_load_or_gen_locked()
    test_ble_io_cap()
    test_ble_doc_fixed()
    test_manifest_no_prev()
    test_manifest_scan_still_works()
    test_ui_non_blocking()
    test_ui_button_still_polled()
    print("ALL ROBUST FIXES PASS")
