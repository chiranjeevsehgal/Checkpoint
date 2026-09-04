"""
Tests for 8-pin SD variant (no DET) — HW_HAS_SD_DETECT 0.

Run: python3 test_no_det_variant.py
      python3 -m pytest test_no_det_variant.py -v
      python3 -m pytest firmware/tests -v
"""
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"


def read(name: str) -> str:
    return (BASE / name).read_text(encoding="utf-8", errors="ignore")


def test_config_has_flag():
    cfg = read("config.h")
    assert "HW_HAS_SD_DETECT" in cfg, "flag missing in config.h"
    # default 0 for 8-pin build
    assert "#define HW_HAS_SD_DETECT 0" in cfg, "default should be 0 for this build"
    # guarded GPIO definition
    assert "#if HW_HAS_SD_DETECT" in cfg
    assert "#define HW_SD_DETECT_GPIO 7" in cfg
    assert "#define HW_SD_DETECT_GPIO -1" in cfg, "NC sentinel missing"
    print("PASS config flag")


def test_config_hw_has_det_ifndef_guard():
    cfg = read("config.h")
    # Allow override via build flag: should have #ifndef
    assert "#ifndef HW_HAS_SD_DETECT" in cfg, "missing ifndef guard for build-flag override"
    print("PASS config ifndef guard")


def test_sd_manager_det_guarded():
    sd = read("sd_manager.cpp")
    # sd_present must be guarded, not unconditional digitalRead
    assert "bool sd_present()" in sd
    p_idx = sd.find("bool sd_present()")
    p_seg = sd[p_idx : p_idx + 600]
    assert "#if HW_HAS_SD_DETECT" in p_seg, "sd_present not guarded"
    assert "digitalRead(HW_SD_DETECT_GPIO)" in p_seg
    assert "return true;" in p_seg, "DET-less fallback should return true"
    # sd_begin must not unconditionally pinMode/DET gate
    b_idx = sd.find("bool sd_begin()")
    b_seg = sd[b_idx : b_idx + 900]
    assert "#if HW_HAS_SD_DETECT" in b_seg, "sd_begin not guarded"
    assert "pinMode(HW_SD_DETECT_GPIO, INPUT)" in b_seg
    assert "if (!sd_present()) return false;" in b_seg
    # Outside #if, sd_spi.begin and SD.begin must exist unconditionally
    assert "sd_spi.begin(HW_SD_CLK_GPIO" in b_seg
    assert "SD.begin(HW_SD_CS_GPIO" in b_seg
    print("PASS sd_manager guarded")


def test_checkpoint_mount_health_probe():
    ino = read("checkpoint.ino")
    # Must have both DET and DET-less branches
    assert "#if HW_HAS_SD_DETECT" in ino
    assert "#else" in ino
    # DET branch original logic
    assert "!sd_present() && sd_mounted()" in ino
    assert "sd_present() && !sd_mounted()" in ino
    # DET-less branch health probe
    assert "if (!sd_mounted())" in ino
    assert "SD.exists(REC_DIR)" in ino, "health probe missing SD.exists"
    assert "sd_lock(200)" in ino, "health probe should use short 200ms lock"
    assert "sd_end()" in ino
    assert "ui_signal_error()" in ino
    assert "manifest_scan_and_recover()" in ino
    print("PASS checkpoint probe")


def test_recorder_guarded():
    rec = read("recorder.cpp")
    # open_chunk guard must be conditional
    assert "static bool open_chunk()" in rec
    o_idx = rec.find("static bool open_chunk()")
    o_seg = rec[o_idx : o_idx + 600]
    assert "#if HW_HAS_SD_DETECT" in o_seg
    assert "if (!sd_mounted() || !sd_present())" in o_seg, "DET branch guard missing"
    assert "if (!sd_mounted()) return false;" in o_seg, "DET-less branch guard missing"
    # task card-loss guard
    t_marker = "while (s_recording) {"
    t_idx = rec.find(t_marker)
    t_seg = rec[t_idx : t_idx + 800]
    assert "#if HW_HAS_SD_DETECT" in t_seg
    assert "if (!sd_present() || !sd_mounted())" in t_seg
    assert "if (!sd_mounted())" in t_seg
    # Ensure no stray unconditional sd_present call remains outside guard in those functions
    # (sd_present should only appear inside #if blocks in recorder.cpp)
    # Count appearances outside #if by checking the guarded segments cover them
    total_present = rec.count("sd_present()")
    # 2 guarded spots => 2 occurrences when counting both branches (DET + DET-less dedup -> 2)
    assert total_present == 2, f"expected 2 sd_present() in recorder.cpp (both guarded), got {total_present}"
    print("PASS recorder guarded")


def test_docs_variant_notes():
    # CONNECTIONS under hardware/ (current) or HW/ (legacy)
    conn_cands = [
        Path(__file__).resolve().parent.parent.parent / "hardware" / "CONNECTIONS.md",
        Path(__file__).resolve().parent.parent.parent / "HW" / "CONNECTIONS.md",
    ]
    con = ""
    for cand in conn_cands:
        if cand.exists():
            con = cand.read_text(encoding="utf-8", errors="ignore")
            break
    assert "8-pin Variant" in con, "CONNECTIONS.md missing 8-pin variant section"
    assert "HW_HAS_SD_DETECT 0" in con
    assert "GPIO7" in con and "Not connected" in con
    assert "DAT2" in con and "DAT1" in con
    guide_cands = [
        Path(__file__).resolve().parent.parent / "UPLOAD_GUIDE.md",
        Path(__file__).resolve().parent.parent / "docs" / "UPLOAD_GUIDE.md",
    ]
    guide = ""
    for cand in guide_cands:
        if cand.exists():
            guide = cand.read_text(encoding="utf-8", errors="ignore")
            break
    assert "GPIO7 DET" in guide
    assert "8-pin" in guide or "HW_HAS_SD_DETECT" in guide
    print("PASS docs variant")


def test_no_det_api_preserved():
    hdr = read("sd_manager.h")
    assert "bool sd_present();" in hdr, "sd_present API must stay for compat"
    assert "bool sd_mounted();" in hdr
    assert "bool sd_begin();" in hdr
    print("PASS api preserved")


def test_no_floating_gpio7_when_no_det():
    sd = read("sd_manager.cpp")
    cfg = read("config.h")
    # When HW_HAS_SD_DETECT 0, there must be no unconditional pinMode(HW_SD_DETECT_GPIO, INPUT)
    # i.e., all pinMode(HW_SD_DETECT_GPIO should be inside #if
    unconditional = sd.count("pinMode(HW_SD_DETECT_GPIO, INPUT)")
    guarded = sd.count("#if HW_HAS_SD_DETECT")
    assert unconditional == 1, "should have exactly one pinMode inside guard"
    assert guarded >= 2, "expected at least 2 #if guards in sd_manager.cpp"
    # config must set -1 sentinel so stray digitalRead(-1) is not compiled
    assert "HW_SD_DETECT_GPIO -1" in cfg
    print("PASS no floating gpio7")


if __name__ == "__main__":
    test_config_has_flag()
    test_config_hw_has_det_ifndef_guard()
    test_sd_manager_det_guarded()
    test_checkpoint_mount_health_probe()
    test_recorder_guarded()
    test_docs_variant_notes()
    test_no_det_api_preserved()
    test_no_floating_gpio7_when_no_det()
    print("ALL NO-DET VARIANT PASS")
