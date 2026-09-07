"""
Benchmark capture — standalone wrapper around CheckpointClient.

Captures BLE goodput / RTT / duplicates per file and optionally tails
firmware Serial BENCH lines into the same csv.

Usage:
    python benchmark_capture.py [device_name_or_address] [--serial COMx] [--duration 300]
    # Writes ./received/benchmark_<ts>.csv + .json + fw_bench_*.csv

Reuses: client.py (CheckpointClient), bench.h header (csv schema)
External: pyserial (optional, for --serial tail), bleak, cryptography
"""
import argparse
import asyncio
import csv
import json
import sys
import time
from pathlib import Path

from client import CheckpointClient, OUTPUT_DIR, BENCH_DIR, find_device

try:
    import serial  # type: ignore
    HAS_SERIAL = True
except Exception:
    HAS_SERIAL = False


async def serial_tail(port: str, out_csv: Path, stop_evt: asyncio.Event):
    # Firmware BENCH removed (error-only Serial). Keep --serial as error-log tail.
    print(f"[serial] firmware BENCH removed; tailing {port} for E/W error lines only")
    if not HAS_SERIAL:
        print(f"[serial] pyserial not installed — pip install pyserial to tail {port}")
        return
    fw_csv = out_csv.with_name(out_csv.stem + "_fw.csv")
    try:
        ser = serial.Serial(port, 115200, timeout=1)
    except Exception as e:
        print(f"[serial] open failed {port}: {e}")
        return
    f = fw_csv.open("w", newline="", encoding="utf-8")
    writer = None
    header_written = False
    buf = ""
    loop = asyncio.get_event_loop()
    while not stop_evt.is_set():
        try:
            chunk = await loop.run_in_executor(None, ser.read, 512)
            if not chunk:
                await asyncio.sleep(0.2)
                continue
            buf += chunk.decode(errors="ignore")
            while "\n" in buf:
                line, buf = buf.split("\n", 1)
                line = line.strip()
                if not line:
                    continue
                if line.startswith("BENCH"):
                    # Legacy firmware BENCH line (pre-removal); keep for old captures.
                    if not header_written and line.startswith("BENCH,src"):
                        writer = csv.writer(f)
                        f.write(line + "\n")
                        header_written = True
                    elif line.startswith("BENCH,fw") or line.startswith("BENCH,client"):
                        if not header_written:
                            f.write("BENCH,src,file_id,total_bytes,total_frags,mtu,frag_size,window,t_start_ms,t_end_ms,bytes_tx,frags_acked,retries,stalls,max_inflight,rtt_sum_ms,rtt_count,goodput_kBps\n")
                            header_written = True
                        f.write(line + "\n")
                        f.flush()
                    else:
                        f.write(f"# {line}\n")
                        f.flush()
                elif line.startswith("E ") or line.startswith("W ") or line.startswith("CK boot") or line.startswith("HELLO") or line.startswith("BLE ") or line.startswith("Checkpoint") or line.startswith("SD ") or line.startswith("REC ") or line.startswith("Transfer") or line.startswith("I2S") or "rst:" in line or "Reset reason" in line:
                    # Enhanced debug: capture HELLO/BLE/SD/REC logs for pairing/MIC debug
                    f.write(f"# {line}\n")
                    f.flush()
                    print(f"[fw] {line}")
                    continue
                # also echo fw lines of interest to stdout
                if line.startswith("E ") or line.startswith("W ") or line.startswith("CK boot"):
                    print(f"[fw] {line}")
                elif line.startswith("HELLO_ACK") or line.startswith("BENCH"):
                    print(f"[fw] {line}")
                elif line.startswith("HELLO") and "recv" in line:
                    print(f"[fw] {line}")
                elif line.startswith("BLE "):
                    print(f"[fw] {line}")
        except Exception as e:
            print(f"[serial] tail error: {e}")
            await asyncio.sleep(1)
    try:
        f.close()
        ser.close()
    except Exception:
        pass
    print(f"[serial] saved {fw_csv}")


async def run(device: str | None, serial_port: str | None, duration: int | None):
    ts = time.strftime("%Y%m%d_%H%M%S")
    out_csv = BENCH_DIR / f"benchmark_{ts}.csv"
    print(f"Benchmark capture -> {out_csv}")
    if serial_port:
        print(f"Serial tail: {serial_port} (optional)")
    address = await find_device(device)
    client = CheckpointClient(address, bench_csv=out_csv)
    await client.connect()
    await client.do_handshake()
    print("\nListening for file transfers. Press Ctrl+C to stop.\n")
    stop_evt = asyncio.Event()
    tasks = []
    if serial_port:
        tasks.append(asyncio.create_task(serial_tail(serial_port, out_csv, stop_evt)))
    try:
        if duration:
            print(f"Running for {duration}s ...")
            await asyncio.sleep(duration)
        else:
            while True:
                await asyncio.sleep(1)
    except KeyboardInterrupt:
        print("\nShutting down...")
    finally:
        stop_evt.set()
        await client.disconnect()
        for t in tasks:
            t.cancel()
            try:
                await t
            except asyncio.CancelledError:
                pass
        if client._csv_file:
            try:
                client._csv_file.close()
            except Exception:
                pass
        rows = client.bench_rows()
        if rows:
            jpath = out_csv.with_suffix(".json")
            try:
                jpath.write_text(json.dumps(rows, indent=2))
                print(f"Saved csv {out_csv} ({len(rows)} files) + json {jpath}")
                # summary to stdout
                for r in rows:
                    print(f"  file {r['file_id']} {r['total_bytes']}B goodput {r['goodput_kBps']} kB/s median {r['median_rtt_ms']}ms p95 {r['p95_rtt_ms']}ms dup {r['duplicates']} crc {r['crc_ok']} resume {r['resume_from']}")
                # also print aggregate for throughput problem doc
                try:
                    kbs = [float(r["goodput_kBps"]) for r in rows]
                    print(f"\nAggregated goodput: min {min(kbs):.1f} max {max(kbs):.1f} avg {sum(kbs)/len(kbs):.1f} kB/s over {len(rows)} files")
                except Exception:
                    pass
            except Exception as e:
                print(f"json save failed: {e}")
        else:
            print(f"No files captured — csv {out_csv} header only. Check connection / manifest pending.")


def main():
    ap = argparse.ArgumentParser(description="Checkpoint benchmark capture")
    ap.add_argument("device", nargs="?", default=None, help="BLE name or address (default Checkpoint)")
    ap.add_argument("--serial", dest="serial_port", default=None, help="Serial port for fw BENCH tail (e.g., COM7)")
    ap.add_argument("--duration", type=int, default=None, help="Auto-stop after N seconds")
    args = ap.parse_args()
    asyncio.run(run(args.device, args.serial_port, args.duration))


if __name__ == "__main__":
    main()
