import unittest
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

from app import timeutil


class TimeutilTest(unittest.TestCase):
    def test_resolve_zone_prefers_user_value(self):
        self.assertEqual(str(timeutil.resolve_zone("Asia/Kolkata", "UTC")), "Asia/Kolkata")

    def test_resolve_zone_falls_back_on_unknown(self):
        self.assertEqual(str(timeutil.resolve_zone("Not/AZone", "Europe/Berlin")), "Europe/Berlin")

    def test_dual_renders_utc_and_local(self):
        got = timeutil.dual(datetime(2026, 9, 20, 6, 30, tzinfo=timezone.utc), ZoneInfo("Asia/Kolkata"))
        self.assertEqual(got["utc"], "2026-09-20T06:30:00+00:00")
        self.assertEqual(got["local"], "2026-09-20T12:00:00+05:30")

    def test_dual_handles_absent_instant(self):
        self.assertEqual(timeutil.dual(None, ZoneInfo("UTC")), {"utc": None, "local": None})

    def test_parse_date_bound_uses_zone(self):
        start = timeutil.parse_bound("2026-09-20", ZoneInfo("Asia/Kolkata"), end_of_day=False)
        self.assertEqual(start, datetime(2026, 9, 19, 18, 30, tzinfo=timezone.utc))

    def test_parse_iso_bound_keeps_offset(self):
        got = timeutil.parse_bound("2026-09-20T12:00:00+05:30", ZoneInfo("UTC"), end_of_day=False)
        self.assertEqual(got, datetime(2026, 9, 20, 6, 30, tzinfo=timezone.utc))

    def test_parse_range_allows_missing_bounds(self):
        self.assertEqual(timeutil.parse_range(None, None, ZoneInfo("UTC")), (None, None))


if __name__ == "__main__":
    unittest.main()
