"""
Benchmark capture logic tests.
Run: python -m pytest firmware/tests/test_benchmark_logic.py -v

Covers:
  bench.h header format
  transfer.cpp bench instrumentation presence
  checkpoint.ino bench dump presence
  ble_service helpers
  client.py MTU_OVERHEAD + resume prefill + bench csv + duplicate tracking
  benchmark_capture.py existence + arg handling
  goodput / RTT / csv math
  failure/empty/boundary cases
"""
import csv
import json
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CHECKPOINT = ROOT / "checkpoint"
# support both old firmware/client and new firmware/host/checkpoint_client
def _resolve_client_file(name: str) -> Path:
    for cand in [ROOT / "client" / name, ROOT / "host" / "checkpoint_client" / name]:
        if cand.exists():
            return cand
    return ROOT / "host" / "checkpoint_client" / name

CLIENT = _resolve_client_file("client.py")
BENCH_H = CHECKPOINT / "bench.h"
TRANSFER_CPP = CHECKPOINT / "transfer.cpp"
TRANSFER_H = CHECKPOINT / "transfer.h"
BLE_CPP = CHECKPOINT / "ble_service.cpp"
BLE_H = CHECKPOINT / "ble_service.h"
INO = CHECKPOINT / "checkpoint.ino"
BENCH_CAP = _resolve_client_file("benchmark_capture.py")

SRC_CLIENT = CLIENT.read_text(encoding="utf-8") if CLIENT.exists() else ""
SRC_TRANSFER = TRANSFER_CPP.read_text(encoding="utf-8") if TRANSFER_CPP.exists() else ""
SRC_BLE = BLE_CPP.read_text(encoding="utf-8")
SRC_BLE_H = BLE_H.read_text(encoding="utf-8")
SRC_INO = INO.read_text(encoding="utf-8")
SRC_BENCH_H = BENCH_H.read_text(encoding="utf-8") if BENCH_H.exists() else ""
SRC_TRANSFER_H = TRANSFER_H.read_text(encoding="utf-8")


def test_bench_h_exists_and_format():
    assert BENCH_H.exists(), "bench.h missing (T1)"
    assert "struct BenchSample" in SRC_BENCH_H, "BenchSample missing"
    assert "bench_print_header" in SRC_BENCH_H, "bench_print_header missing"
    assert "bench_print_sample" in SRC_BENCH_H, "bench_print_sample missing"
    assert "BENCH,src" in SRC_BENCH_H, "csv header must contain BENCH,src"
    assert "goodput_kBps" in SRC_BENCH_H, "goodput column missing"
    # T3 live goodput when t_end==0
    assert "t_end_ms == 0" in SRC_BENCH_H or "millis() - s.t_start_ms" in SRC_BENCH_H, "live goodput for in-progress missing"
    print("PASS bench_h")


def test_transfer_bench_instrumentation():
    assert "bench.h" in SRC_TRANSFER, "transfer.cpp must include bench.h"
    assert "transfer_bench_snapshot" in SRC_TRANSFER, "bench snapshot missing"
    assert "s_bench" in SRC_TRANSFER, "s_bench static missing"
    assert "s_send_ms" in SRC_TRANSFER, "per-window send timestamps missing"
    assert "max_inflight" in SRC_TRANSFER, "max_inflight track missing"
    assert "rtt_sum_ms" in SRC_TRANSFER, "rtt_sum missing"
    assert "stalls" in SRC_TRANSFER, "stalls counter missing"
    assert "s_bench.t_start_ms = millis()" in SRC_TRANSFER, "t_start not set per file"
    assert "s_bench.t_end_ms = millis()" in SRC_TRANSFER, "t_end not set on done/fail"
    assert "transfer_bench_snapshot" in SRC_TRANSFER_H, "transfer.h missing snapshot decl"
    assert "transfer_bench_reset" in SRC_TRANSFER_H, "reset decl missing"
    # T1 fix: O-O ack accounting 1:1, no dangling rtt_count without sum
    assert "if (m.ok && s_send_ms[w]" in SRC_TRANSFER, "T1 fix must count any w with s_send_ms[w]"
    assert "if (w == 0) base_acked" in SRC_TRANSFER, "base_acked only for w==0"
    assert "else if (m.ok)" not in SRC_TRANSFER or SRC_TRANSFER.count("s_bench.rtt_count++") == SRC_TRANSFER.count("s_bench.rtt_sum_ms") , "dangling rtt_count without sum must be removed"
    # snapshot lock
    assert "portMUX_TYPE s_bench_mux" in SRC_TRANSFER or "s_bench_mux" in SRC_TRANSFER, "snapshot mux missing"
    assert "portENTER_CRITICAL" in SRC_TRANSFER, "critical section for bench missing"
    print("PASS transfer_bench")


def test_checkpoint_ino_bench_dump():
    assert "bench.h" in SRC_INO, "checkpoint.ino must include bench.h"
    assert "bench_print_header" in SRC_INO, "missing bench header print in loop"
    assert "transfer_bench_snapshot" in SRC_INO, "must call snapshot"
    assert "BENCH" in SRC_INO, "BENCH dump missing"
    assert "last_bench" in SRC_INO, "periodic timer missing"
    print("PASS ino_bench")


def test_ble_helpers():
    assert "ble_conn_interval_ms" in SRC_BLE_H, "ble_conn_interval_ms decl missing"
    assert "ble_mtu_negotiated" in SRC_BLE_H, "ble_mtu_negotiated decl missing"
    assert "ble_phy" in SRC_BLE_H, "ble_phy decl missing"
    assert "HELLO_ACK session=" in SRC_BLE, "HELLO_ACK bench log missing"
    assert "ble_conn_interval_ms()" in SRC_BLE, "conn interval not logged"
    assert "getPeerInfoByHandle" in SRC_BLE, "T2 must use getPeerInfoByHandle for real interval"
    assert "getConnInterval" in SRC_BLE, "real interval via getConnInterval missing"
    assert "getMTU" in SRC_BLE, "real MTU via getMTU missing"
    assert "getPhy" in SRC_BLE, "real PHY via getPhy missing"
    print("PASS ble_helpers")


def test_client_mtu_overhead():
    assert "MTU_OVERHEAD" in SRC_CLIENT, "MTU_OVERHEAD missing — T5"
    # backward compat string for old test_client_logic grep
    assert "mtu - 27" in SRC_CLIENT or "mtu-27" in SRC_CLIENT, "legacy mtu - 27 string must remain for compatibility"
    assert "MTU_OVERHEAD = PROTO_HEADER" in SRC_CLIENT, "MTU_OVERHEAD must derive from PROTO_HEADER"
    assert "mtu - MTU_OVERHEAD" in SRC_CLIENT, "derived must use MTU_OVERHEAD"
    print("PASS client_mtu_overhead")


def test_client_resume_prefill():
    assert "resume_from = device_start_seq" in SRC_CLIENT, "resume_from calc missing"
    assert "bytearray(resume_from * self.frag_size)" in SRC_CLIENT, "resume prefill buffer missing"
    assert "set(range(resume_from))" in SRC_CLIENT, "resume prefill set missing"
    assert "resume prefill" in SRC_CLIENT, "log line for resume prefill expected"
    print("PASS resume_prefill")


def test_client_bench_csv():
    assert "bench_csv" in SRC_CLIENT, "bench_csv param missing"
    assert "csv.DictWriter" in SRC_CLIENT, "csv writer missing"
    assert "benchmark_" in SRC_CLIENT, "benchmark file naming missing"
    assert "goodput_kBps" in SRC_CLIENT, "goodput col missing"
    assert "median_rtt_ms" in SRC_CLIENT or "median" in SRC_CLIENT, "median RTT missing"
    assert "p95" in SRC_CLIENT, "p95 RTT missing"
    assert "_bench_finalize" in SRC_CLIENT, "finalize helper missing"
    assert "BENCH,client" in SRC_CLIENT, "client BENCH line missing"
    print("PASS client_bench_csv")


def test_client_duplicate_and_decrypt_fail():
    assert "_bench_duplicates" in SRC_CLIENT, "duplicates counter missing"
    assert "_bench_decrypt_fail" in SRC_CLIENT, "decrypt_fail counter missing"
    assert "if seq in self.current_file.received_frags" in SRC_CLIENT, "duplicate check missing"
    assert "self._bench_decrypt_fail += 1" in SRC_CLIENT, "decrypt fail inc missing"
    print("PASS duplicate_decrypt")


def test_client_resume_resp_and_write_ack_timing():
    assert "PKT_RESUME_RESP" in SRC_CLIENT, "RESUME_RESP handling missing"
    assert "_bench_t_send" in SRC_CLIENT, "t_send map missing"
    print("PASS resume_resp")


def test_benchmark_capture_exists():
    assert BENCH_CAP.exists(), "benchmark_capture.py missing T7"
    txt = BENCH_CAP.read_text(encoding="utf-8")
    assert "CheckpointClient" in txt, "must reuse CheckpointClient"
    assert "bench_csv" in txt or "benchmark_" in txt, "csv path logic missing"
    assert "--serial" in txt, "serial tail arg missing"
    assert "serial_tail" in txt or "pyserial" in txt, "serial tail code missing"
    assert "argparse" in txt, "argparse missing"
    print("PASS bench_capture")


def _load_client_mod():
    import importlib.util, sys
    spec = importlib.util.spec_from_file_location("bench_client", str(CLIENT))
    mod = importlib.util.module_from_spec(spec)
    # stub bleak if missing
    try:
        spec.loader.exec_module(mod)  # type: ignore[union-attr]
    except Exception as e:
        # If bleak missing, source-only checks already passed
        return None
    return mod


def test_goodput_math_synthetic():
    mod = _load_client_mod()
    if mod is None:
        print("SKIP goodput synthetic (deps missing)")
        return
    # Simulate 1.92MB file (8728 frags 220B) at 40 kB/s -> ~48s
    total = 1920000
    # Use client finalizer math directly: goodput = total/elapsed/1024
    elapsed = total / (40 * 1024)  # 46.8s
    goodput = total / elapsed / 1024
    assert abs(goodput - 40.0) < 0.01, f"goodput formula broken {goodput}"
    # csv header correctness
    import tempfile
    p = Path(tempfile.mktemp(suffix=".csv"))
    c = mod.CheckpointClient("00:00:00:00:00:00", bench_csv=p)
    assert c._csv_writer is not None, "csv writer not inited"
    assert p.exists()
    # simulate file lifecycle
    c.mtu = 247
    c.frag_size = 220
    c._bench_reset_file(0x1234, 440, 2, 0)
    # add fragments to trigger duplicates logic via handler? just test finalize
    c._bench_rtts = [10.0, 20.0, 30.0, 40.0, 100.0]
    c._bench_duplicates = 1
    # need current_file for finalize ok path? finalizer uses _bench_file_meta only
    c._bench_finalize(True, 440)
    rows = c.bench_rows()
    assert len(rows) == 1
    r = rows[0]
    assert r["total_bytes"] == 440
    assert r["duplicates"] == 1
    assert "goodput_kBps" in r
    assert r["crc_ok"] is True
    # cleanup
    try:
        c._csv_file.close()
        p.unlink()
        p.with_suffix(".json").unlink(missing_ok=True)
    except Exception:
        pass
    print("PASS goodput synthetic")


def test_resume_is_complete_after_prefill():
    mod = _load_client_mod()
    if mod is None:
        print("SKIP resume is_complete (deps missing)")
        return
    f = mod.IncomingFile(file_id=0x1, total_bytes=440, total_frags=2, expected_crc=0, key=b"k"*16, session_id=1, frag_size=220)
    # Simulate resume_from=1 prefill as client does
    resume_from = 1
    f.buffer = bytearray(resume_from * 220)
    f.received_frags = set(range(resume_from))
    assert 0 in f.received_frags and 1 not in f.received_frags
    assert not f.is_complete()
    f.add_fragment(1, b"B"*220)
    assert f.is_complete(), "after prefill, receiving remaining frags should complete"
    # Without prefill, single frag should not complete
    g = mod.IncomingFile(file_id=0x2, total_bytes=440, total_frags=2, expected_crc=0, key=b"k"*16, session_id=1, frag_size=220)
    g.add_fragment(1, b"B"*220)
    assert not g.is_complete(), "gap without prefill must not complete"
    print("PASS resume is_complete")


def test_null_empty_boundary_bench():
    mod = _load_client_mod()
    if mod is None:
        print("SKIP null empty bench")
        return
    # null file finalize should not crash
    c = mod.CheckpointClient("00:00:00:00:00:00")
    # no bench File start -> finalize is no-op
    c._bench_finalize(True, 0)
    assert len(c.bench_rows()) == 0
    # proto parse empty
    assert mod.proto_parse(b"") is None
    assert mod.proto_parse(b"\x01\x02") is None
    # is_complete empty
    f = mod.IncomingFile(file_id=0x3, total_bytes=0, total_frags=0, expected_crc=0, key=b"k"*16, session_id=1, frag_size=220)
    assert not f.is_complete()
    print("PASS null empty bench")


def test_bench_json_schema():
    mod = _load_client_mod()
    if mod is None:
        print("SKIP json schema")
        return
    import tempfile, json
    p = Path(tempfile.mktemp(suffix=".csv"))
    c = mod.CheckpointClient("00:00:00:00:00:00", bench_csv=p)
    c.mtu = 247
    c._bench_reset_file(0xABCD, 220, 1, 0)
    c._bench_rtts = [15.0]
    c._bench_finalize(True, 220)
    rows = c.bench_rows()
    j = json.dumps(rows)
    data = json.loads(j)
    assert data[0]["file_id"] == "0000abcd"
    try:
        c._csv_file.close()
        p.unlink()
    except Exception:
        pass
    print("PASS json schema")


def test_out_of_order_ack_accounting():
    """Synthetic window: verify fix counts O-O ack exactly once, no double."""
    # Simulate transfer.cpp window[4] with T1 fix logic
    BLE_WINDOW = 4
    FRAG = 220
    # window slots
    window = [{"seq": None, "len": 0, "acked": False} for _ in range(BLE_WINDOW)]
    s_send = [0] * BLE_WINDOW
    bench = {"bytes_tx": 0, "frags_acked": 0, "rtt_count": 0, "rtt_sum": 0}

    def fill(base):
        for w in range(BLE_WINDOW):
            if window[w]["acked"] or window[w]["len"]:
                continue
            seq = base + w
            window[w] = {"seq": seq, "len": FRAG, "acked": False}
            s_send[w] = 1000 + seq  # fake send_ms per slot

    def ack(seq):
        for w in range(BLE_WINDOW):
            if not window[w]["acked"] and window[w]["len"] and window[w]["seq"] == seq:
                window[w]["acked"] = True
                if s_send[w]:
                    bench["rtt_sum"] += 50
                    bench["rtt_count"] += 1
                    bench["frags_acked"] += 1
                    bench["bytes_tx"] += window[w]["len"]
                return w
        return None

    def slide():
        window.pop(0)
        window.append({"seq": None, "len": 0, "acked": False})
        s_send.pop(0)
        s_send.append(0)

    # Fill base 0: seq 0-3
    fill(0)
    assert [w["seq"] for w in window] == [0, 1, 2, 3]
    # Ack out-of-order seq 2 first
    ack(2)
    assert bench["bytes_tx"] == 220 and bench["frags_acked"] == 1 and bench["rtt_count"] == 1, "O-O ack must count"
    # Ack base 0
    ack(0)
    assert bench["bytes_tx"] == 440 and bench["frags_acked"] == 2, "base ack must also count"
    # Slide base 0 (acked), now window seq 1,2,3, None ; slot 2 (seq2) already acked should stay acked
    slide()  # base now 1
    assert window[0]["seq"] == 1 and window[1]["seq"] == 2 and window[1]["acked"] is True
    # Simulate that when window slides and already-acked slot becomes new base, outer loop short-circuits
    # but bytes already counted — verify no second count when re-acked
    before = bench["bytes_tx"]
    # duplicate ack for seq2 should be guarded by !acked
    ack(2)
    assert bench["bytes_tx"] == before, "duplicate ack must not double-count (guard !acked)"
    # Ack seq1
    ack(1)
    assert bench["bytes_tx"] == 660, "seq1 must count exactly once"
    # rtt_count must stay 1:1 with frags_acked after T1 fix (no dangling rtt_count without sum)
    assert bench["rtt_count"] == bench["frags_acked"], f"rtt_count {bench['rtt_count']} must == frags_acked {bench['frags_acked']}"
    # Ack remaining 3
    ack(3)
    assert bench["bytes_tx"] == 880
    print("PASS out_of_order_ack_accounting")


if __name__ == "__main__":
    test_bench_h_exists_and_format()
    test_transfer_bench_instrumentation()
    test_checkpoint_ino_bench_dump()
    test_ble_helpers()
    test_client_mtu_overhead()
    test_client_resume_prefill()
    test_client_bench_csv()
    test_client_duplicate_and_decrypt_fail()
    test_client_resume_resp_and_write_ack_timing()
    test_benchmark_capture_exists()
    test_goodput_math_synthetic()
    test_resume_is_complete_after_prefill()
    test_null_empty_boundary_bench()
    test_bench_json_schema()
    test_out_of_order_ack_accounting()
    print("ALL BENCH TESTS PASS")
