"""Silero VAD gate + OGG repair. Fail-open: any error means upload anyway."""

import struct

from . import config as cfg


def ogg_crc(data: bytes) -> int:
    crc = 0
    for byte in data:
        crc ^= byte << 24
        for _ in range(8):
            if crc & 0x80000000:
                crc = ((crc << 1) & 0xFFFFFFFF) ^ 0x04C11DB7
            else:
                crc = (crc << 1) & 0xFFFFFFFF
    return crc


def make_ogg_page(header_type, granule_position, serial, sequence,
                  segments, payload) -> bytes:
    page = bytearray()
    page += b"OggS"
    page += b"\x00"
    page += bytes([header_type])
    page += struct.pack("<Q", granule_position)
    page += struct.pack("<I", serial)
    page += struct.pack("<I", sequence)
    page += b"\x00\x00\x00\x00"
    page += bytes([len(segments)])
    page += bytes(segments)
    page += payload
    page[22:26] = struct.pack("<I", ogg_crc(page))
    return bytes(page)


def parse_ogg_pages(data: bytes) -> list:
    pages = []
    pos = 0
    while pos < len(data):
        if data[pos:pos + 4] != b"OggS":
            raise ValueError(f"Invalid OGG page at byte {pos}")
        n_segments = data[pos + 26]
        table_end = pos + 27 + n_segments
        segments = data[pos + 27:table_end]
        pages.append(data[pos:table_end + sum(segments)])
        pos = table_end + sum(segments)
    return pages


def repair_opus_ogg(data: bytes) -> bytes:
    """Split firmware's combined OpusHead+OpusTags page 0 into two pages."""
    pages = parse_ogg_pages(data)
    if not pages:
        raise ValueError("No OGG pages found")
    first = pages[0]
    serial = struct.unpack("<I", first[14:18])[0]
    n_segments = first[26]
    segments = list(first[27:27 + n_segments])
    payload = first[27 + n_segments:]
    if payload.startswith(b"OpusHead") and len(segments) == 1:
        return data
    if len(segments) < 2:
        raise ValueError(f"Unexpected OGG layout: {segments}")
    head_size, tags_size = segments[0], segments[1]
    opus_head = payload[:head_size]
    opus_tags = payload[head_size:head_size + tags_size]
    if not opus_head.startswith(b"OpusHead"):
        raise ValueError("OpusHead not found")
    if not opus_tags.startswith(b"OpusTags"):
        raise ValueError("OpusTags not found")
    fixed = [make_ogg_page(0x02, 0, serial, 0, [head_size], opus_head),
             make_ogg_page(0x00, 0, serial, 1, [tags_size], opus_tags)]
    for original in pages[1:]:
        page = bytearray(original)
        if struct.unpack("<I", page[14:18])[0] == serial:
            old_seq = struct.unpack("<I", page[18:22])[0]
            page[18:22] = struct.pack("<I", old_seq + 1)
            page[22:26] = b"\x00\x00\x00\x00"
            page[22:26] = struct.pack("<I", ogg_crc(page))
        fixed.append(bytes(page))
    return b"".join(fixed)


def vad_prewarm() -> None:
    """Import torch stack BEFORE any BLE/WinRT activity (call before scan)."""
    try:
        import torch  # noqa: F401
        import torchaudio  # noqa: F401
        import silero_vad  # noqa: F401
    except ModuleNotFoundError as e:
        raise RuntimeError(
            f"VAD package missing (pip install silero-vad torch torchaudio): {e}")


def vad_load_model(retries: int = 2):
    """Load Silero VAD once at startup. Retries transient native failures."""
    import time as _time
    last: Exception | None = None
    for attempt in range(max(1, retries)):
        try:
            from silero_vad import load_silero_vad
            return load_silero_vad()
        except ModuleNotFoundError as e:
            raise RuntimeError(
                f"VAD package missing (pip install silero-vad torch torchaudio): {e}")
        except Exception as e:
            last = e
            if attempt + 1 < max(1, retries):
                print(f"[vad] load attempt {attempt + 1} failed ({e}) — retrying in 3s ...")
                _time.sleep(3)
    raise RuntimeError(f"VAD model load failed after retries: {last}")


def _decode_ogg_to_tensor(repaired: bytes):
    import io as _io
    errors = []
    try:
        import torchaudio
        waveform, sample_rate = torchaudio.load(_io.BytesIO(repaired))
        mono = waveform.mean(dim=0) if waveform.shape[0] > 1 else waveform.squeeze(0)
        return mono, sample_rate
    except Exception as e:
        errors.append(f"torchaudio: {e}")
    try:
        import shutil
        import subprocess
        exe = shutil.which("ffmpeg")
        if exe is None:
            raise RuntimeError("ffmpeg not on PATH")
        proc = subprocess.run(
            [exe, "-hide_banner", "-loglevel", "error",
             "-i", "pipe:0", "-ar", "16000", "-ac", "1",
             "-f", "f32le", "-acodec", "pcm_f32le", "pipe:1"],
            input=repaired, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            timeout=30)
        if proc.returncode != 0:
            raise RuntimeError(proc.stderr.decode(errors="ignore")[:300])
        if not proc.stdout or len(proc.stdout) < 4:
            raise RuntimeError("ffmpeg produced no audio")
        import torch
        import numpy as np
        pcm = np.frombuffer(proc.stdout, dtype=np.float32).copy()
        return torch.from_numpy(pcm), 16000
    except Exception as e:
        errors.append(f"ffmpeg: {e}")
    try:
        import soundfile as sf
        import torch
        import numpy as np
        wav, sr = sf.read(_io.BytesIO(repaired), dtype="float32", always_2d=True)
        return torch.from_numpy(np.mean(wav, axis=1).astype("float32")), sr
    except Exception as e:
        errors.append(f"soundfile: {e}")
    raise RuntimeError("OGG decode failed [" + " | ".join(errors) + "]")


def vad_has_speech(data: bytes, model, threshold: float = cfg.VAD_THRESHOLD,
                   min_speech_s: float = cfg.VAD_MIN_SPEECH_S) -> tuple:
    """Blocking VAD check. Returns (status, speech_s, segments). Never raises."""
    try:
        from silero_vad import get_speech_timestamps
        import torchaudio
        waveform, sample_rate = _decode_ogg_to_tensor(repair_opus_ogg(data))
        if sample_rate != 16000:
            waveform = torchaudio.functional.resample(waveform, sample_rate, 16000)
        stamps = get_speech_timestamps(
            waveform, model, sampling_rate=16000, threshold=threshold,
            min_speech_duration_ms=cfg.VAD_MIN_SPEECH_MS,
            min_silence_duration_ms=cfg.VAD_MIN_SILENCE_MS,
            speech_pad_ms=cfg.VAD_PAD_MS, return_seconds=True)
        total = sum(s["end"] - s["start"] for s in stamps)
        if total >= min_speech_s:
            return "speech", float(total), stamps
        return "no-speech", float(total), stamps
    except Exception as e:
        return "error", 0.0, [{"error": str(e)[:200]}]
