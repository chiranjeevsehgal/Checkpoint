"""ttkbootstrap window: flash firmware, drive the serial console, register devices."""

import queue
import shutil
import threading
import tkinter as tk
from tkinter import filedialog, messagebox, scrolledtext
from typing import Callable

import ttkbootstrap as ttk

from . import logic
from .device_admin import find_repo_root, load_env_file, run_device_admin, stream_command
from .logic import database_url_from_env, is_claim_hash, is_device_id
from .serial_link import SerialMonitor, list_serial_ports, serial_available

POLL_MS = 100
BAUDRATE = 115200
READ_IDS_DELAY_MS = 1500

QUICK_COMMANDS = (
    ('auth list', 'List'),
    ('auth export', 'Export'),
    ('auth provision', 'Provision'),
    ('auth reset', 'Reset'),
)


class BootstrapWindow:
    def __init__(self, root: ttk.Window) -> None:
        self.root = root
        self.repo_root = find_repo_root()
        self.monitor = SerialMonitor()
        self.messages: queue.Queue[tuple[str, str]] = queue.Queue()
        self._busy = False
        self._collecting = False
        self._collected: list[str] = []
        self._action_buttons: list[ttk.Button] = []

        self._build_variables()
        self._build_widgets()
        self.root.protocol('WM_DELETE_WINDOW', self._on_close)
        self.root.after(POLL_MS, self._drain)

    def _build_variables(self) -> None:
        repo = self.repo_root
        env_values = load_env_file(repo / '.env') if repo else {}
        self.arduino_var = tk.StringVar(value=shutil.which('arduino-cli') or '')
        self.port_var = tk.StringVar()
        self.sketch_var = tk.StringVar(value=str(repo / 'firmware' / 'checkpoint') if repo else '')
        self.build_var = tk.StringVar(
            value=str(repo / 'firmware' / 'checkpoint' / 'build') if repo else ''
        )
        self.fqbn_var = tk.StringVar(value=logic.FQBN_COMPILE)
        self.db_var = tk.StringVar(value=database_url_from_env(env_values))
        self.device_var = tk.StringVar()
        self.hash_var = tk.StringVar()
        self.slot_var = tk.StringVar(value='0')
        self.command_var = tk.StringVar()

    def _build_widgets(self) -> None:
        self.root.title('Checkpoint Bootstrap')
        self.root.minsize(820, 720)
        outer = ttk.Frame(self.root, padding=12)
        outer.pack(fill='both', expand=True)
        outer.columnconfigure(0, weight=1)
        outer.rowconfigure(4, weight=1)

        self._build_toolchain(outer)
        self._build_firmware(outer)
        self._build_serial(outer)
        self._build_registration(outer)
        self._build_log(outer)

    def _build_toolchain(self, outer: ttk.Frame) -> None:
        frame = ttk.Labelframe(outer, text='Toolchain', padding=10)
        frame.grid(row=0, column=0, sticky='ew')
        frame.columnconfigure(1, weight=1)
        self._field(frame, 0, 'arduino-cli', self.arduino_var, self._pick_arduino)
        ttk.Button(frame, text='Refresh ports', command=self._refresh_ports).grid(
            row=1, column=1, sticky='w', pady=4
        )
        self.port_combo = ttk.Combobox(frame, textvariable=self.port_var, values=[])
        ttk.Label(frame, text='COM port').grid(row=2, column=0, sticky='w', padx=(0, 8), pady=4)
        self.port_combo.grid(row=2, column=1, sticky='ew', pady=4)
        self._refresh_ports()

    def _build_firmware(self, outer: ttk.Frame) -> None:
        frame = ttk.Labelframe(outer, text='Firmware', padding=10)
        frame.grid(row=1, column=0, sticky='ew', pady=(10, 0))
        frame.columnconfigure(1, weight=1)
        self._field(frame, 0, 'Sketch directory', self.sketch_var, self._pick_sketch_directory)
        self._field(frame, 1, 'Build directory', self.build_var, self._pick_build_directory)
        self._field(frame, 2, 'FQBN', self.fqbn_var)

        buttons = ttk.Frame(frame)
        buttons.grid(row=3, column=0, columnspan=3, sticky='w', pady=(8, 0))
        self._action_button(buttons, 'Compile', lambda: self._run_firmware('compile'))
        self._action_button(
            buttons, 'Upload build dir', lambda: self._run_firmware('upload'), 'secondary-outline'
        )
        self._action_button(
            buttons, 'Compile + upload', lambda: self._run_firmware('compile-upload'), 'success'
        )

    def _build_serial(self, outer: ttk.Frame) -> None:
        frame = ttk.Labelframe(outer, text='Serial console', padding=10)
        frame.grid(row=2, column=0, sticky='ew', pady=(10, 0))
        frame.columnconfigure(0, weight=1)

        controls = ttk.Frame(frame)
        controls.grid(row=0, column=0, sticky='w')
        self.serial_button = ttk.Button(controls, text='Open', command=self._toggle_serial)
        self.serial_button.pack(side='left')
        for command, label in QUICK_COMMANDS:
            self._action_button(controls, label, lambda c=command: self._send(c), 'secondary-outline')
        ttk.Label(controls, text='slot').pack(side='left', padx=(8, 4))
        ttk.Combobox(controls, textvariable=self.slot_var, values=['0', '1'], width=3).pack(
            side='left'
        )
        self._action_button(
            controls,
            'Forget',
            lambda: self._send(f'auth forget {self.slot_var.get()}'),
            'secondary-outline',
        )
        self._action_button(controls, 'Sleep', self._power_sleep, 'warning-outline')

        entry_row = ttk.Frame(frame)
        entry_row.grid(row=1, column=0, sticky='ew', pady=(8, 0))
        entry_row.columnconfigure(0, weight=1)
        ttk.Entry(entry_row, textvariable=self.command_var).grid(row=0, column=0, sticky='ew')
        send_button = ttk.Button(entry_row, text='Send', command=self._send_free)
        send_button.grid(row=0, column=1, padx=(8, 0))
        self._action_buttons.append(send_button)
        self._sync_serial_buttons()

    def _build_registration(self, outer: ttk.Frame) -> None:
        frame = ttk.Labelframe(outer, text='Cloud registration', padding=10)
        frame.grid(row=3, column=0, sticky='ew', pady=(10, 0))
        frame.columnconfigure(1, weight=1)
        self._field(frame, 0, 'DATABASE_URL', self.db_var)
        self._field(frame, 1, 'Device id', self.device_var)
        self._field(frame, 2, 'Claim hash', self.hash_var)

        buttons = ttk.Frame(frame)
        buttons.grid(row=3, column=0, columnspan=3, sticky='w', pady=(8, 0))
        self._action_button(buttons, 'Read IDs', self._read_ids, 'secondary-outline')
        self._action_button(buttons, 'Register', self._register, 'success')
        self._action_button(buttons, 'Status', self._status, 'secondary-outline')

    def _build_log(self, outer: ttk.Frame) -> None:
        frame = ttk.Labelframe(outer, text='Log', padding=10)
        frame.grid(row=4, column=0, sticky='nsew', pady=(10, 0))
        frame.columnconfigure(0, weight=1)
        frame.rowconfigure(0, weight=1)
        self.log = scrolledtext.ScrolledText(frame, height=12, state='disabled', wrap='none')
        self.log.grid(row=0, column=0, sticky='nsew')
        ttk.Button(frame, text='Clear', bootstyle='secondary-outline', command=self._clear_log).grid(
            row=1, column=0, sticky='e', pady=(6, 0)
        )

    def _field(
        self,
        parent: ttk.Frame,
        row: int,
        label: str,
        variable: tk.StringVar,
        browse: Callable[[], None] | None = None,
    ) -> ttk.Entry:
        ttk.Label(parent, text=label).grid(row=row, column=0, sticky='w', padx=(0, 8), pady=4)
        entry = ttk.Entry(parent, textvariable=variable)
        entry.grid(row=row, column=1, sticky='ew', pady=4)
        if browse is not None:
            ttk.Button(parent, text='Browse…', bootstyle='secondary-outline', command=browse).grid(
                row=row, column=2, padx=(8, 0), pady=4
            )
        return entry

    def _action_button(
        self,
        parent: tk.Widget,
        text: str,
        command: Callable[[], None],
        style: str | None = None,
    ) -> ttk.Button:
        buttons = {'bootstyle': style} if style else {}
        button = ttk.Button(parent, text=text, command=command, **buttons)
        button.pack(side='left', padx=(0, 8))
        self._action_buttons.append(button)
        return button

    # -- picks -----------------------------------------------------------------

    def _pick_arduino(self) -> None:
        path = filedialog.askopenfilename(title='Select arduino-cli executable')
        if path:
            self.arduino_var.set(path)

    def _pick_sketch_directory(self) -> None:
        self._pick_directory(self.sketch_var)

    def _pick_build_directory(self) -> None:
        self._pick_directory(self.build_var)

    def _pick_directory(self, variable: tk.StringVar) -> None:
        path = filedialog.askdirectory()
        if path:
            variable.set(path)

    # -- serial ----------------------------------------------------------------

    def _refresh_ports(self) -> None:
        if not serial_available():
            self._log('pyserial is not installed — run pip install -r requirements.txt')
            return
        ports = list_serial_ports()
        self.port_combo.configure(values=ports)
        if ports and not self.port_var.get().strip():
            self.port_var.set(ports[0])

    def _toggle_serial(self) -> None:
        if self.monitor.is_open:
            self.monitor.close()
            self._log('Serial closed')
        else:
            port = self.port_var.get().strip()
            if not port:
                self._log('Select a COM port first')
                return
            try:
                self.monitor.open(port, BAUDRATE)
            except Exception as error:
                self._log(f'Serial open failed: {error}')
                return
            self._log(f'Serial {port} open at {BAUDRATE}')
        self._sync_serial_buttons()

    def _sync_serial_buttons(self) -> None:
        self.serial_button.configure(text='Close' if self.monitor.is_open else 'Open')

    def _ensure_serial(self) -> bool:
        if self.monitor.is_open:
            return True
        self._log('Open the serial port first')
        return False

    def _send(self, command: str) -> None:
        if not self._ensure_serial():
            return
        try:
            self.monitor.send(command)
        except Exception as error:
            self._log(f'Send failed: {error}')
            return
        self._log(f'> {command}')

    def _send_free(self) -> None:
        command = self.command_var.get().strip()
        if not command:
            return
        self._send(command)
        self.command_var.set('')

    def _power_sleep(self) -> None:
        if not self._ensure_serial():
            return
        if messagebox.askyesno('Power sleep', 'The pendant stops USB output until woken. Continue?'):
            self._send('power sleep')

    # -- firmware --------------------------------------------------------------

    def _run_firmware(self, action: str) -> None:
        arduino_cli = self.arduino_var.get().strip()
        fqbn = self.fqbn_var.get().strip()
        sketch = self.sketch_var.get().strip()
        build = self.build_var.get().strip()
        port = self.port_var.get().strip()
        if not arduino_cli:
            self._log('Set the arduino-cli path first')
            return
        if action in ('compile', 'compile-upload') and not sketch:
            self._log('Set the sketch directory first')
            return
        if action in ('upload', 'compile-upload') and (not port or not build):
            self._log('Select a COM port and build directory first')
            return
        if self.monitor.is_open:
            self.monitor.close()
            self._sync_serial_buttons()
            self._log('Serial closed for flashing')

        def work() -> None:
            if action in ('compile', 'compile-upload'):
                self._stream(logic.build_compile(arduino_cli, fqbn, sketch, build), 'compile')
            if action in ('upload', 'compile-upload'):
                upload_fqbn = logic.to_upload_fqbn(fqbn)
                self._stream(logic.build_upload(arduino_cli, upload_fqbn, port, build), 'upload')

        self._run_worker(action, work)

    def _stream(self, command: list[str], label: str) -> None:
        self._post_log(f'$ {" ".join(command)}')
        try:
            code = stream_command(command, self._post_log)
        except FileNotFoundError:
            self._post_log(f'{label}: executable not found: {command[0]}')
            return
        self._post_log(f'{label}: exit {code}')

    # -- registration ----------------------------------------------------------

    def _read_ids(self) -> None:
        if not self._ensure_serial():
            return
        self._collected = []
        self._collecting = True
        self._send('auth provision')
        self.root.after(READ_IDS_DELAY_MS, self._collect_done)

    def _collect_done(self) -> None:
        self._collecting = False
        parsed = logic.parse_provision('\n'.join(self._collected))
        if parsed is None:
            self._log('Read IDs: no device/cloud-sha256 in the response')
            return
        device, digest = parsed
        self.device_var.set(device)
        self.hash_var.set(digest)
        self._log(f'Read IDs: device {device}')

    def _register(self) -> None:
        self._run_device_admin('provision', self._provision_args)

    def _status(self) -> None:
        self._run_device_admin('status', self._status_args)

    def _provision_args(self, device: str) -> list[str]:
        return ['provision', '-device', device, '-claim-hash', self.hash_var.get().strip()]

    def _status_args(self, device: str) -> list[str]:
        return ['status', '-device', device]

    def _run_device_admin(
        self, name: str, build_args: Callable[[str], list[str]]
    ) -> None:
        repo = self.repo_root
        device = self.device_var.get().strip()
        db_url = self.db_var.get().strip()
        if repo is None:
            self._log('Could not locate the repository root')
            return
        if not is_device_id(device):
            self._log('Device id must be 32 lowercase hex characters')
            return
        if name == 'provision' and not is_claim_hash(self.hash_var.get().strip()):
            self._log('Claim hash must be 64 lowercase hex characters')
            return
        if not db_url:
            self._log('Set DATABASE_URL first')
            return

        def work() -> None:
            code = run_device_admin(repo, build_args(device), db_url, self._post_log)
            self._post_log(f'{name}: exit {code}')

        self._run_worker(name, work)

    # -- plumbing --------------------------------------------------------------

    def _run_worker(self, name: str, work: Callable[[], None]) -> None:
        if self._busy:
            self._log('Another task is still running')
            return
        self._set_busy(True)

        def run() -> None:
            try:
                work()
            except Exception as error:
                self.messages.put(('log', f'{name} failed: {error}'))
            finally:
                self.messages.put(('done', name))

        threading.Thread(target=run, daemon=True).start()

    def _set_busy(self, busy: bool) -> None:
        self._busy = busy
        state = 'disabled' if busy else 'normal'
        for button in self._action_buttons:
            button.configure(state=state)

    def _post_log(self, line: str) -> None:
        self.messages.put(('log', line))

    def _log(self, text: str) -> None:
        self.log.configure(state='normal')
        self.log.insert('end', f'{text}\n')
        self.log.see('end')
        self.log.configure(state='disabled')

    def _clear_log(self) -> None:
        self.log.configure(state='normal')
        self.log.delete('1.0', 'end')
        self.log.configure(state='disabled')

    def _drain(self) -> None:
        self._drain_messages()
        self._drain_serial()
        self.root.after(POLL_MS, self._drain)

    def _drain_messages(self) -> None:
        while True:
            try:
                tag, payload = self.messages.get_nowait()
            except queue.Empty:
                return
            if tag == 'log':
                self._log(payload)
            elif tag == 'done':
                self._set_busy(False)

    def _drain_serial(self) -> None:
        while True:
            try:
                line = self.monitor.lines.get_nowait()
            except queue.Empty:
                return
            if self._collecting:
                self._collected.append(line)
            self._log(line)

    def _on_close(self) -> None:
        self.monitor.close()
        self.root.destroy()


def main() -> None:
    root = ttk.Window(themename='flatly')
    BootstrapWindow(root)
    root.mainloop()


if __name__ == '__main__':
    main()
