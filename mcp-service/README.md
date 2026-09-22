# mcp-service

Python MCP server (Streamable HTTP, `:1417`) over the derived
`search_documents` read model: `search`, `timeline`, `list_todos`,
`list_reminders`, `get_summaries`, `get_transcript`, `whoami`. Single worker
only (in-process TurboVec index, rebuilt at startup and refreshed every
`MCP_POLL_SECONDS`).

## Run

```bash
python -m unittest discover -s tests -v
TEST_DATABASE_URL=... python -m unittest tests.test_retrieval_eval -v
python -u -m app.main   # needs DATABASE_MCP_URL, DATABASE_WORKER_URL
```

## Retrieval

Hybrid by default (`MCP_HYBRID_ENABLED=1`): dense TurboVec vectors fused
with Postgres full-text (`to_tsvector('simple', content)`, GIN-indexed) via
reciprocal rank fusion, with `MCP_OVERSAMPLE` (default 2) over-fetch before
the `scoped()` ownership post-filter. Set `MCP_HYBRID_ENABLED=0` for
dense-only. Search is per-account rate limited (`MCP_SEARCH_PER_MINUTE`,
`MCP_SEARCH_BURST`).

## Auth

Static `cp_mcp_` keys (mint via ingestion `POST /v1/me/mcp-keys`) plus OAuth
2.1 for hosted clients when `MCP_OAUTH_SESSION_SECRET` + `KRATOS_PUBLIC_URL`
are set. OAuth sign-in emails a one-time code through Kratos (no password).
All reads run as `checkpoint_mcp` with per-transaction `app.user_id` (RLS).

## Config (env)

`DATABASE_MCP_URL`, `DATABASE_WORKER_URL`, `EMBEDDING_MODEL`,
`MCP_HOST/PORT/PUBLIC_URL`, `MCP_POLL_SECONDS`, `MCP_MAX_TEXT_CHARS`,
`MCP_FALLBACK_TIMEZONE`, `KRATOS_ADMIN_URL` (display names, best-effort).
