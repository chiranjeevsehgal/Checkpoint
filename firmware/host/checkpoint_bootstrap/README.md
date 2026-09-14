# Checkpoint Bootstrap

Small desktop app for bench work: flash the pendant firmware, drive the USB
serial console, and register the pendant in the backend. Reuses `arduino-cli`,
`pyserial`, and the `device-admin` Go CLI (no duplicated provisioning logic).

## Install

```powershell
cd firmware/host/checkpoint_bootstrap
pip install -r requirements.txt
```

Requires Python 3.10+, `arduino-cli` with the ESP32 core/libraries, and Go for
device registration.

## Run

```powershell
python -m bootstrap_app
```

## What it does

- **Firmware** — pick the `arduino-cli` path, COM port, sketch dir, build dir
  and FQBN. `Compile` writes binaries to the build dir, `Upload build dir`
  flashes prebuilt binaries, `Compile + upload` does both. Uploads use the
  slower, more reliable upload speed; the serial console is closed first.
- **Serial console** — open the port at 115200 and use the quick buttons
  (`auth list`, `auth export`, `auth provision`, `auth reset`, `auth forget 0|1`,
  `power sleep`) or send any command.
- **Cloud registration** — `Read IDs` parses `auth provision` from the serial
  response into the device id and claim hash, then `Register` runs
  `device-admin provision`. `Status` is read-only. `DATABASE_URL` is prefilled
  from the repo `.env` when present.

`auth export` prints the BLE claim key, which is a secret — it is shown in the
log but never written to disk.

## Tests

Pure logic is covered without hardware:

```powershell
pytest firmware/tests/test_bootstrap_logic.py -v
```
