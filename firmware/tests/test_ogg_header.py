"""Regression: OpusHead must declare 16k input + 312 preskip, not 48k/6000.

Bug: 50s recording played as ~15s at 3x speed in naive players honouring
OpusHead.input_sample_rate (48k header vs 16k SILK payload).
"""
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"


def test_ogg_head_sample_rate_16k():
    h = (BASE / "ogg_mux.h").read_text()
    # 16000 LE = 0x80 0x3E 0x00 0x00 ; 48000 LE = 0x80 0xBB must be gone
    assert "0x80; out[13]=0x3E" in h or "0x80, 0x3E" in h or "out[13]=0x3E" in h, \
        "OpusHead input rate must be 16000 LE (0x3E80)"
    code = h.split("ogg_opus_head")[1].split("}")[0]
    code = "\n".join(l.split("//")[0] for l in code.splitlines())  # strip comments
    assert "0xBB" not in code, "48000 (0xBB80) must not remain in ogg_opus_head"


def test_ogg_head_preskip_312():
    h = (BASE / "ogg_mux.h").read_text()
    seg = h.split("ogg_opus_head")[1].split("}")[0]
    assert "0x38" in seg and "0x01" in seg, "pre-skip must be 312 LE (0x38 0x01)"
    assert "0x70" not in seg and "0x17" not in seg, "6000 pre-skip must be gone"


def test_granule_still_48k_timeline():
    rec = (BASE / "recorder.cpp").read_text()
    # Opus granule timeline is always 48k: +960 per 20ms frame, unchanged
    assert "s_opus_granule += 960" in rec or "s_opus_granule + 960" in rec


def test_bos_split_head_and_tags():
    h = (BASE / "ogg_mux.h").read_text()
    seg = h.split("ogg_write_bos")[1].split("ogg_write_audio_frame")[0]
    # Two single-packet pages: seq 0 BOS then seq 1 normal
    assert seg.count("ogg_build_page") == 2, "BOS must build two pages"
    assert ", 0, 0x02," in seg, "page 0 must be BOS seq 0"
    assert ", 1, 0x00," in seg, "page 1 must be normal seq 1"
    assert "lens, 2," not in seg, "combined two-packet BOS must be gone"


def test_audio_seq_starts_at_two():
    rec = (BASE / "recorder.cpp").read_text()
    assert "s_opus_seq = 2" in rec, "audio must start at seq 2 (0=head, 1=tags)"
    assert "s_opus_seq = 1;" not in rec, "old seq 1 start must be gone"
