"""
Host-side checks for BLE advertising discovery.
Regression: the app filters scans by SERVICE_UUID, so the UUID must be in the
advertisement packet (not only the scan response) and the scan response must
be re-applied after a controller resync.
Run: pytest firmware/tests/test_ble_advertising.py -v
"""
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent / "checkpoint"


def read(name: str) -> str:
    return (BASE / name).read_text(encoding="utf-8", errors="ignore")


def test_service_uuid_in_advertisement():
    ble = read("ble_service.cpp")
    # UUID must be in the ADV packet; the name alone does not drive the filter.
    assert "advData.addServiceUUID(BLE_SERVICE_UUID)" in ble
    assert "advData.setName" not in ble, "name in ADV overflows the 31-byte limit"
    print("PASS uuid in advertisement")


def test_scan_response_enabled():
    ble = read("ble_service.cpp")
    assert "scanResp.setName(BLE_DEVICE_NAME)" in ble
    assert "scanResp.addServiceUUID(BLE_SERVICE_UUID)" in ble
    assert "enableScanResponse(true)" in ble, "scan response lost after host resync"
    print("PASS scan response")


def test_advertising_errors_logged():
    ble = read("ble_service.cpp")
    assert 'LOG_E("BLE adv data fail")' in ble
    assert 'LOG_E("BLE scan rsp fail")' in ble
    assert 'LOG_E("BLE adv start fail")' in ble
    print("PASS adv errors logged")


def test_advertising_watchdog():
    ble = read("ble_service.cpp")
    assert "ble_check_advertising" in ble
    assert "isAdvertising()" in ble
    assert "adv->start()" in ble
    ino = read("checkpoint.ino")
    assert "ble_check_advertising()" in ino
    print("PASS advertising watchdog")


if __name__ == "__main__":
    test_service_uuid_in_advertisement()
    test_scan_response_enabled()
    test_advertising_errors_logged()
    test_advertising_watchdog()
    print("ALL BLE ADVERTISING PASS")
