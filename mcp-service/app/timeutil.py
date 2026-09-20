"""Timestamp formatting and range parsing for the MCP tools.

Every instant is returned twice: as UTC and in the user's IANA zone, so the
client LLM never has to guess a timezone or an offset.
"""

from datetime import date, datetime, time, timezone
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

DATE_FORMAT_LENGTH = 10


def resolve_zone(name: str | None, fallback: str) -> ZoneInfo:
    """The user's zone, else the service fallback, else UTC."""
    for candidate in (name, fallback, "UTC"):
        if candidate:
            try:
                return ZoneInfo(candidate)
            except (ZoneInfoNotFoundError, ValueError):
                continue
    return ZoneInfo("UTC")


def utc_now() -> datetime:
    return datetime.now(timezone.utc)


def as_utc(value: datetime) -> datetime:
    """Treat a naive timestamp as UTC and return an aware datetime."""
    if value.tzinfo is None:
        return value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc)


def utc_iso(value: datetime) -> str:
    return as_utc(value).isoformat()


def local_iso(value: datetime, zone: ZoneInfo) -> str:
    return as_utc(value).astimezone(zone).isoformat()


def dual(value: datetime | None, zone: ZoneInfo) -> dict[str, str | None]:
    """Both renderings of one instant, or nulls when it is absent."""
    if value is None:
        return {"utc": None, "local": None}
    return {"utc": utc_iso(value), "local": local_iso(value, zone)}


def loads(value: str | None) -> datetime | None:
    """Parse an ISO-8601 string produced by dual(), or None."""
    if not value:
        return None
    return datetime.fromisoformat(value)


def parse_bound(value: str | None, zone: ZoneInfo, end_of_day: bool) -> datetime | None:
    """Parse a range bound. A bare YYYY-MM-DD is interpreted in the user's
    zone; an ISO-8601 instant keeps its own offset. Returns UTC."""
    if value is None or value == "":
        return None
    raw = value.strip()
    if len(raw) == DATE_FORMAT_LENGTH and raw.count("-") == 2:
        day = date.fromisoformat(raw)
        clock = time.max if end_of_day else time.min
        return datetime.combine(day, clock, tzinfo=zone).astimezone(timezone.utc)
    parsed = datetime.fromisoformat(raw)
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=zone)
    return parsed.astimezone(timezone.utc)


def parse_range(start: str | None, end: str | None, zone: ZoneInfo) -> tuple[datetime | None, datetime | None]:
    return parse_bound(start, zone, end_of_day=False), parse_bound(end, zone, end_of_day=True)
