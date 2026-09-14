"""
Checks for the Checkpoint bootstrap tool's pure logic.
Run: python -m pytest firmware/tests/test_bootstrap_logic.py -v
No hardware, serial, or GUI needed.
"""
import sys
from pathlib import Path

HOST = Path(__file__).resolve().parent.parent / "host" / "checkpoint_bootstrap"
sys.path.insert(0, str(HOST))

DEVICE = "474f8bcaff162d3f18fbda36a168f201"
HASH = "88bccb336643dcfce900e55b1e934a2d821d3319e952114e51ebe6fde1210661"


def test_parse_provision_roundtrip():
    from bootstrap_app.logic import parse_provision

    text = f"device {DEVICE}\ncloud-sha256 {HASH}\n"
    assert parse_provision(text) == (DEVICE, HASH)


def test_parse_provision_returns_none_when_incomplete():
    from bootstrap_app.logic import parse_provision

    assert parse_provision(f"device {DEVICE}\n") is None
    assert parse_provision("") is None


def test_id_validators_reject_bad_shapes():
    from bootstrap_app.logic import is_claim_hash, is_device_id

    assert is_device_id(DEVICE)
    assert not is_device_id(DEVICE.upper())
    assert not is_device_id(DEVICE[:-1])

    assert is_claim_hash(HASH)
    assert not is_claim_hash(HASH.upper())
    assert not is_claim_hash(HASH[:-1])


def test_upload_fqbn_only_changes_speed():
    from bootstrap_app.logic import FQBN_COMPILE, to_upload_fqbn

    upload = to_upload_fqbn(FQBN_COMPILE)
    assert "UploadSpeed=512000" in upload
    assert "UploadSpeed=921600" in FQBN_COMPILE
    assert upload.replace("UploadSpeed=512000", "") == FQBN_COMPILE.replace(
        "UploadSpeed=921600", ""
    )


def test_command_builders():
    from bootstrap_app.logic import build_compile, build_device_admin, build_upload

    assert build_compile("arduino-cli", "fqbn", "sketch", "out") == [
        "arduino-cli",
        "compile",
        "--fqbn",
        "fqbn",
        "sketch",
        "--output-dir",
        "out",
    ]
    assert build_upload("arduino-cli", "fqbn", "COM6", "out") == [
        "arduino-cli",
        "upload",
        "-p",
        "COM6",
        "--fqbn",
        "fqbn",
        "--input-dir",
        "out",
    ]
    assert build_device_admin(["status", "-device", DEVICE]) == [
        "go",
        "run",
        "./cmd/device-admin",
        "status",
        "-device",
        DEVICE,
    ]


def test_database_url_from_env():
    from bootstrap_app.logic import database_url_from_env

    url = database_url_from_env(
        {"POSTGRES_USER": "checkpoint", "POSTGRES_PASSWORD": "12345678", "POSTGRES_DB": "checkpoint_db"}
    )
    assert url == "postgres://checkpoint:12345678@localhost:5432/checkpoint_db?sslmode=disable"
    assert database_url_from_env({"POSTGRES_USER": "checkpoint"}) == ""
