"""mcp-service entrypoint.

One process owns the bge-m3 embedder, the TurboVec index, a polling indexer
thread, and the authenticated MCP endpoint. When the OAuth settings are present
it is also the MCP authorization server. Run a single worker: TurboVec is not
safe across processes.
"""

import logging
import threading

from mcp.server import MCPServer
from mcp.server.auth.settings import AuthSettings, ClientRegistrationOptions, RevocationOptions
from pydantic import AnyHttpUrl

from . import auth, search, tools
from .config import load
from .embedder import EMBEDDING_DIM, Embedder
from .indexer import Indexer
from .logging_setup import setup
from .oauth import CheckpointOAuthProvider
from .oauth_store import OAuthStore
from .oauth_web import register_oauth_routes
from .store import ReadStore

log = logging.getLogger("mcp-service")


def _index_loop(indexer: Indexer, poll_seconds: int, stop: threading.Event) -> None:
    while not stop.wait(poll_seconds):
        try:
            indexer.run_once()
        except Exception as exc:  # noqa: BLE001 - keep serving reads through index errors
            log.warning("index cycle failed", extra={"error": str(exc)})
            try:
                indexer.reset()
            except Exception:  # noqa: BLE001
                log.warning("indexer reconnect failed")


def _auth_settings(cfg, allow_registration: bool) -> AuthSettings:
    return AuthSettings(
        issuer_url=AnyHttpUrl(cfg.public_url),
        resource_server_url=AnyHttpUrl(cfg.public_url.rstrip("/") + "/mcp"),
        required_scopes=[auth.SCOPE],
        client_registration_options=(
            ClientRegistrationOptions(enabled=True, valid_scopes=[auth.SCOPE], default_scopes=[auth.SCOPE])
            if allow_registration else None
        ),
        revocation_options=RevocationOptions(enabled=True) if allow_registration else None,
        validate_token_resource=False,
    )


def build_server(cfg, store: ReadStore, index, embedder: Embedder,
                 oauth_store: OAuthStore | None = None, as_of=None) -> MCPServer:
    if oauth_store is not None:
        provider = CheckpointOAuthProvider(
            oauth_store, cfg.public_url, cfg.database_mcp_url,
            set(cfg.oauth_allowed_redirect_hosts), cfg.oauth_access_ttl_seconds,
            cfg.oauth_refresh_ttl_seconds,
        )
        server = MCPServer("checkpoint", auth_server_provider=provider, auth=_auth_settings(cfg, True),
                           instructions=tools.GROUNDING)
        register_oauth_routes(server, provider, oauth_store, cfg.oauth_session_secret,
                              cfg.kratos_public_url, cfg.public_url)
    else:
        server = MCPServer(
            "checkpoint",
            token_verifier=auth.KeyTokenVerifier(cfg.database_mcp_url),
            auth=_auth_settings(cfg, False),
            instructions=tools.GROUNDING,
        )
    tools.register(server, store, index, embedder, cfg.fallback_timezone, cfg.kratos_admin_url,
                   cfg.max_text_chars, as_of=as_of, hybrid_enabled=cfg.hybrid_enabled,
                   oversample=cfg.oversample)
    return server


def main() -> None:
    setup("mcp-service")
    cfg = load()

    embedder = Embedder(cfg.embedding_model)
    index = search.DocumentIndex(dim=EMBEDDING_DIM, bit_width=cfg.turbovec_bits)

    store = ReadStore(cfg.database_mcp_url)
    store.connect()

    indexer = Indexer(cfg.database_worker_url, embedder, index, cfg.embed_batch_size)
    indexer.connect()
    indexer.rebuild_index()
    indexer.run_once()

    oauth_store = None
    if cfg.oauth_enabled:
        oauth_store = OAuthStore(cfg.database_worker_url)
        oauth_store.connect()
        log.info("oauth authorization server enabled")

    stop = threading.Event()
    threading.Thread(target=_index_loop, args=(indexer, cfg.poll_seconds, stop), daemon=True).start()

    server = build_server(cfg, store, index, embedder, oauth_store,
                          as_of=lambda: indexer.last_synced_at)
    try:
        server.run(transport="streamable-http", host=cfg.host, port=cfg.port)
    finally:
        stop.set()
        store.close()
        indexer.close()
        if oauth_store is not None:
            oauth_store.close()


if __name__ == "__main__":
    main()
