#pragma once
#include <Arduino.h>

// Shared bench sample — used by firmware Serial dump and client csv join.
// Keep fields sized to avoid String churn on ESP32.
struct BenchSample {
  uint32_t file_id = 0;
  uint32_t total_bytes = 0;
  uint16_t total_frags = 0;
  uint16_t mtu = 0;
  uint16_t frag_size = 0;
  uint8_t window = 0;
  uint32_t t_start_ms = 0;
  uint32_t t_end_ms = 0;
  uint32_t bytes_tx = 0;
  uint32_t frags_acked = 0;
  uint32_t retries = 0;
  uint32_t stalls = 0; // base waited full timeout
  uint32_t max_inflight = 0;
  // RTT tracking for last file — p50/p95 computed client-side from per-frag rtts
  uint32_t rtt_sum_ms = 0;
  uint32_t rtt_count = 0;
};

// One-line csv header for Serial BENCH and client benchmark_capture.py.
// fw_bench, prefix distinguishes firmware vs client rows.
inline void bench_print_header() {
  Serial.println("BENCH,src,file_id,total_bytes,total_frags,mtu,frag_size,window,t_start_ms,t_end_ms,bytes_tx,frags_acked,retries,stalls,max_inflight,rtt_sum_ms,rtt_count,goodput_kBps");
}

// Firmware prints csv data line; client also writes BENCH-prefixed lines to same csv.
// goodput_kBps: if t_end set use t_end-t_start else live millis()-t_start for in-progress trend.
inline void bench_print_sample(const char *src, const BenchSample &s) {
  float goodput = 0.0f;
  uint32_t dt = 0;
  if (s.t_start_ms != 0) {
    if (s.t_end_ms > s.t_start_ms && s.t_end_ms != 0) dt = s.t_end_ms - s.t_start_ms;
    else if (s.t_end_ms == 0) dt = millis() - s.t_start_ms;
    if (dt > 0) goodput = (float)s.bytes_tx * 1000.0f / (float)dt / 1024.0f;
  }
  Serial.printf("BENCH,%s,%lu,%lu,%u,%u,%u,%u,%lu,%lu,%lu,%lu,%lu,%lu,%lu,%lu,%lu,%.2f\n",
                src,
                (unsigned long)s.file_id,
                (unsigned long)s.total_bytes,
                s.total_frags, s.mtu, s.frag_size, s.window,
                (unsigned long)s.t_start_ms,
                (unsigned long)s.t_end_ms,
                (unsigned long)s.bytes_tx,
                (unsigned long)s.frags_acked,
                (unsigned long)s.retries,
                (unsigned long)s.stalls,
                (unsigned long)s.max_inflight,
                (unsigned long)s.rtt_sum_ms,
                (unsigned long)s.rtt_count,
                goodput);
}
