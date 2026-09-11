#!/usr/bin/env bash
# Apply otel.identity_change DDL to ClickHouse (HTTP interface).
#
# DBeaver-style defaults for this env:
#   host=…  port=18123 (HTTP)  user=default  password=(empty)  database=otel
#
# Usage:
#   export CLICKHOUSE_HOST=10.25.20.155
#   export CLICKHOUSE_HTTP_PORT=18123
#   ./examples/apply_migration.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SQL_FILE="${ROOT}/migrations/001_identity_change.sql"

: "${CLICKHOUSE_HOST:?set CLICKHOUSE_HOST}"
CLICKHOUSE_HTTP_PORT="${CLICKHOUSE_HTTP_PORT:-18123}"
CLICKHOUSE_USER="${CLICKHOUSE_USER:-default}"
CLICKHOUSE_PASSWORD="${CLICKHOUSE_PASSWORD-}"
CLICKHOUSE_DB="${CLICKHOUSE_DB:-otel}"

# Strip SQL comments for HTTP.
SQL="$(python3 - "${SQL_FILE}" <<'PY'
import sys
from pathlib import Path
lines = []
for line in Path(sys.argv[1]).read_text().splitlines():
    s = line.strip()
    if not s or s.startswith("--"):
        continue
    lines.append(line)
print("\n".join(lines))
PY
)"

AUTH="${CLICKHOUSE_USER}"
if [[ -n "${CLICKHOUSE_PASSWORD}" ]]; then
  AUTH="${CLICKHOUSE_USER}:${CLICKHOUSE_PASSWORD}"
fi

URL="http://${CLICKHOUSE_HOST}:${CLICKHOUSE_HTTP_PORT}/?database=${CLICKHOUSE_DB}"

echo "Applying ${SQL_FILE} -> ${CLICKHOUSE_USER}@${CLICKHOUSE_HOST}:${CLICKHOUSE_HTTP_PORT}/${CLICKHOUSE_DB}"
curl -sS --fail-with-body --user "${AUTH}" "${URL}" --data-binary "${SQL}"
echo
echo "OK. Describe:"
curl -sS --fail-with-body --user "${AUTH}" "${URL}" --data-binary "DESCRIBE TABLE identity_change FORMAT PrettyCompact"
echo
