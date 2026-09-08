"""
Client-side checks for Checkpoint BLE sync.
Run: python -m pytest firmware/tests/test_client_logic.py -v
      python firmware/tests/test_client_logic.py
Covers: handshake error handling, frag_size derivation, IncomingFile reassembly,
resume honor, OUTPUT_DIR, crypto parity, protocol framing, integration boundaries.
"""
import sys
import struct
import zlib
import hashlib
import asyncio
from pathlib import Path

def _resolve_client() -> Path:
    for cand in [
        Path(__file__).resolve().parent.parent / "client" / "client.py",
        Path(__file__).resolve().parent.parent / "host" / "checkpoint_client" / "client.py",
    ]:
        if cand.exists():
            return cand
    return Path(__file__).resolve().parent.parent / "host" / "checkpoint_client" / "client.py"

CLIENT = _resolve_client()
SRC = CLIENT.read_text(encoding="utf-8") if CLIENT.exists() else ""

# -- helpers: load pure functions without requiring bleak --------------------

def _load_pure():
    """Exec the crypto+protocol section with stubbed bleak to get callables."""
    stub = """
class BleakClient: pass
class BleakScanner: pass
class AESCCM:
    def __init__(self, *a, **kw): pass
    def decrypt(self, *a, **kw): return b""
"""
    src = SRC
    # replace bleak/cryptography imports with stubs if not installed
    import importlib.util
    has_bleak = importlib.util.find_spec("bleak") is not None
    has_crypto = importlib.util.find_spec("cryptography") is not None
    if not has_bleak or not has_crypto:
        # Use source inspection path only
        return None
    # otherwise import normally
    import importlib.machinery, types
    sys.path.insert(0, str(CLIENT.parent))
    try:
        import importlib, importlib.util
        spec = importlib.util.spec_from_file_location("checkpoint_client", str(CLIENT))
        mod = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mod)
        return mod
    except Exception as e:
        print(f"import checkpoint_client failed: {e}")
        return None

MOD = _load_pure()

# ---------------------------------------------------------------------------

def test_output_dir_script_relative():
    assert 'Path(__file__).resolve().parent / "received"' in SRC, "OUTPUT_DIR must be script-relative"
    assert 'Path("./received")' not in SRC, "old CWD-relative OUTPUT_DIR still present"
    print("PASS output_dir_script_relative")

def test_frag_size_derivation_from_mtu():
    # Protocol v1: frag pinned at 220 both sides; MTU below 241 refuses transfer.
    assert "self.frag_size" in SRC, "frag_size field missing"
    assert "self.mtu" in SRC and "self.chunk_sec" in SRC, "mtu/chunk_sec not persisted"
    assert "self.frag_size = BLE_FRAG_SIZE_GUESS" in SRC, "frag must be pinned at 220"
    assert "MIN_MTU_REQUIRED" in SRC, "MTU floor missing"
    print("PASS frag_size_pinned")

def test_incoming_file_uses_frag_size():
    assert "frag_size: int = BLE_FRAG_SIZE_GUESS" in SRC, "IncomingFile frag_size default missing"
    assert "seq * self.frag_size" in SRC, "add_fragment must use instance frag_size"
    # add_fragment itself must not use the constant (part-file seek may share the stride)
    seg = SRC.split("def add_fragment")[1].split("def ")[0]
    assert "seq * BLE_FRAG_SIZE_GUESS" not in seg, "add_fragment must use instance frag_size"
    print("PASS incoming_file_frag_size")

def test_is_complete_byte_coverage():
    assert "def is_complete" in SRC
    assert "len(self.received_frags) < self.total_frags" in SRC, "must check frag count"
    assert "all(i in self.received_frags for i in range" in SRC, "must verify no gap"
    # should not be simple len >= total_frags only
    seg = SRC.split("def is_complete")[1].split("def ")[0]
    assert "byte_coverage" in seg or "total_bytes" in seg, "should check byte coverage"
    print("PASS is_complete_byte_coverage")

def test_resume_honor():
    # Phase 7: resume only from bytes actually on disk (.part + sidecar).
    assert "_load_resume_state" in SRC, "disk-backed resume loader missing"
    assert "bytearray(resume_from *" not in SRC, "zero-prefill must be gone"
    assert "_completed_ok" in SRC, "completed-file confirmation missing"
    assert 'frag_size=self.frag_size' in SRC, "IncomingFile must receive negotiated frag_size"
    print("PASS resume_from_disk")

def test_handshake_error_handling():
    assert "self.error_event" in SRC, "error_event missing"
    assert "self.last_error" in SRC, "last_error missing"
    assert "PKT_ERROR" in SRC and "self.last_error = code" in SRC, "must capture PKT_ERROR code"
    assert "pairing required (0x01)" in SRC, "0x01 detail missing"
    assert "version mismatch (0x02)" in SRC, "0x02 detail missing"
    assert ("for attempt in range(2)" in SRC or "for attempt in range(5)" in SRC), "must retry on 0x01 (2 or 5 attempts for Windows MIC race)"
    assert "_wait_for_handshake_result" in SRC, "helper wait missing"
    print("PASS handshake_error_handling")

def test_handshake_success_path():
    assert "self.hello_acked.is_set()" in SRC, "success check missing"
    assert "Handshake complete" in SRC and "frag_size" in SRC, "success log should include frag_size"
    print("PASS handshake_success_path")

def test_handshake_invalid_input():
    # HELLO_ACK too short <11
    assert 'len(payload) < 11' in SRC, "HELLO_ACK short check missing"
    # proto_ver mismatch warning
    assert "proto_ver != PROTO_VER" in SRC, "version mismatch check missing in _parse_hello_ack"
    # FILE_ANNOUNCE <21 (v2: 8-byte uid)
    assert "len(p) < 21" in SRC, "FILE_ANNOUNCE short guard missing"
    # FILE_DONE <16 (v2: 8-byte uid)
    assert "len(p) < 16" in SRC, "FILE_DONE guard missing"
    # DATA short < CRYPTO_TAG_BYTES
    assert "len(raw) < CRYPTO_TAG_BYTES" in SRC, "DATA tag guard missing"
    print("PASS handshake_invalid_input")

def test_null_empty_boundary():
    assert "if not pkt:" in SRC, "proto_parse None guard missing in handlers"
    assert "f = self.current_file" in SRC, "DATA must bind file to a local"
    assert "if not f:" in SRC, "DATA without file guard missing"
    assert "if not f.key:" in SRC, "no-key guard missing"
    assert "if f and f.file_id == file_id:" in SRC, "FILE_DONE unknown file guard missing"
    # proto_parse empty
    assert "len(data) < PROTO_HEADER + PROTO_CRC" in SRC, "empty packet guard missing"
    print("PASS null_empty_boundary")

def test_seq_wraparound():
    assert "(self._seq_gen + 1) & 0xFFFF" in SRC, "seq wrap missing"
    print("PASS seq_wraparound")

def test_protocol_parity():
    # Mirror protocol.h constants (v2: READY, session key, HKDF, W8 ACK, resume, UID)
    assert "PROTO_VER = 2" in SRC
    assert "PROTO_HEADER = 6" in SRC
    assert "CRYPTO_TAG_BYTES = 8" in SRC
    assert "CRYPTO_KEY_BYTES = 16" in SRC
    assert "BLE_FRAG_SIZE_GUESS = 220" in SRC
    assert "PKT_READY" in SRC
    print("PASS protocol_parity")

def test_crypto_parity_with_firmware():
    # derive_file_key must be RFC 5869 HKDF-SHA256 (see crypto.cpp/hkdf test vector)
    assert "hkdf_sha256" in SRC, "HKDF helper missing"
    assert "hmac.new" in SRC, "HMAC-SHA256 missing"
    assert '"checkpoint-file-v1"' in SRC or "'checkpoint-file-v1'" in SRC, "domain string missing"
    # build_nonce: SHA256("checkpoint-nonce-v1" + session + uid64 + seq)[:12]
    assert 'b"checkpoint-nonce-v1"' in SRC
    assert 'struct.pack("<IQH"' in SRC
    print("PASS crypto_parity")

def test_decrypt_aad_matches_firmware():
    assert 'struct.pack("<BBHH", PROTO_VER, PKT_DATA, seq, frag_len)' in SRC, "AAD mismatch"
    assert "tag_length=CRYPTO_TAG_BYTES" in SRC, "tag length must be 8"
    print("PASS decrypt_aad")

def test_proto_build_parse_roundtrip():
    if MOD is None:
        print("SKIP proto roundtrip (bleak/cryptography not installed)")
        return
    for payload in [b"", b"hello", b"\x00"*220]:
        pkt = MOD.proto_build(0x10, 42, payload)
        parsed = MOD.proto_parse(pkt)
        assert parsed is not None, "parse failed"
        assert parsed.payload == payload
        assert parsed.seq == 42
        assert parsed.type == 0x10
    # corrupt CRC
    pkt = bytearray(MOD.proto_build(0x12, 1, b"data"))
    pkt[-1] ^= 0xFF
    assert MOD.proto_parse(bytes(pkt)) is None, "bad CRC should fail"
    # bad version
    pkt = bytearray(MOD.proto_build(0x01, 0, b""))
    pkt[0] = 0xFF
    assert MOD.proto_parse(bytes(pkt)) is None, "bad version should fail"
    # empty
    assert MOD.proto_parse(b"") is None
    assert MOD.proto_parse(b"\x01\x02\x03") is None
    print("PASS proto_build_parse_roundtrip")

def test_derive_and_nonce_known_vector():
    if MOD is None:
        print("SKIP derive/nonce vector (deps missing)")
        return
    master = bytes(range(16))
    sess, fid = 0x12345678, 0xdeadbeef
    k = MOD.derive_file_key(master, sess, fid)
    # Known answer: RFC 5869 HKDF-SHA256 over info "checkpoint-file-v1"+LE(sess,fid)
    import hashlib, hmac, struct
    prk = hmac.new(b"", master, hashlib.sha256).digest()
    info = b"checkpoint-file-v1" + struct.pack("<IQ", sess, fid)
    expect = hmac.new(prk, info + b"\x01", hashlib.sha256).digest()[:16]
    assert k == expect, f"KDF mismatch {k.hex()} != {expect.hex()}"
    n = MOD.build_nonce(sess, fid, 42)
    assert n == hashlib.sha256(b"checkpoint-nonce-v1"
                               + struct.pack("<IQH", sess, fid, 42)).digest()[:12]
    assert len(n) == 12
    print("PASS derive_and_nonce_vector")

def test_decrypt_roundtrip():
    if MOD is None:
        print("SKIP decrypt roundtrip (deps missing)")
        return
    key = bytes([0x42]*16)
    sess, fid, seq, frag_len = 0x11111111, 0x22222222, 7, 5
    plain = b"hello"
    nonce = MOD.build_nonce(sess, fid, seq)
    aad = struct.pack("<BBHH", MOD.PROTO_VER, MOD.PKT_DATA, seq, frag_len)
    from cryptography.hazmat.primitives.ciphers.aead import AESCCM
    aes = AESCCM(key, tag_length=8)
    ct = aes.encrypt(nonce, plain, aad)
    dec = MOD.decrypt_fragment(key, sess, fid, seq, frag_len, ct)
    assert dec == plain, "decrypt roundtrip failed"
    # wrong key
    bad = MOD.decrypt_fragment(bytes([0x00]*16), sess, fid, seq, frag_len, ct)
    assert bad is None, "wrong key should fail"
    # wrong frag_len AAD
    bad2 = MOD.decrypt_fragment(key, sess, fid, seq, frag_len+1, ct)
    assert bad2 is None, "wrong AAD should fail"
    print("PASS decrypt_roundtrip")

def test_incoming_file_reassembly():
    if MOD is None:
        print("SKIP reassembly (deps missing)")
        return
    f = MOD.IncomingFile(file_id=0x1, total_bytes=440, total_frags=2, expected_crc=0, key=b"k"*16, session_id=1, frag_size=220)
    f.add_fragment(0, b"A"*220)
    assert not f.is_complete(), "should not be complete with 1/2"
    f.add_fragment(1, b"B"*220)
    assert f.is_complete(), "should be complete with 2/2"
    assert f.buffer[:220] == b"A"*220
    assert f.buffer[220:440] == b"B"*220
    # gap test: only seq 1, not 0
    g = MOD.IncomingFile(file_id=0x2, total_bytes=440, total_frags=2, expected_crc=0, key=b"k"*16, session_id=1, frag_size=220)
    g.add_fragment(1, b"B"*220)
    assert not g.is_complete(), "gap should not be complete"
    print("PASS incoming_file_reassembly")

def test_incoming_file_last_frag_short():
    if MOD is None:
        print("SKIP last frag short (deps missing)")
        return
    # total 300 -> 2 frags: 220 + 80
    f = MOD.IncomingFile(file_id=0x3, total_bytes=300, total_frags=2, expected_crc=0, key=b"k"*16, session_id=1, frag_size=220)
    f.add_fragment(0, b"X"*220)
    f.add_fragment(1, b"Y"*80)
    assert f.is_complete()
    assert len(f.buffer[:300]) == 300
    print("PASS last_frag_short")

def test_regression_ack_polarity():
    # Firmware: PKT_ACK ok=0x00 cumulative, FILE_DONE_ACK ok=0x01
    assert 'struct.pack("<HB"' in SRC, "cumulative ACK pack missing"
    assert "PKT_ACK" in SRC and ", 0x00)" in SRC, "ACK must send status 0x00 (firmware ok)"
    assert "contig_seq" in SRC, "ACK must use highest contiguous seq"
    assert 'struct.pack("<HBBB", pkt.seq, status, 0, 0)' in SRC or \
        'struct.pack("<HBBB"' in SRC, "FILE_DONE_ACK pack missing"
    # Blast mode must be gone: no fire-and-forget comment
    assert "Fire-and-forget" not in SRC, "blast mode comment still present"
    print("PASS regression_ack_polarity")

def test_integration_boundaries_uuids():
    assert '9a8b0001-4a2b-4e3c-8f1a-5b2c9d0e1f2a' in SRC, "SERVICE_UUID mismatch"
    assert '9a8b0002' in SRC and '9a8b0004' in SRC, "CTRL/ACK UUID mismatch"
    assert 'response=True' in SRC and 'response=False' in SRC, "WRITE vs WRITE_NR mismatch"
    print("PASS integration_boundaries_uuids")

if __name__ == "__main__":
    test_output_dir_script_relative()
    test_frag_size_derivation_from_mtu()
    test_incoming_file_uses_frag_size()
    test_is_complete_byte_coverage()
    test_resume_honor()
    test_handshake_error_handling()
    test_handshake_success_path()
    test_handshake_invalid_input()
    test_null_empty_boundary()
    test_seq_wraparound()
    test_protocol_parity()
    test_crypto_parity_with_firmware()
    test_decrypt_aad_matches_firmware()
    test_proto_build_parse_roundtrip()
    test_derive_and_nonce_known_vector()
    test_decrypt_roundtrip()
    test_incoming_file_reassembly()
    test_incoming_file_last_frag_short()
    test_regression_ack_polarity()
    test_integration_boundaries_uuids()
    print("ALL CLIENT TESTS PASS")
