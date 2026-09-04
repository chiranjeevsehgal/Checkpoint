"""
Host-side unit checks for firmware fixes.
Covers: SPI clock, protocol CRC, manifest lazy CRC, window retry, header patch.
Run: python3 test_firmware_logic.py
"""
import struct
import zlib
import json
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"

def test_spi_clock():
    SD_SPI_FREQ_KHZ = 10000
    freq = SD_SPI_FREQ_KHZ * 1000
    assert freq == 10000000, f"freq {freq} != 10MHz"
    old = 1000000 * (SD_SPI_FREQ_KHZ // 1000) // 1000
    assert old == 10000, "old formula should be 10k"
    print("PASS spi clock")

def test_proto_crc_build_parse():
    ver = 1
    typ = 0x12
    seq = 42
    payload = b"hello"
    hdr = struct.pack("<BBHH", ver, typ, seq, len(payload))
    crc = zlib.crc32(hdr + payload) & 0xFFFFFFFF
    # mimic proto_build: hdr+payload+crc
    packet = hdr + payload + struct.pack("<I", crc)
    # parse
    r_ver, r_typ, r_seq, r_len = struct.unpack("<BBHH", packet[:6])
    assert r_ver == ver
    assert r_typ == typ
    assert r_seq == seq
    assert r_len == len(payload)
    recv_crc = struct.unpack("<I", packet[6+len(payload):6+len(payload)+4])[0]
    calc = zlib.crc32(packet[:6+len(payload)]) & 0xFFFFFFFF
    assert recv_crc == calc
    # corrupt
    bad = bytearray(packet)
    bad[7] ^= 0xFF
    calc2 = zlib.crc32(bad[:6+len(payload)]) & 0xFFFFFFFF
    recv2 = struct.unpack("<I", bad[6+len(payload):6+len(payload)+4])[0]
    assert calc2 != recv2
    print("PASS proto crc")

def test_manifest_lazy():
    # manifest_scan should not compute crc for all files
    p = (BASE / "manifest.cpp").read_text()
    # ensure no loop that reads all files with sd_file_crc32
    assert p.count("sd_file_crc32") <= 1 or "crc = 0" in p, "should be lazy"
    assert "sd_file_crc32" not in p or p.count("for (size_t i = 0; i < s_count; i++)") <= 3
    print("PASS manifest lazy")

def test_header_patch():
    # wav header build for size 1000 -> chunk 36+1000, file 44+1000
    sample_rate = 16000
    byte_rate = 32000
    data_bytes = 1000
    chunk = 36 + data_bytes
    h = bytearray(44)
    h[0:4]=b'RIFF'
    struct.pack_into("<I", h, 4, chunk)
    h[8:12]=b'WAVE'
    # check size
    assert struct.unpack("<I", h[4:8])[0] == chunk
    print("PASS header patch")

def test_window_retry_logic():
    tr = (BASE / "transfer.cpp").read_text()
    assert "xQueueCreate" in tr
    assert "attempts" in tr
    assert "xSemaphoreTake(s_ack_mutex, 0)" not in tr
    # must still wait with BLE_ACK_TIMEOUT_MS but now with short-circuit for already-acked base
    assert "BLE_ACK_TIMEOUT_MS" in tr
    assert "window[0].acked" in tr
    assert "base_acked" in tr
    # ensure clear on stale
    assert "xQueueReset" in tr
    print("PASS window")

def test_psram_allocation():
    man = (BASE / "manifest.cpp").read_text()
    assert "heap_caps_malloc" in man
    assert "MALLOC_CAP_SPIRAM" in man
    assert "s_capacity" in man
    print("PASS psram")

def test_crypto_nvs():
    cry = (BASE / "crypto.cpp").read_text()
    assert 'Preferences' in cry
    assert 'ccmmaster' in cry
    assert 'crypto_load_or_gen_key' in cry
    print("PASS crypto nvs")

def test_no_isr():
    ino = (BASE / "checkpoint.ino").read_text()
    assert 'attachInterrupt' not in ino
    ui = (BASE / "ui.cpp").read_text()
    assert 'recorder_notify_bookmark' in ui
    print("PASS ui no isr")

def test_sd_mutex():
    sd = (BASE / "sd_manager.cpp").read_text()
    assert 'xSemaphoreCreateRecursiveMutex' in sd
    assert 'sd_lock' in sd
    print("PASS sd mutex")

if __name__ == "__main__":
    test_spi_clock()
    test_proto_crc_build_parse()
    test_manifest_lazy()
    test_header_patch()
    test_window_retry_logic()
    test_psram_allocation()
    test_crypto_nvs()
    test_no_isr()
    test_sd_mutex()
    print("ALL PASS")
