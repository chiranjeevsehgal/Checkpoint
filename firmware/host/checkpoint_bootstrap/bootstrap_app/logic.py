"""Pure helpers for the bootstrap tool: parsers, validators and command builders.

No I/O lives here, so everything is unit-testable without hardware.
"""

import re

COMPILE_UPLOAD_SPEED = 921600
UPLOAD_SPEED = 512000

FQBN_BASE = (
    "esp32:esp32:esp32s3:FlashSize=16M,PartitionScheme=app3M_fat9M_16MB,"
    "PSRAM=opi,CDCOnBoot=default,USBMode=hwcdc,FlashMode=qio"
)
FQBN_COMPILE = f"{FQBN_BASE},UploadSpeed={COMPILE_UPLOAD_SPEED}"

DEVICE_ID_PATTERN = re.compile(r"^[0-9a-f]{32}$")
CLAIM_HASH_PATTERN = re.compile(r"^[0-9a-f]{64}$")

_PROVISION_DEVICE = re.compile(r"^device ([0-9a-f]{32})$", re.MULTILINE)
_PROVISION_HASH = re.compile(r"^cloud-sha256 ([0-9a-f]{64})$", re.MULTILINE)


def to_upload_fqbn(fqbn: str) -> str:
    """Flashing runs slower than compiling for a reliable upload."""
    return re.sub(r"UploadSpeed=\d+", f"UploadSpeed={UPLOAD_SPEED}", fqbn)


def is_device_id(value: str) -> bool:
    return bool(DEVICE_ID_PATTERN.match(value.strip()))


def is_claim_hash(value: str) -> bool:
    return bool(CLAIM_HASH_PATTERN.match(value.strip()))


def parse_provision(text: str) -> tuple[str, str] | None:
    """Return (device_id, cloud_sha256) from `auth provision` output."""
    device = _PROVISION_DEVICE.search(text)
    digest = _PROVISION_HASH.search(text)
    if not device or not digest:
        return None
    return device.group(1), digest.group(1)


def build_compile(arduino_cli: str, fqbn: str, sketch_dir: str, output_dir: str) -> list[str]:
    return [arduino_cli, "compile", "--fqbn", fqbn, sketch_dir, "--output-dir", output_dir]


def build_upload(arduino_cli: str, fqbn: str, port: str, input_dir: str) -> list[str]:
    return [arduino_cli, "upload", "-p", port, "--fqbn", fqbn, "--input-dir", input_dir]


def build_device_admin(args: list[str]) -> list[str]:
    return ["go", "run", "./cmd/device-admin", *args]


def database_url_from_env(values: dict[str, str]) -> str:
    """Compose the host DSN from root .env POSTGRES_* values."""
    user = values.get("POSTGRES_USER", "").strip()
    password = values.get("POSTGRES_PASSWORD", "").strip()
    name = values.get("POSTGRES_DB", "").strip()
    if not user or not name:
        return ""
    credentials = f"{user}:{password}" if password else user
    return f"postgres://{credentials}@localhost:5432/{name}?sslmode=disable"
