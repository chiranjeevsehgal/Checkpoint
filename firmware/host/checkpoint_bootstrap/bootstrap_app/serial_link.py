"""Serial port discovery and a threaded reader for the pendant console."""

import queue
import threading

try:
    import serial
    from serial.tools import list_ports
except ImportError:  # reported in the UI instead of failing at import time
    serial = None
    list_ports = None

BAUD = 115200


def serial_available() -> bool:
    return serial is not None


def list_serial_ports() -> list[str]:
    if list_ports is None:
        return []
    return [port.device for port in list_ports.comports()]


class SerialMonitor:
    """Owns one serial port on a reader thread; decoded lines arrive on `lines`."""

    def __init__(self) -> None:
        self.lines: queue.Queue[str] = queue.Queue()
        self._serial = None
        self._thread: threading.Thread | None = None
        self._stop = threading.Event()

    @property
    def is_open(self) -> bool:
        return self._serial is not None

    def open(self, port: str, baud: int = BAUD) -> None:
        if serial is None:
            raise RuntimeError('pyserial is not installed')
        self.close()
        self._serial = serial.Serial(port, baud, timeout=0.2)
        self._stop.clear()
        self._thread = threading.Thread(target=self._read_loop, daemon=True)
        self._thread.start()

    def send(self, command: str) -> None:
        if self._serial is None:
            raise RuntimeError('serial port is not open')
        self._serial.write((command.rstrip('\r\n') + '\n').encode())

    def close(self) -> None:
        self._stop.set()
        thread = self._thread
        if thread is not None and thread.is_alive():
            thread.join(timeout=1.0)
        self._thread = None
        port = self._serial
        self._serial = None
        if port is not None:
            try:
                port.close()
            except Exception:
                pass

    def _read_loop(self) -> None:
        buffer = ''
        while not self._stop.is_set():
            port = self._serial
            if port is None:
                break
            try:
                chunk = port.readline()
            except Exception as error:
                self.lines.put(f'[serial] read error: {error}')
                break
            if not chunk:
                continue
            buffer += chunk.decode(errors='ignore')
            while '\n' in buffer:
                line, buffer = buffer.split('\n', 1)
                self.lines.put(line.rstrip('\r'))
