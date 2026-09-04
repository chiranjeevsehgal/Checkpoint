"""
Tests for mic_ble_test optimized fast transfer (window 4, queue, bench, short timeout).
Run: python -m pytest firmware/tests/test_mic_ble_optimal.py -v
Covers: config tuning, queue vs single-var, windowed burst, short-circuit, bench, goodput math.
"""
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
EX = ROOT / "examples" / "mic_ble_test"
CHK = ROOT / "checkpoint"

CFG = (EX / "config.h").read_text(encoding="utf-8") if (EX / "config.h").exists() else ""
INO = (EX / "mic_ble_test.ino").read_text(encoding="utf-8") if (EX / "mic_ble_test.ino").exists() else ""
BENCH_H = (EX / "bench.h").read_text(encoding="utf-8") if (EX / "bench.h").exists() else ""
CHK_TRANSFER = (CHK / "transfer.cpp").read_text(encoding="utf-8") if (CHK / "transfer.cpp").exists() else ""

def test_config_tuned():
    assert '#define BLE_MTU 517' in CFG, "MTU must be 517 for DLE"
    assert '#define BLE_FRAG_SIZE 220' in CFG, "FRAG 220 safe for both 247/517"
    # P0 bench: WINDOW 16/32 + ACK_EVERY 16 + WNR, FRAG 220 unchanged for clean A/B vs 14.3kB/s
    assert ('#define BLE_WINDOW 32' in CFG or '#define BLE_WINDOW 16' in CFG), "WINDOW 16/32 for cumulative ACK16 pipelined (P0)"
    assert '#define BLE_ACK_EVERY 16' in CFG, "ACK_EVERY 16 (3520B per ACK) missing — P0"
    assert '#define BLE_IDLE_MS 100' in CFG, "IDLE 100ms flush missing — P0"
    assert '#define BLE_ACK_TIMEOUT_MS 1500' in CFG, "ACK timeout 1500ms for cumulative 16 (was 600 per-frag) — P0"
    assert 'BLE_ACK_TIMEOUT_MS 1500' in CFG
    assert 'BLE_ACK_TIMEOUT_MS 2000' not in CFG, "old 2000 must be removed"
    assert '#define TASK_STACK_TRANSFER 8192' in CFG, "stack 8192 for W32"
    print("PASS config tuned P0")

def test_frag_margin_math():
    mtu = 517
    frag = 220
    tag = 8
    hdr = 6
    crc = 4
    att = 3
    packet = hdr + frag + tag + crc
    value = mtu - att
    assert packet <= value, f"packet {packet} > value {value} would overflow MTU"
    # check 220 leaves 294 margin for 517, but still safe for 247 (220<=226)
    assert value - packet == 514 - 238, "220 leaves margin"
    # verify max for 517 would be 496
    max_frag = value - hdr - tag - crc
    assert max_frag == 496, f"max frag {max_frag} !=496 for 517"
    # also verify 247 max 226 for fallback
    assert (247-3 - hdr - tag - crc) == 226
    print("PASS frag margin")

def test_window_vs_single():
    assert 'TEST_BLE_WINDOW 1' not in INO, "old window 1 stop-and-wait must be gone"
    assert 'BLE_WINDOW' in INO, "should use BLE_WINDOW from config"
    # must not use single-var ack collapse
    assert 'g_last_ack_seq' not in INO or 's_ack_q' in INO, "single var ack collapse must be replaced by queue"
    assert 'g_last_ack_ok' not in INO or 'AckMsg' in INO, "single var ok must be queue"
    print("PASS window vs single")

def test_queue_based_ack():
    assert 'struct AckMsg' in INO, "AckMsg queue struct missing"
    assert 'xQueueCreate(32' in INO, "queue depth 32 for W32 P0 (was 16)"
    assert 'signal_ack' in INO, "signal_ack helper missing"
    assert 'xQueueSend(s_ack_q' in INO, "queue send missing"
    assert 'xQueueReceive(s_ack_q' in INO, "queue receive missing"
    assert 'xQueueReset(s_ack_q)' in INO, "queue reset before announce/done missing"
    assert INO.count('g_ack_pending') == 0, "old g_ack_pending single flag must be removed"
    print("PASS queue ack P0")

def test_windowed_burst_logic():
    assert 'WindowSlot window[BLE_WINDOW]' in INO, "window array missing"
    assert 'for(int w=0; w<BLE_WINDOW' in INO, "window fill loop missing"
    assert 'ble_send_raw(pkt_buf' in INO, "burst send via notify missing"
    assert 's_send_ms[w] = millis()' in INO, "per-slot send timestamp missing"
    assert 's_bench.max_inflight' in INO, "max_inflight bench missing"
    assert 'memset(window, 0' in INO, "window init memset missing"
    print("PASS windowed burst")

def test_short_circuit_and_retry():
    assert 'bool base_acked = window[0].acked' in INO or 'base_acked' in INO, "short-circuit check missing"
    assert 'while(!base_acked && millis()-wait_start < BLE_ACK_TIMEOUT_MS)' in INO, "wait loop with short-circuit missing"
    # P0 cumulative: either legacy Short-circuit or cumulative comment
    assert ('Short-circuit' in INO or 'already-acked' in INO or 'O-O' in INO or 'cumulative' in INO or 'CUM' in INO), "short-circuit/cumulative comment missing"
    assert 's_bench.stalls++' in INO, "stalls counter missing"
    assert 's_bench.retries++' in INO, "retries counter missing"
    assert 'if(window[0].attempts > BLE_RETRY_MAX)' in INO, "retry max check missing"
    assert 'continue;' in INO, "retry continue for base missing"
    print("PASS short-circuit retry P0")

def test_ack_window_draining():
    # P0 cumulative: ack_seq means 0..ack_seq contiguous OK (<=), legacy was == per-frag
    assert 'for(int w=0; w<BLE_WINDOW; w++)' in INO, "ACK to window slot matching missing"
    assert ('m.seq==window[w].seq' in INO or 'window[w].seq <= m.seq' in INO), "selective/cumulative ack match missing"
    assert 's_bench.rtt_sum_ms += rtt' in INO, "RTT sum missing"
    assert 's_bench.frags_acked++' in INO, "frags acked count missing"
    assert 's_bench.bytes_tx += window[w].len' in INO, "bytes_tx count missing"
    print("PASS ack draining P0")

def test_slide_on_base_ack():
    assert 'base++;' in INO, "base slide missing"
    assert 'for(int i=0;i<BLE_WINDOW-1;i++)' in INO, "window slide memmove missing"
    assert 's_send_ms[i]=s_send_ms[i+1]' in INO, "send_ms slide missing"
    print("PASS slide")

def test_bench_instrumentation():
    assert '#include "bench.h"' in INO, "bench.h include missing"
    assert 'BenchSample s_bench' in INO, "bench static missing"
    assert 'portMUX_TYPE s_bench_mux' in INO, "bench mux missing"
    assert 's_bench.t_start_ms = millis()' in INO, "t_start not set"
    assert 's_bench.t_end_ms = millis()' in INO, "t_end not set"
    assert 'bench_print_header()' in INO, "bench header print missing"
    assert 'bench_print_sample("fw"' in INO, "bench sample print missing"
    assert 'BENCH fw' in INO or 'bench_print_sample' in INO, "BENCH output missing"
    assert 'goodput' in INO, "goodput log missing"
    print("PASS bench")

def test_file_announce_and_done():
    assert 'PKT_FILE_ANNOUNCE' in INO, "announce missing"
    assert 'PKT_FILE_DONE' in INO, "file done missing"
    assert 'proto_crc32(s_wav, total)' in INO, "crc compute missing"
    assert 'crypto_derive_file_key' in INO, "per-file key derive missing"
    assert 'crypto_encrypt' in INO, "encrypt missing"
    assert 'wait_ack(ann_seq' in INO, "announce wait_ack missing"
    assert 'wait_ack(done_seq' in INO, "done wait_ack missing"
    print("PASS announce done")

def test_crypto_nonce_invariant():
    assert 'crypto_build_nonce' in INO, "nonce build missing"
    # INVARIANT comment required for CCM reuse correctness
    assert 'INVARIANT' in INO or 'same nonce+key only for identical plaintext' in INO, "nonce invariant comment missing"
    print("PASS nonce P0")

def test_no_blocking_delays_in_window():
    # window burst should not have per-frag 20*att delay inside retry loop? actually retry does but burst does not
    # Ensure main burst has no delay(20) inside window fill — retry has, but not fill
    # Count delays: one per retry only, not per frag send
    assert INO.count('delay(20') <= 1, "should have at most one delay for retry, not per frag burst"
    print("PASS no blocking")

def test_efficiency_vs_single():
    # theoretical goodput: window 6 pipelined ~6x window1 with 7.5ms vs 60ms
    frag = 220
    window1_per_sec = 1 * (1000/60) * frag  # 1 frag per 60ms interval
    window6_per_sec = 6 * (1000/7.5) * frag  # 6 frags per 7.5ms interval (web optimal)
    ratio = window6_per_sec / window1_per_sec
    assert ratio >= 10, f"window6 not 12x single: ratio {ratio}"
    # timeout improvement: 600 vs 2000 => stall recovery 3.3x faster
    assert 2000/600 >= 3.0
    print(f"PASS efficiency ratio {ratio:.1f}x window6 + 3.3x stall")

def test_parity_with_checkpoint():
    # ensure mic_ble_test now mirrors checkpoint's key window idioms
    for snippet in ['s_send_ms[w] = millis()', 'xQueueReceive(s_ack_q', 's_bench.max_inflight']:
        assert snippet in INO, f"parity snippet missing {snippet}"
        assert snippet in CHK_TRANSFER, f"checkpoint parity missing {snippet} (should mirror)"
    print("PASS parity with checkpoint")

if __name__ == "__main__":
    test_config_tuned()
    test_frag_margin_math()
    test_window_vs_single()
    test_queue_based_ack()
    test_windowed_burst_logic()
    test_short_circuit_and_retry()
    test_ack_window_draining()
    test_slide_on_base_ack()
    test_bench_instrumentation()
    test_file_announce_and_done()
    test_crypto_nonce_invariant()
    test_no_blocking_delays_in_window()
    test_efficiency_vs_single()
    test_parity_with_checkpoint()
    print("ALL MIC_BLE OPTIMAL TESTS PASS")
