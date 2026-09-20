import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Config:
    kafka_brokers: str
    kafka_topic: str
    kafka_consumer_group: str
    database_url: str
    embedding_model: str
    chunk_tokens: int
    chunk_overlap_tokens: int
    retry_backoffs: tuple[int, ...]
    metrics_addr: str
    metrics_port: int


def _int_env(name: str, default: int) -> int:
    raw = os.getenv(name)
    if raw is None or raw == "":
        return default
    try:
        return int(raw)
    except ValueError:
        raise ValueError(f"{name} must be an integer, got {raw!r}")


def _csv_int_env(name: str, default: tuple[int, ...]) -> tuple[int, ...]:
    raw = os.getenv(name)
    if raw is None or raw == "":
        return default
    try:
        values = tuple(int(part) for part in raw.split(",") if part.strip())
    except ValueError:
        raise ValueError(f"{name} must be comma-separated integers, got {raw!r}")
    if not values:
        raise ValueError(f"{name} must not be empty")
    return values


def load() -> Config:
    database_url = os.getenv("DATABASE_URL")
    if not database_url:
        raise ValueError("DATABASE_URL must be set")

    cfg = Config(
        kafka_brokers=os.getenv("KAFKA_BROKERS", "kafka:9092"),
        kafka_topic=os.getenv("KAFKA_TOPIC_EMBEDDING", "embedding.jobs.v1"),
        kafka_consumer_group=os.getenv("KAFKA_CONSUMER_GROUP", "embedding-service"),
        database_url=database_url,
        embedding_model=os.getenv("EMBEDDING_MODEL", "BAAI/bge-m3"),
        chunk_tokens=_int_env("EMBEDDING_CHUNK_TOKENS", 2048),
        chunk_overlap_tokens=_int_env("EMBEDDING_CHUNK_OVERLAP_TOKENS", 100),
        retry_backoffs=_csv_int_env("EMBEDDING_RETRY_BACKOFFS", (1, 5, 15, 60)),
        metrics_addr=os.getenv("METRICS_ADDR", "0.0.0.0"),
        metrics_port=_int_env("METRICS_PORT", 9085),
    )

    if cfg.chunk_tokens <= 0:
        raise ValueError("EMBEDDING_CHUNK_TOKENS must be positive")
    if cfg.chunk_overlap_tokens < 0 or cfg.chunk_overlap_tokens >= cfg.chunk_tokens:
        raise ValueError("EMBEDDING_CHUNK_OVERLAP_TOKENS must be in [0, EMBEDDING_CHUNK_TOKENS)")
    return cfg
