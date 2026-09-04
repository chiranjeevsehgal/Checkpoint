#pragma once
#include <Arduino.h>
#include "bench.h"
bool transfer_init();
void transfer_task(void *arg);
void transfer_on_packet(const uint8_t *data, size_t len);
bool transfer_is_busy();
String transfer_current_file();
BenchSample transfer_bench_snapshot();
void transfer_bench_reset();
