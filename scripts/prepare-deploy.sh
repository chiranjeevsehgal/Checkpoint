#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

chmod 755 infra/postgres/bootstrap.sh

find infra/kratos -type d -exec chmod 755 {} +
find infra/kratos -type f -exec chmod 644 {} +

find infra/ntfy -type d -exec chmod 755 {} +
find infra/ntfy -type f -exec chmod 644 {} +

find infra/caddy -type d -exec chmod 755 {} +
find infra/caddy -type f -exec chmod 644 {} +
