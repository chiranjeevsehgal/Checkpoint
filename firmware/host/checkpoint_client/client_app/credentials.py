"""Permanent per-device credentials. Session keys stay in RAM only."""

import json
import os
from pathlib import Path

SERVICE = "Checkpoint"


def _fallback_path() -> Path:
    return Path.home() / ".checkpoint" / "credentials.json"


def _device_label(device_id: bytes) -> str:
    return f"{SERVICE}/{bytes(device_id).hex()}"


def save_client_credential(device_id: bytes, client_id: bytes, client_key: bytes,
                           pending: bool = False) -> None:
    """Persist a credential. Pending enrollments are kept across reconnects
    until the READY finish is confirmed (see mark_active/delete_credential)."""
    label = _device_label(device_id)
    payload = {"client_id": bytes(client_id).hex(), "client_key": bytes(client_key).hex()}
    try:
        import keyring
        keyring.set_password(label, "client_id", payload["client_id"])
        keyring.set_password(label, "client_key", payload["client_key"])
        keyring.set_password(label, "pending", "1" if pending else "0")
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
    data[bytes(device_id).hex()] = {**payload, "pending": bool(pending)}
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


def is_pending(device_id: bytes) -> bool:
    label = _device_label(device_id)
    try:
        import keyring
        if keyring.get_password(label, "pending") == "1":
            return True
    except Exception:
        pass
    try:
        data = json.loads(_fallback_path().read_text())
        rec = data.get(bytes(device_id).hex())
        return bool(rec and rec.get("pending"))
    except Exception:
        return False


def mark_active(device_id: bytes) -> None:
    label = _device_label(device_id)
    try:
        import keyring
        if keyring.get_password(label, "client_id") is not None:
            keyring.set_password(label, "pending", "0")
    except Exception:
        pass
    path = _fallback_path()
    try:
        data = json.loads(path.read_text())
    except Exception:
        return
    rec = data.get(bytes(device_id).hex())
    if rec and rec.get("pending"):
        rec["pending"] = False
        path.write_text(json.dumps(data, indent=2))


def delete_credential(device_id: bytes) -> None:
    label = _device_label(device_id)
    try:
        import keyring
        for field in ("client_id", "client_key", "pending"):
            try:
                keyring.delete_password(label, field)
            except Exception:
                pass
    except Exception:
        pass
    path = _fallback_path()
    try:
        data = json.loads(path.read_text())
    except Exception:
        return
    if data.pop(bytes(device_id).hex(), None) is not None:
        path.write_text(json.dumps(data, indent=2))


def load_or_create_client_id(device_id: bytes) -> bytes:
    found = load_client_credential(device_id)
    if found:
        return found[0]
    import secrets
    return secrets.token_bytes(16)
