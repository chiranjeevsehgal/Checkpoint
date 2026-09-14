# Firmware

Segregated layout (light reorg 2026-09-02):

```
firmware/
  checkpoint/               # PROD pendant — 16kHz I2S + SD + BLE encrypted sync
    checkpoint.ino          #   Arduino entry (setup: checkpoints 0/1 via recorder_init/ble_init)
    config.h                #   single source of pins/timings (edit here)
    recorder.{cpp,h}        #   I2S + PSRAM ring -> SD .tmp/.wav
    sd_manager.{cpp,h}      #   mount, SD.exists health probe, atomic ops
    manifest.{cpp,h}        #   /rec scan, .tmp/.del recovery, resume next_seq
    ble_service.{cpp,h}     #   NimBLE GATT, handshake crypto_load_or_gen_key
    transfer.{cpp,h}        #   window-4 fragments + ACK + bench
    protocol.{cpp,h}        #   6B header + CRC32
    crypto.{cpp,h}          #   AES-128-CCM, SHA256 KDF, explicit-key AEAD
    auth.{cpp,h}            #   BLE claim key + cloud claim secret (NVS ckauth)
    control.{cpp,h}         #   remote control ops (LED/sync/erase/cloud secret)
    ui.{cpp,h}              #   button debounce, LED states
    power.{cpp,h}           #   mic-off deep sleep, held-button wake
  examples/                 # bring-up sketches (keep folder==.ino per Arduino)
    board_test_rainbow/     #   RGB WS2812 sanity
    mic_test/               #   I2S only, no SD/BLE
    mic_wav_record/         #   I2S + SD SPI, no BLE
    mic_serial_stream/      #   PSRAM buffer -> USB serial WAV dump
    mic_ble_test/           #   fork of checkpoint: I2S+BLE, no SD (MicTest, WINDOW=1)
  host/
    checkpoint_client/      # Python BLE client for checkpoint (moved from firmware/client)
      client_app/            #   protocol v3 client package (ble_client/crypto/ingestion)
      benchmark_capture.py   #   wraps client + serial tail
  tests/                    # host-side grep tests (pytest firmware/tests -v)
  docs/
    UPLOAD_GUIDE.md         #   Arduino IDE upload steps
```

See also: `tools/` (top-level) for `ble_test_capture.py` (MicTest) + `capture_serial_wav.py`, and `hardware/` for wiring.

Arduino IDE: `File -> Open -> firmware/checkpoint/checkpoint.ino` (folder==ino required). Each `examples/` sketch opens standalone.

Host client: `pip install bleak cryptography && python -m client_app --cli --session-token <kratos> --device-id <32hex>` (from `firmware/host/checkpoint_client`).

Serial commands: `auth list | auth forget 0|1 | auth reset | auth export | auth provision | power sleep`. `auth provision` prints `device <32hex>` and `cloud-sha256 <64hex>` for the backend `device-admin provision` import; `auth export` prints the BLE claim key/URI.
