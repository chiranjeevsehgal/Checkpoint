"""mcp-service entrypoint.

One process owns the bge-m3 embedder, the TurboVec index, a polling indexer
thread, and the authenticated MCP endpoint. Run a single worker: TurboVec is
not safe across processes.
"""

import logging
import threading

from mcp.server import MCPServer
from mcp.server.auth.settings import AuthSettings
from pydantic import AnyHttpUrl

from . import auth, search, tools
from .config import load
from .embedder import EMBEDDING_DIM, Embedder
from .indexer import Indexer
from .store import ReadStore

log = logging.getLogger("mcp-service")


def _index_loop(indexer: Indexer, poll_seconds: int, stop: threading.Event) -> None:
    while not stop.wait(poll_seconds):
        try:
            indexer.run_once()
        except Exception as exc:  # noqa: BLE001 - keep serving reads through index errors
            log.warning("index cycle failed: %s", exc)
            try:
                indexer.reset()
            except Exception:  # noqa: BLE001
                log.warning("indexer reconnect failed")


def build_server(cfg, store: ReadStore, index, embedder: Embedder) -> MCPServer:
    server = MCPServer(
        "checkpoint",
        token_verifier=auth.KeyTokenVerifier(cfg.database_mcp_url),
        auth=AuthSettings(
            issuer_url=AnyHttpUrl(cfg.public_url),
            resource_server_url=AnyHttpUrl(cfg.public_url.rstrip("/") + "/mcp"),
            required_scopes=[auth.SCOPE],
            validate_token_resource=False,
        ),
    )
    tools.register(server, store, index, embedder, cfg.fallback_timezone)
    return server


def main() -> None:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
    cfg = load()

    embedder = Embedder(cfg.embedding_model)
    index = search.DocumentIndex(dim=EMBEDDING_DIM, bit_width=cfg.turbovec_bits)

    store = ReadStore(cfg.database_mcp_url)
    store.connect()

    indexer = Indexer(cfg.database_worker_url, embedder, index, cfg.embed_batch_size)
    indexer.connect()
    indexer.rebuild_index()
    indexer.run_once()

    stop = threading.Event()
    threading.Thread(target=_index_loop, args=(indexer, cfg.poll_seconds, stop), daemon=True).start()

    server = build_server(cfg, store, index, embedder)
    try:
        server.run(transport="streamable-http", host=cfg.host, port=cfg.port)
    finally:
        stop.set()
        store.close()
        indexer.close()


if __name__ == "__main__":
    main()
