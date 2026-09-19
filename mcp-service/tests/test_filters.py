import unittest
from datetime import datetime, timezone

from app.search import build_filters


class BuildFiltersTest(unittest.TestCase):
    def test_user_only_is_a_single_condition(self):
        self.assertEqual(
            build_filters("user-1"),
            {"field": "meta.user_id", "operator": "==", "value": "user-1"},
        )

    def test_types_and_range_combine_with_and(self):
        filters = build_filters(
            "user-1",
            source_types=("todo", "reminder"),
            start=datetime(2026, 1, 1, tzinfo=timezone.utc),
            end=None,
        )
        self.assertEqual(filters["operator"], "AND")
        self.assertEqual(len(filters["conditions"]), 3)
        self.assertEqual(filters["conditions"][0]["field"], "meta.user_id")
        self.assertEqual(filters["conditions"][1]["value"], ["todo", "reminder"])

    def test_user_only_is_not_a_compound_filter(self):
        filters = build_filters("user-1", source_types=None)
        self.assertNotIn("conditions", filters)
        self.assertEqual(filters["field"], "meta.user_id")


if __name__ == "__main__":
    unittest.main()
