"""Permanent per-device credentials. Session keys stay in RAM only."""

import json
import os
from pathlib import Path

SERVICE = "Checkpoint"


def _fallback_path() -> Path:
    return Path.home() / ".checkpoint" / "credentials.json"


def _device_label(device_id: bytes) -> str:
    return f"{SERVICE}/{bytes(device_id).hex()}"


def save_client_credential(device_id: bytes, client_id: bytes, client_key: bytes) -> None:
    label = _device_label(device_id)
    payload = {"client_id": bytes(client_id).hex(), "client_key": bytes(client_key).hex()}
    try:
        import keyring
        keyring.set_password(label, "client_id", payload["client_id"])
        keyring.set_password(label, "client_key", payload["client_key"])
        return
    except Exception:
        pass
    path = _fallback_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    data = {}
    try:
        if path.exists():
            data = json.loads(path.read_text())
    except Exception:
        data = {}
    data[bytes(device_id).hex()] = payload
    path.write_text(json.dumps(data, indent=2))
    try:
        os.chmod(path, 0o600)
    except Exception:
        pass


def load_client_credential(device_id: bytes):
    label = _device_label(device_id)
    try:
        import keyring
        cid = keyring.get_password(label, "client_id")
        ckey = keyring.get_password(label, "client_key")
        if cid and ckey:
            return bytes.fromhex(cid), bytes.fromhex(ckey)
    except Exception:
        pass
    path = _fallback_path()
    try:
        data = json.loads(path.read_text())
        rec = data.get(bytes(device_id).hex())
        if rec:
            return bytes.fromhex(rec["client_id"]), bytes.fromhex(rec["client_key"])
    except Exception:
        pass
    return None


def load_or_create_client_id(device_id: bytes) -> bytes:
    found = load_client_credential(device_id)
    if found:
        return found[0]
    import secrets
    return secrets.token_bytes(16)
