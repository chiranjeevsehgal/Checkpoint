#!/bin/bash
set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    DO \$\$
    BEGIN
      IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_request') THEN
        CREATE ROLE checkpoint_request LOGIN PASSWORD '${CHECKPOINT_REQUEST_PASSWORD}' NOBYPASSRLS NOINHERIT;
      END IF;
      IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_worker') THEN
        CREATE ROLE checkpoint_worker LOGIN PASSWORD '${CHECKPOINT_WORKER_PASSWORD}' BYPASSRLS NOINHERIT;
      END IF;
      IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'checkpoint_mcp') THEN
        CREATE ROLE checkpoint_mcp LOGIN PASSWORD '${CHECKPOINT_MCP_PASSWORD}' NOBYPASSRLS NOINHERIT;
      END IF;
      IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'kratos') THEN
        CREATE ROLE kratos LOGIN PASSWORD '${KRATOS_DB_PASSWORD}';
      END IF;
    END
    \$\$;
EOSQL

if ! psql --username "$POSTGRES_USER" --dbname postgres -tAc "SELECT 1 FROM pg_database WHERE datname = 'kratos'" | grep -q 1; then
    psql --username "$POSTGRES_USER" --dbname postgres -c "CREATE DATABASE kratos OWNER kratos"
fi
