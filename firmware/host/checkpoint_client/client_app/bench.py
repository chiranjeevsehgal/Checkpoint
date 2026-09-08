"""Per-file throughput metrics + CSV persistence. Same schema as bench.h."""

import csv
import time
from pathlib import Path

from . import config as cfg


class BenchRecorder:
    def __init__(self, csv_path: Path | None = None, on_event=None):
        self.csv_path = csv_path
        self.on_event = on_event
        self._rows: list[dict] = []
        self._t_send: dict[int, float] = {}
        self._rtts: list[float] = []
        self._file_start: float | None = None
        self._file_meta: dict | None = None
        self._duplicates = 0
        self._decrypt_fail = 0
        self._last_data: float | None = None
        self._csv_writer: csv.DictWriter | None = None
        self._csv_file = None
        if csv_path:
            csv_path.parent.mkdir(parents=True, exist_ok=True)
            self._csv_file = csv_path.open("w", newline="", encoding="utf-8")
            self._csv_writer = csv.DictWriter(self._csv_file, fieldnames=cfg.BENCH_FIELDNAMES)
            self._csv_writer.writeheader()
            self._csv_file.flush()

    @property
    def t_send(self) -> dict[int, float]:
        return self._t_send

    @property
    def duplicates(self) -> int:
        return self._duplicates

    @duplicates.setter
    def duplicates(self, value: int) -> None:
        self._duplicates = value

    @property
    def decrypt_fail(self) -> int:
        return self._decrypt_fail

    @decrypt_fail.setter
    def decrypt_fail(self, value: int) -> None:
        self._decrypt_fail = value

    def reset_file(self, file_id: int, total: int, total_frags: int, resume_from: int) -> None:
        self._file_start = time.perf_counter()
        self._rtts.clear()
        self._t_send.clear()
        self._duplicates = 0
        self._decrypt_fail = 0
        self._last_data = None
        self._file_meta = {"file_id": file_id, "total": total,
                           "total_frags": total_frags, "resume_from": resume_from}

    def note_data_arrival(self) -> None:
        now = time.perf_counter()
        if len(self._rtts) < 5000:
            gap = 0.0 if self._last_data is None else (now - self._last_data) * 1000.0
            self._rtts.append(gap)
            self._last_data = now

    def _percentile(self, sorted_vals: list[float], frac: float) -> float:
        if not sorted_vals:
            return 0.0
        idx = int(len(sorted_vals) * frac)
        return sorted_vals[min(idx, len(sorted_vals) - 1)]

    def finalize(self, crc_ok: bool, total: int, mtu: int, frag_size: int,
                 ingest_upload_id: str = "", ingest_status: str = "",
                 ingest_error: str = "", vad_status: str = "",
                 vad_speech_s: str = "") -> dict | None:
        if not self._file_start or not self._file_meta:
            return None
        elapsed = time.perf_counter() - self._file_start
        goodput = (total / elapsed / 1024.0) if elapsed > 0 else 0.0
        rtts = sorted(self._rtts)
        row = {
            "ts": time.strftime("%Y-%m-%dT%H:%M:%S"),
            "file_id": f"{self._file_meta['file_id']:016x}",
            "total_bytes": total,
            "total_frags": self._file_meta["total_frags"],
            "mtu": mtu or 0,
            "frag_size": frag_size,
            "goodput_kBps": f"{goodput:.2f}",
            "median_rtt_ms": f"{self._percentile(rtts, 0.5):.1f}",
            "p95_rtt_ms": f"{self._percentile(rtts, 0.95):.1f}",
            "duplicates": self._duplicates,
            "retries": 0,
            "decrypt_fail": self._decrypt_fail,
            "crc_ok": crc_ok,
            "resume_from": self._file_meta["resume_from"],
            "elapsed_s": f"{elapsed:.2f}",
            "ingest_upload_id": ingest_upload_id,
            "ingest_status": ingest_status,
            "ingest_error": ingest_error,
            "vad_status": vad_status,
            "vad_speech_s": vad_speech_s,
        }
        self._rows.append(row)
        if self._csv_writer:
            self._csv_writer.writerow(row)
            self._csv_file.flush()
        print(f"BENCH,client,{row['file_id']},{row['total_bytes']},"
              f"{row['total_frags']},{row['mtu']},{row['frag_size']},"
              f"4,0,0,0,0,0,0,0,0,{row['goodput_kBps']}")
        self._file_start = None
        self._file_meta = None
        return row

    def update_ingest(self, file_id_hex: str, upload_id: str, status: str,
                      error: str = "", vad_status: str | None = None,
                      vad_speech_s: str | None = None) -> None:
        for row in self._rows:
            if row.get("file_id") == file_id_hex:
                row["ingest_upload_id"] = upload_id
                row["ingest_status"] = status
                row["ingest_error"] = error[:200] if error else ""
                if vad_status is not None:
                    row["vad_status"] = vad_status
                if vad_speech_s is not None:
                    row["vad_speech_s"] = vad_speech_s
                break
        else:
            return
        if self.on_event is not None:
            try:
                self.on_event({"type": "ingest", "file_id": file_id_hex,
                               "upload_id": upload_id, "ingest_status": status,
                               "ingest_error": error[:200] if error else "",
                               "vad_status": vad_status, "vad_speech_s": vad_speech_s})
            except Exception:
                pass
        if self._csv_file and self._csv_writer:
            try:
                self._csv_file.seek(0)
                self._csv_file.truncate(0)
                self._csv_writer = csv.DictWriter(self._csv_file,
                                                  fieldnames=cfg.BENCH_FIELDNAMES)
                self._csv_writer.writeheader()
                self._csv_writer.writerows(self._rows)
                self._csv_file.flush()
            except Exception as e:
                print(f"  [!] bench csv rewrite failed: {e}")

    def rewrite_csv(self) -> None:
        if not self.csv_path or not self._rows:
            return
        try:
            if self._csv_file:
                try:
                    self._csv_file.close()
                except Exception:
                    pass
                self._csv_file = None
                self._csv_writer = None
            with self.csv_path.open("w", newline="", encoding="utf-8") as f:
                writer = csv.DictWriter(f, fieldnames=cfg.BENCH_FIELDNAMES)
                writer.writeheader()
                writer.writerows(self._rows)
        except Exception as e:
            print(f"  [!] bench csv final rewrite failed: {e}")

    def rows(self) -> list[dict]:
        return list(self._rows)

    def close(self) -> None:
        try:
            if self._csv_file:
                self._csv_file.close()
        except Exception:
            pass
        finally:
            self._csv_file = None
            self._csv_writer = None
