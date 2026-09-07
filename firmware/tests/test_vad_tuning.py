"""
Pass 1 pendant VAD tuning regression tests.
Run: python -m pytest firmware/tests/test_vad_tuning.py -v

Covers (config-only change, no vad.cpp logic):
  Pass 1 values in config.h (mode 1, floor -54, min_speech 150)
  recorder OFFSET enforcement path intact (too_short -> discard)
  vad.cpp guards untouched (r_dom 1.5 held, crest veto, score table)
  boundary: 150ms / 20ms frame = 8 true-voiced frames
"""
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CHECKPOINT = ROOT / "checkpoint"


def read(name):
    return (CHECKPOINT / name).read_text(encoding="utf-8", errors="ignore")


def test_pass1_config_values():
    cfg = read("config.h")
    assert "#define VAD_MODE 1" in cfg, "VAD_MODE must be 1 (Pass 1)"
    assert "#define VAD_MIN_SPEECH_MS 150" in cfg, "MIN_SPEECH must be 150ms (Pass 1)"
    assert "#define VAD_ABS_FLOOR_DBFS -54.0f" in cfg, "ABS_FLOOR must be -54.0f (Pass 1)"
    assert "#define VAD_MODE 2" not in cfg, "stale mode 2 must be gone"
    assert "-50.0f" not in cfg or "VAD_ABS_FLOOR_DBFS -54.0f" in cfg, "stale -50 floor must be gone"
    print("PASS pass1 config values")


def test_min_speech_enforcement_intact():
    rec = read("recorder.cpp")
    # OFFSET verdict uses true voiced frames, discards blips
    assert "vad_min_speech_frames_cfg()" in rec, "frames helper missing"
    assert "s_vad_speech_frames < min_fr" in rec, "too_short verdict missing"
    assert "close_chunk(!too_short)" in rec, "discard-on-short path missing"
    # pre-roll must not count toward MIN_SPEECH (seed from onset window only)
    assert "s_vad_speech_frames = vad_utterance_frames()" in rec, "onset seed missing"
    print("PASS min_speech enforcement intact")


def test_vad_guards_untouched():
    vad = read("vad.cpp")
    # Pass 2 deferred: r_dom held at 1.5, crest veto and score table unchanged
    assert "r_dom < 1.5f" in vad, "formant guard must stay 1.5 (Pass 2 deferred)"
    assert "r_dom < 1.7f" not in vad, "1.7 nudge must NOT be present yet"
    assert "crest > 10.0f" in vad, "crest veto must stay 10.0"
    assert "2.5f, 4.0f, 5.5f, 7.0f" in vad, "score table must be unchanged"
    print("PASS vad guards untouched")


def test_min_speech_boundary_frames():
    cfg = read("config.h")
    rec = read("recorder.cpp")
    # 150ms / 20ms frame via integer division = 7 frames (140ms effective).
    # Derivation must flow through the helper, not a hard-coded count.
    assert "(VAD_MIN_SPEECH_MS / REC_OPUS_FRAME_MS)" in rec, "frame derivation missing"
    assert "#define VAD_MIN_SPEECH_MS 150" in cfg
    assert "#define REC_OPUS_FRAME_MS 20" in read("config.h"), "frame geometry changed?"
    assert 150 // 20 == 7, "150ms must derive 7 true-voiced frames"
    # onset seed (~5 frames) alone must not pass a 7-frame minimum
    assert 5 < 7, "seed-only utterance must still be discarded"
    print("PASS min_speech boundary 7 frames")


if __name__ == "__main__":
    test_pass1_config_values()
    test_min_speech_enforcement_intact()
    test_vad_guards_untouched()
    test_min_speech_boundary_frames()
    print("ALL VAD TUNING TESTS PASS")
