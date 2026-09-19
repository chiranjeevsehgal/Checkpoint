import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Config:
    database_mcp_url: str
    database_worker_url: str
    embedding_model: str
    embed_batch_size: int
    host: str
    port: int
    public_url: str
    turbovec_bits: int
    poll_seconds: int
    fallback_timezone: str


def _int_env(name: str, default: int) -> int:
    raw = os.getenv(name)
    if raw is None or raw == "":
        return default
    try:
        return int(raw)
    except ValueError:
        raise ValueError(f"{name} must be an integer, got {raw!r}")


def load() -> Config:
    database_mcp_url = os.getenv("DATABASE_MCP_URL")
    if not database_mcp_url:
        raise ValueError("DATABASE_MCP_URL must be set")
    database_worker_url = os.getenv("DATABASE_WORKER_URL")
    if not database_worker_url:
        raise ValueError("DATABASE_WORKER_URL must be set")

    cfg = Config(
        database_mcp_url=database_mcp_url,
        database_worker_url=database_worker_url,
        embedding_model=os.getenv("EMBEDDING_MODEL", "BAAI/bge-m3"),
        embed_batch_size=_int_env("MCP_EMBED_BATCH_SIZE", 32),
        host=os.getenv("MCP_HOST", "0.0.0.0"),
        port=_int_env("MCP_PORT", 1417),
        public_url=os.getenv("MCP_PUBLIC_URL", "http://localhost:1417"),
        turbovec_bits=_int_env("MCP_TURBOVEC_BITS", 4),
        poll_seconds=_int_env("MCP_POLL_SECONDS", 30),
        fallback_timezone=os.getenv("MCP_FALLBACK_TIMEZONE", "UTC"),
    )

    if cfg.turbovec_bits not in (2, 3, 4):
        raise ValueError("MCP_TURBOVEC_BITS must be one of 2, 3, 4")
    if cfg.poll_seconds <= 0:
        raise ValueError("MCP_POLL_SECONDS must be positive")
    if cfg.embed_batch_size <= 0:
        raise ValueError("MCP_EMBED_BATCH_SIZE must be positive")
    return cfg
