# Upload the Pendant Firmware

You have a `firmware/checkpoint` folder. It works with Arduino IDE. No extra toolchain needed.

## What you need

* Arduino IDE 2.x
* ESP32 board package 3.x
* One `ESP32-S3-WROOM-1-N16R8` board wired per `HW/CONNECTIONS.md`
* One `3V Only` microSD breakout with a genuine `32 GB` card
* One USB-C data cable

## Install the board

1. Open Arduino IDE.
2. Go to File, Preferences, Additional Boards Manager URLs. Add `https://espressif.github.io/arduino-esp32/package_esp32_index.json`.
3. Open Tools, Board, Boards Manager. Search `esp32`. Install `esp32 by Espressif`.
4. Restart IDE.

## Install libraries

1. Open Sketch, Include Library, Manage Libraries.
2. Search and install `NimBLE-Arduino` by `h2zero`.
3. Search and install `ArduinoJson` by `Benoit Blanchon` (6.x).
4. Search and install `Adafruit NeoPixel` by `Adafruit` (onboard WS2812 status LED on GPIO48).

`SD`, `SPI`, `mbedtls` come with the ESP32 package. You do not install them.

> RGB note: status LED is the onboard WS2812 on `GPIO48` (`HW_RGB_PIN`, v1.1 clones use 38).
> If it stays dark, bridge the RGB solder pads on the back near the LED (many N16R8 clones ship open).

## Open the project

1. Double click `firmware/checkpoint/checkpoint.ino`. IDE opens all tabs.
2. Keep the folder name `checkpoint`. Arduino needs the folder and the `.ino` to share the name.

## Pick the board

In Tools set:

* Board: `ESP32S3 Dev Module`
* USB CDC On Boot: `Enabled`
* USB Mode: `Hardware CDC and JTAG`
* Flash Size: `16MB (128Mb)`
* Partition Scheme: `16M Flash (3MB APP/9.9MB FATFS)` or `Default 16MB` if that is missing
* PSRAM: `OPI PSRAM`
* PSRAM Mode: `Enabled`
* Flash Mode: `QIO 80MHz`
* Upload Speed: `921600`
* Upload Mode: `UART0 / Hardware CDC`

Your `N16R8` needs `OPI PSRAM` on. Without it, the pendant reboots.

## Wire check before you power

* `INMP441 VDD` to `3V3`, not `5V`.
* `microSD 3V` to `3V3`, not `5V`.
* `L/R` to `GND`.
* Wiring per `HW/CONNECTIONS.md`. Match `GPIO4 WS`, `GPIO5 SCK`, `GPIO6 SD`, `GPIO10-13` SPI, `GPIO1` button, `GPIO2` LED with `1k`. `GPIO7 DET` only for Rev B 13-pin board (`HW_HAS_SD_DETECT 1`); 8-pin variant (`HW_HAS_SD_DETECT 0`) leaves `GPIO7` NC.
* Check with a DMM. Look for shorts between `3V3` and `GND`.

## Upload

1. Plug the dev board with the `USB-UART` port (check silkscreen; some clones label the two USB-C ports).
2. Select the port in Tools, Port.
3. Click Upload.
4. Watch the console at `115200` baud. You should see:

```
Checkpoint Rev B
SD mounted
Pending files: 0
Recorder started, 1-min chunks
```

* No SD message means wiring or card issue. Reseat the card. Check `CONNECTIONS.md`. On 8-pin board (no DET, `HW_HAS_SD_DETECT 0`) wait 2 s for mount-health poll; check `SD mounted` vs `SD mount failed` at 115200 baud.
* `I2S init failed` means `GPIO4-6` conflict. Check `HW/HARDWARE.md` §5 reserved pins `26-37` and `19/20`.

## Test the flow

1. Speak for two minutes. You get two files in `/rec` like `REC_000001_0000.wav` (boot_id + sequence; survives reboot).
2. Pull the card and play a clip on your computer. Speech should be clear. No clicks.
3. Insert the card. The pendant continues. No reboot.
4. Press the button. You get a `bookmark` blink on the LED.
5. Open your BLE test app. Connect to `Checkpoint`. The pendant runs a handshake
   (HELLO → HELLO_ACK → host READY, then transfers start). Then it sends one file
   at a time in `220`-byte fragments with sequence numbers. It waits for cumulative
   `ACK`s, retries with backoff, resumes real partial progress from the host's
   `.part` files, and deletes a file only after it gets `FILE_DONE_ACK`.
   Requires negotiated MTU ≥ 241; the app refuses smaller MTUs instead of corrupting.
6. Pull power mid-record. Reboot. The pendant keeps old files. The open `.tmp` either promotes or drops if too small. It starts a fresh chunk.

## BLE pairing

The pendant uses `NimBLE` Secure Connections with bonding and Just Works pairing
(no passkey display; pendant has LED only) with MITM off — no display for
numeric comparison; previously bonded phones need one re-pair after this change.
On first boot it creates a 16-byte master key and stores it in `NVS`
(`checkpoint/ccmmaster`). `HELLO_ACK` sends a per-session key derived from it
(the master itself never leaves the device). App derives per-file keys with
RFC 5869 HKDF-SHA256. Fragments use `AES-128-CCM` with `8-byte` tag. Nonce is `session || file || seq`. You do not need to configure keys. To rotate, erase NVS: hold `BOOT` on flash or call `Preferences.clear`.

The recorder keeps a 96 kB PSRAM ring. Short SD stalls do not drop audio. The manifest keeps `next_seq` in `/rec/manifest.json` so resume survives reboot. Recovered `.tmp` files get a patched WAV header.

## Common fixes

* Upload fails — hold `BOOT`, press `RESET`, release `BOOT`, upload again. Or swap to the other USB-C port.
* SD not found — lower `SD_SPI_FREQ_KHZ` in `config.h` from `10000` to `4000`.
* Silent recordings — check `CHIPEN` on your `INMP441` breakout. It must sit high.
* Resets on battery — measure charger `5V` before you connect to `ESP32 5V`. Keep only one power source live per `HW/HARDWARE.md` §14.3.
* Slow BLE — move the pendant close to the phone. Keep `BLE_FRAG_SIZE` at `220`. Change it only if you retest.

## Change settings

Open `config.h`. You get all tunables in one place.

* `REC_CHUNK_SEC` — `60` gives `1 min` files.
* `BLE_FRAG_SIZE` — `220` fits MTU `247`.
* `BLE_WINDOW` — `4` gives throughput; set `1` for max stability.
* `BLE_ACK_TIMEOUT_MS` — `800`.
* `CRYPTO_TAG_BYTES` — `8`.

Edit, save, upload again.

## Keep it simple

This build avoids extras. You get record, store, handshake, fragment, ack, retry, resume, delete on ack, and keep recording while you send. Wi-Fi stays off except for future OTA. Add features only when you need them.
