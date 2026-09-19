"""
Usage, from the mcp-service directory:

    DATABASE_URL=... CHECKPOINT_MCP_PASSWORD=... python migrate.py up
    python migrate.py status

Migrations run as a separate deployment step and use the goose SQL format
so they stay diffable against the other services; this runner implements
the subset goose needs: up + status. The MCP role is created here (not in
bootstrap.sh alone) so already-initialized volumes get it without a reset.
"""

import os
import re
import sys
from pathlib import Path

import psycopg
from psycopg import sql

VERSION_TABLE = "mcp_schema_version"
MCP_ROLE = "checkpoint_mcp"
MIGRATIONS_DIR = Path(__file__).parent / "migrations"
UP = re.compile(r"^--\s*\+goose\s+Up\b", re.IGNORECASE)
DOWN = re.compile(r"^--\s*\+goose\s+Down\b", re.IGNORECASE)
FILENAME = re.compile(r"^(\d+)_.*\.sql$")


def up_section(sql_text: str) -> str:
    lines = sql_text.splitlines()
    start = next((i for i, line in enumerate(lines) if UP.match(line)), None)
    if start is None:
        raise ValueError("missing '-- +goose Up' marker")
    end = next((i for i, line in enumerate(lines) if i > start and DOWN.match(line)), len(lines))
    return "\n".join(lines[start + 1 : end])


def migration_files() -> list[tuple[int, Path]]:
    files = []
    for path in sorted(MIGRATIONS_DIR.glob("*.sql")):
        match = FILENAME.match(path.name)
        if match:
            files.append((int(match.group(1)), path))
    return files


def ensure_version_table(cur) -> None:
    cur.execute(
        f"""
        CREATE TABLE IF NOT EXISTS {VERSION_TABLE} (
            id SERIAL PRIMARY KEY,
            version_id BIGINT NOT NULL,
            is_applied BOOLEAN NOT NULL,
            tstamp TIMESTAMP NULL DEFAULT now()
        )
        """
    )


def current_version(cur) -> int | None:
    cur.execute(
        f"""
        SELECT version_id FROM {VERSION_TABLE}
        WHERE is_applied ORDER BY id DESC LIMIT 1
        """
    )
    row = cur.fetchone()
    return row[0] if row else None


def bootstrap_role(conn) -> None:
    """Create the MCP role if missing. Requires CHECKPOINT_MCP_PASSWORD."""
    password = os.getenv("CHECKPOINT_MCP_PASSWORD")
    if not password:
        return
    with conn.cursor() as cur:
        cur.execute("SELECT 1 FROM pg_roles WHERE rolname = %s", (MCP_ROLE,))
        if cur.fetchone() is None:
            cur.execute(
                sql.SQL("CREATE ROLE {} LOGIN PASSWORD {} NOBYPASSRLS").format(
                    sql.Identifier(MCP_ROLE), sql.Literal(password)
                )
            )
    conn.commit()


def cmd_up(conn) -> None:
    bootstrap_role(conn)
    with conn.cursor() as cur:
        ensure_version_table(cur)
        applied = current_version(cur)
        for version, path in migration_files():
            if applied is not None and version <= applied:
                continue
            print(f"applying {path.name}")
            cur.execute(up_section(path.read_text(encoding="utf-8")))
            cur.execute(
                f"INSERT INTO {VERSION_TABLE} (version_id, is_applied) VALUES (%s, TRUE)",
                (version,),
            )
        conn.commit()
    print("up to date")


def cmd_status(conn) -> None:
    with conn.cursor() as cur:
        ensure_version_table(cur)
        cur.execute(
            f"SELECT version_id, is_applied, tstamp FROM {VERSION_TABLE} ORDER BY id"
        )
        for version_id, is_applied, tstamp in cur.fetchall():
            mark = "v" if is_applied else "x"
            print(f"[{mark}] {version_id} {tstamp or ''}")
    conn.commit()


def main() -> None:
    if len(sys.argv) != 2 or sys.argv[1] not in ("up", "status"):
        print("usage: migrate.py <up|status>", file=sys.stderr)
        sys.exit(2)

    database_url = os.getenv("DATABASE_URL")
    if not database_url:
        print("DATABASE_URL must be set", file=sys.stderr)
        sys.exit(1)

    with psycopg.connect(database_url) as conn:
        if sys.argv[1] == "up":
            cmd_up(conn)
        else:
            cmd_status(conn)


if __name__ == "__main__":
    main()
