#!/usr/bin/env bash
# Create otel.identity_change on the ClickHouse cluster (HTTP interface).
#
# Usage:
#   export CLICKHOUSE_HOST=YOUR_CLICKHOUSE_HOST
#   ./migrations/001_identity_change.sh
#
# Defaults: port=18123 user=default password=(empty) database=otel cluster=default

set -euo pipefail

: "${CLICKHOUSE_HOST:?set CLICKHOUSE_HOST}"
CLICKHOUSE_HTTP_PORT="${CLICKHOUSE_HTTP_PORT:-18123}"
CLICKHOUSE_USER="${CLICKHOUSE_USER:-default}"
CLICKHOUSE_PASSWORD="${CLICKHOUSE_PASSWORD-}"
CLICKHOUSE_DB="${CLICKHOUSE_DB:-otel}"
CLICKHOUSE_CLUSTER="${CLICKHOUSE_CLUSTER:-default}"

AUTH="${CLICKHOUSE_USER}"
if [[ -n "${CLICKHOUSE_PASSWORD}" ]]; then
  AUTH="${CLICKHOUSE_USER}:${CLICKHOUSE_PASSWORD}"
fi

URL="http://${CLICKHOUSE_HOST}:${CLICKHOUSE_HTTP_PORT}/?database=${CLICKHOUSE_DB}"

SQL=$(cat <<EOF
CREATE TABLE IF NOT EXISTS ${CLICKHOUSE_DB}.identity_change ON CLUSTER ${CLICKHOUSE_CLUSTER}
(
    Timestamp DateTime64(9) CODEC(Delta(8), ZSTD(1)),
    TimestampTime DateTime DEFAULT toDateTime(Timestamp),
    PreObservedAt DateTime64(9) CODEC(ZSTD(1)),
    PostObservedAt DateTime64(9) CODEC(ZSTD(1)),
    Tenant LowCardinality(String) DEFAULT '' CODEC(ZSTD(1)),
    Hostname LowCardinality(String) CODEC(ZSTD(1)),
    ServiceName LowCardinality(String) CODEC(ZSTD(1)),
    Metric LowCardinality(String) CODEC(ZSTD(1)),
    Action LowCardinality(String) CODEC(ZSTD(1)),
    RequestId String CODEC(ZSTD(1)),
    PrechangeData String CODEC(ZSTD(1)),
    PostchangeData String CODEC(ZSTD(1)),
    Body String CODEC(ZSTD(1)),
    ResourceAttributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    LogAttributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    INDEX idx_request_id RequestId TYPE bloom_filter(0.001) GRANULARITY 1,
    INDEX idx_hostname Hostname TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_metric Metric TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/otel/identity_change', '{replica}')
PARTITION BY toDate(TimestampTime)
PRIMARY KEY (ServiceName, Hostname, Metric, TimestampTime)
ORDER BY (ServiceName, Hostname, Metric, TimestampTime, Timestamp)
SETTINGS index_granularity = 8192
EOF
)

echo "Applying identity_change -> ${CLICKHOUSE_USER}@${CLICKHOUSE_HOST}:${CLICKHOUSE_HTTP_PORT}/${CLICKHOUSE_DB} (cluster=${CLICKHOUSE_CLUSTER})"
curl -sS --fail-with-body --user "${AUTH}" "${URL}" --data-binary "${SQL}"
echo
echo "OK. Describe:"
curl -sS --fail-with-body --user "${AUTH}" "${URL}" --data-binary "DESCRIBE TABLE identity_change FORMAT PrettyCompact"
echo
