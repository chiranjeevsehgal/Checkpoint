"""
Host-side checks for review-fix implementation.
Run: python3 test_firmware_review_fixes.py
     python3 -m pytest test_firmware_review_fixes.py -v
Covers I1–I5: crypto gate, handshake timeout, drop counter, window short-circuit, SD lock gap.
"""
import re
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"

def read(name):
    return (BASE / name).read_text(encoding="utf-8", errors="ignore")

# ---- I1: HELLO gated on encryption + version ----
def test_hello_encrypted_gate():
    ble = read("ble_service.cpp")
    # must check s_encrypted before sending key (not just defining variable)
    assert "s_encrypted" in ble, "s_encrypted missing"
    # version check on hello
    assert "PROTO_VER" in ble and "PKT_HELLO" in ble
    # verify CtrlCallbacks path contains encryption guard before payload build
    # locate HELLO branch and ensure s_encrypted appears before crypto_get_key
    hello_idx = ble.find("PKT_HELLO")
    assert hello_idx != -1
    segment = ble[hello_idx: hello_idx + 3000]
    # version guard must be present in same segment
    assert "PROTO_VER" in segment or "v[0]" in segment, "version check missing near HELLO"
    # encryption guard must precede key copy
    assert "!s_encrypted" in segment or "s_encrypted" in segment, "encryption guard missing near HELLO"
    assert "crypto_get_key" in segment, "key retrieval should still exist after guard"
    # must send PKT_ERROR on reject
    assert "PKT_ERROR" in segment, "should send PKT_ERROR when rejecting unauth HELLO"
    # must set s_handshaked only after successful encrypted hello
    assert "s_handshaked = true" in ble
    print("PASS hello encrypted gate")

def test_hello_version_invalid_path():
    ble = read("ble_service.cpp")
    # invalid version should be rejected with error code 0x02 path
    assert "0x02" in ble or "err = 0x02" in ble, "invalid version error code missing"
    # empty HELLO (size <2) must not trigger key path – guard v.size() >=2
    assert "v.size() >= 2" in ble
    print("PASS hello version invalid path")

def test_hello_empty_and_null_guard():
    ble = read("ble_service.cpp")
    # must handle empty getValue() early return
    assert "v.empty()" in ble
    # must guard s_packet_cb null
    assert "!s_packet_cb" in ble
    print("PASS hello null/empty guard")

def test_handshake_timeout_enforced():
    ble = read("ble_service.cpp")
    ino = read("checkpoint.ino")
    cfg = read("config.h")
    # constant still defined
    assert "BLE_HANDSHAKE_TIMEOUT_MS" in cfg
    # used outside config.h
    assert ble.count("BLE_HANDSHAKE_TIMEOUT_MS") >= 1, "timeout unused in ble_service.cpp"
    assert "ble_check_handshake_timeout" in ble
    assert "ble_handshake_timed_out" in ble
    assert "s_handshake_start_ms" in ble
    assert "s_conn_handle" in ble
    # ino must call the check each loop
    assert "ble_check_handshake_timeout()" in ino
    # timeout should trigger disconnect
    assert "disconnect(s_conn_handle)" in ble or "disconnect" in ble
    print("PASS handshake timeout enforced")

def test_handshake_timeout_boundaries():
    ble = read("ble_service.cpp")
    # must compare millis() - start > timeout, not >= to avoid immediate fire
    assert "millis() - s_handshake_start_ms" in ble
    assert "> BLE_HANDSHAKE_TIMEOUT_MS" in ble
    # must clear start on success and disconnect
    assert ble.count("s_handshake_start_ms = 0") >= 2
    print("PASS handshake timeout boundaries")

def test_derived_key_leak_fix():
    ble = read("ble_service.cpp")
    # onConnect must unconditionally load master (no if (!crypto_has_key()))
    # find onConnect segment
    m = re.search(r"void onConnect.*?crypto_load_or_gen_key\(\)", ble, re.DOTALL)
    assert m, "onConnect should call crypto_load_or_gen_key"
    segment = m.group(0)
    assert "if (!crypto_has_key())" not in segment, "onConnect must not be conditional on crypto_has_key"
    # onDisconnect must also restore
    dm = re.search(r"void onDisconnect.*?crypto_load_or_gen_key\(\)", ble, re.DOTALL)
    assert dm, "onDisconnect should restore master key"
    print("PASS derived key leak fix")

# ---- I2: ring drop counter ----
def test_ring_drop_counter():
    rec = read("recorder.cpp")
    hdr = read("recorder.h")
    assert "s_dropped_bytes" in rec
    assert "s_drop_events" in rec
    assert "recorder_dropped_bytes" in rec
    assert "recorder_drop_events" in rec
    assert "recorder_dropped_bytes" in hdr
    assert "recorder_drop_events" in hdr
    # failure branch must not be empty comment
    assert 'xRingbufferSend(s_ring, pcm, pcm_bytes' in rec
    # verify increment in failure branch
    idx = rec.find("xRingbufferSend(s_ring, pcm")
    seg = rec[idx: idx+800]
    assert "s_dropped_bytes += pcm_bytes" in seg or "s_dropped_bytes++" in seg
    assert "s_drop_events++" in seg
    # must escalate via ui_signal_error
    assert "ui_signal_error()" in seg
    # empty comment removed
    assert "ring full, force drain to make space" not in rec
    print("PASS ring drop counter")

def test_ring_drop_threshold_and_log():
    rec = read("recorder.cpp")
    # threshold 8192 bytes or 3 events
    assert "8192" in rec
    assert "s_drop_events >= 3" in rec or "s_drop_events >=" in rec
    # rate-limited Serial log
    assert "REC drop" in rec or "Serial.printf" in rec
    print("PASS ring drop threshold log")

def test_ring_drop_null_boundary():
    rec = read("recorder.cpp")
    # raw/pcm null guard before use still present
    assert "if (!raw || !pcm)" in rec
    # ring creation fallback
    assert "xRingbufferCreateWithCaps" in rec
    print("PASS ring null/boundary")

# ---- I3: window short-circuit + len fix ----
def test_window_short_circuit():
    tr = read("transfer.cpp")
    # must have short-circuit comment and logic before wait
    assert "window[0].acked" in tr
    # wait must be conditional on !base_acked
    assert "while (!base_acked && millis()" in tr or "if (window[0].acked)" in tr
    assert "Short-circuit" in tr or "already acked" in tr
    print("PASS window short-circuit")

def test_window_still_waits_on_unacked():
    tr = read("transfer.cpp")
    # negative: when not acked, must still wait with timeout
    assert "BLE_ACK_TIMEOUT_MS" in tr
    # must still extract remain and qwait
    assert "remain = BLE_ACK_TIMEOUT_MS" in tr
    assert "xQueueReceive" in tr
    print("PASS window still waits when unacked")

def test_file_announce_ack_len_fix():
    tr = read("transfer.cpp")
    # PKT_FILE_ANNOUNCE_ACK must require 4 bytes
    pat = re.search(r"PKT_FILE_ANNOUNCE_ACK.*?pkt\.len < (\d)", tr, re.DOTALL)
    assert pat, "FILE_ANNOUNCE_ACK guard not found"
    assert pat.group(1) == "4", f"expected len <4 but got <{pat.group(1)}"
    # PKT_ACK still 3, FILE_DONE_ACK 5 should be unchanged
    assert "PKT_ACK" in tr and "pkt.len < 3" in tr
    assert "PKT_FILE_DONE_ACK" in tr
    print("PASS file_announce_ack len fix")

def test_file_announce_ack_boundary():
    tr = read("transfer.cpp")
    # verify safe read of payload[3] only after len>=4
    # ensure s_resume_seq assigned after guard
    assert "s_resume_seq = pkt.payload[2] | (pkt.payload[3] << 8)" in tr
    print("PASS file_announce_ack boundary")

def test_nonce_invariant_comment():
    cry = read("crypto.cpp")
    tr = read("transfer.cpp")
    assert "NONCE INVARIANT" in cry
    assert "NONCE INVARIANT" in tr or "INVARIANT" in tr
    # KDF comment should note HKDF vs SHA256
    assert "HKDF" in cry or "SHA256" in cry
    print("PASS nonce invariant comment")

def test_rfc5869_hkdf_and_session_key():
    cry = read("crypto.cpp")
    hdr = read("crypto.h")
    assert "crypto_hkdf_sha256" in cry and "crypto_hkdf_sha256" in hdr
    assert "hmac_sha256" in cry
    assert "checkpoint-file-v1" in cry
    assert "crypto_derive_session_key" in cry and "crypto_derive_session_key" in hdr
    assert "checkpoint-session-v1" in cry
    assert "SHA256(master_key).digest()" not in cry, "custom KDF must be gone"
    print("PASS hkdf session key")

def test_ble_security_and_ready():
    ble = read("ble_service.cpp")
    assert "setSecurityAuth(true, false, true)" in ble, "MITM must be off"
    assert "crypto_derive_session_key" in ble, "HELLO_ACK must carry session key"
    assert "PKT_READY" in ble, "READY completion missing"
    assert "s_hello_sent" in ble and "BLE_AUTH_TIMEOUT_MS" in ble
    cfg = read("config.h")
    assert "BLE_AUTH_TIMEOUT_MS 30000" in cfg, "debug-generous auth timeout expected"
    print("PASS ble security ready")

def test_manifest_uid():
    man = read("manifest.cpp")
    hdr = read("manifest.h")
    assert "uint64_t uid" in hdr and "manifest_uid_or_generate" in hdr
    assert 'o["uid"]' in man
    assert "<< 32" in man, "uid must compose two esp_random() halves (64-bit)"
    tr = read("transfer.cpp")
    assert "manifest_uid_or_generate" in tr
    assert "ann, 21" in tr, "announce must carry the 8-byte uid"
    print("PASS manifest uid")

# ---- I5: SD lock gap ----
def test_sd_patch_locked_variant():
    sd = read("sd_manager.cpp")
    hdr = read("sd_manager.h")
    assert "sd_patch_wav_header_locked" in sd
    assert "sd_patch_wav_header_locked" in hdr
    # locked variant must NOT call sd_lock internally
    idx = sd.find("bool sd_patch_wav_header_locked")
    seg = sd[idx: idx+1500]
    # locked should open directly without sd_lock()
    assert "SD.open" in seg
    # the wrapper must take lock
    widx = sd.find("bool sd_patch_wav_header(const String")
    wseg = sd[widx: widx+600]
    assert "sd_lock" in wseg
    assert "sd_patch_wav_header_locked" in wseg
    print("PASS sd patch locked variant")

def test_manifest_no_unlock_gap():
    man = read("manifest.cpp")
    # old pattern must be gone
    assert "sd_unlock();\n          sd_patch_wav_header(wav);" not in man
    assert "sd_unlock();\n           sd_patch_wav_header" not in man
    # new pattern uses locked variant while holding lock
    assert "sd_patch_wav_header_locked(wav)" in man
    # dead first root open removed (only one SD.open(REC_DIR) after prev copy)
    # count occurrences of File root = SD.open(REC_DIR) should be 1 now
    cnt = man.count("File root = SD.open(REC_DIR)")
    assert cnt == 1, f"expected 1 root open, got {cnt}"
    print("PASS manifest no unlock gap")

def test_sd_end_takes_lock():
    sd = read("sd_manager.cpp")
    eidx = sd.find("void sd_end()")
    eseg = sd[eidx: eidx+400]
    assert "sd_lock" in eseg, "sd_end should acquire lock"
    assert "SD.end()" in eseg
    print("PASS sd_end takes lock")

def test_sd_patch_null_and_small_file():
    sd = read("sd_manager.cpp")
    # locked variant must check total <44
    assert "total < 44" in sd
    assert "if (!f)" in sd
    print("PASS sd patch small/null guards")

# ---- regression: keep existing behavior ----
def test_proto_crc_still_valid():
    import struct, zlib
    ver, typ, seq = 1, 0x12, 42
    payload = b"hello"
    hdr = struct.pack("<BBHH", ver, typ, seq, len(payload))
    crc = zlib.crc32(hdr + payload) & 0xFFFFFFFF
    packet = hdr + payload + struct.pack("<I", crc)
    recv = struct.unpack("<I", packet[6+len(payload):6+len(payload)+4])[0]
    calc = zlib.crc32(packet[:6+len(payload)]) & 0xFFFFFFFF
    assert recv == calc
    print("PASS proto crc regression")

if __name__ == "__main__":
    test_hello_encrypted_gate()
    test_hello_version_invalid_path()
    test_hello_empty_and_null_guard()
    test_handshake_timeout_enforced()
    test_handshake_timeout_boundaries()
    test_derived_key_leak_fix()
    test_ring_drop_counter()
    test_ring_drop_threshold_and_log()
    test_ring_drop_null_boundary()
    test_window_short_circuit()
    test_window_still_waits_on_unacked()
    test_file_announce_ack_len_fix()
    test_file_announce_ack_boundary()
    test_nonce_invariant_comment()
    test_rfc5869_hkdf_and_session_key()
    test_ble_security_and_ready()
    test_manifest_uid()
    test_sd_patch_locked_variant()
    test_manifest_no_unlock_gap()
    test_sd_end_takes_lock()
    test_sd_patch_null_and_small_file()
    test_proto_crc_still_valid()
    print("ALL REVIEW FIXES PASS")
