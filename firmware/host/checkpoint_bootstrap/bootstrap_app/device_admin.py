"""Run the privileged device-admin Go CLI and stream its output."""

import os
import subprocess
from pathlib import Path
from typing import Callable

from .logic import build_device_admin


def find_repo_root(start: Path | None = None) -> Path | None:
    """Walk up until the directory containing ingestion-service is found."""
    here = (start or Path(__file__).resolve()).parent
    for candidate in (here, *here.parents):
        if (candidate / "ingestion-service" / 'cmd' / 'device-admin').is_dir():
            return candidate
    return None


def load_env_file(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    if not path.is_file():
        return values
    for raw in path.read_text(encoding='utf-8').splitlines():
        line = raw.strip()
        if not line or line.startswith('#') or '=' not in line:
            continue
        key, _, value = line.partition('=')
        values[key.strip()] = value.strip()
    return values


def stream_command(
    command: list[str],
    on_line: Callable[[str], None],
    cwd: Path | None = None,
    env: dict[str, str] | None = None,
) -> int:
    """Run a command, streaming merged stdout/stderr line by line."""
    process = subprocess.Popen(
        command,
        cwd=cwd,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        encoding='utf-8',
        errors='replace',
        bufsize=1,
    )
    output = process.stdout
    if output is None:
        raise RuntimeError('process produced no output stream')
    for line in output:
        on_line(line.rstrip('\r\n'))
    return process.wait()


def run_device_admin(
    repo_root: Path,
    args: list[str],
    database_url: str,
    on_line: Callable[[str], None],
) -> int:
    """Run device-admin from ingestion-service, streaming stdout and stderr."""
    return stream_command(
        build_device_admin(args),
        on_line,
        cwd=repo_root / 'ingestion-service',
        env={**os.environ, 'DATABASE_URL': database_url},
    )
