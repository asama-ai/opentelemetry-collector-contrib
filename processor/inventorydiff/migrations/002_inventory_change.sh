#!/usr/bin/env bash
# Create/replace otel.inventory_change for UI/KG-ready inventory events.
# Deprecates otel.identity_change (raw pre/post metric dumps).
#
# Same pattern as otel.otel_configfiles:
#   - standard OTEL log columns (optional via DEFAULT '' / 0)
#   - product columns as DEFAULT from ResourceAttributes / LogAttributes
#
# WARNING: DROP + CREATE. Back up if the table has data you need.
#
# Usage:
#   export CLICKHOUSE_HOST=YOUR_CLICKHOUSE_HOST
#   ./processor/inventorydiff/migrations/002_inventory_change.sh
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

run_sql() {
  local sql="$1"
  curl -sS --fail-with-body --user "${AUTH}" "${URL}" --data-binary "${sql}"
  echo
}

echo "Dropping deprecated identity_change (if any) on cluster=${CLICKHOUSE_CLUSTER}..."
run_sql "DROP TABLE IF EXISTS ${CLICKHOUSE_DB}.identity_change ON CLUSTER ${CLICKHOUSE_CLUSTER} SYNC"

echo "Dropping inventory_change (if any) on cluster=${CLICKHOUSE_CLUSTER}..."
run_sql "DROP TABLE IF EXISTS ${CLICKHOUSE_DB}.inventory_change ON CLUSTER ${CLICKHOUSE_CLUSTER} SYNC"

echo "Creating inventory_change (UI/KG-ready product columns)..."
run_sql "$(cat <<EOF
CREATE TABLE IF NOT EXISTS ${CLICKHOUSE_DB}.inventory_change ON CLUSTER ${CLICKHOUSE_CLUSTER}
(
    -- Standard OTEL log columns: present so the exporter can INSERT.
    Timestamp DateTime64(9) DEFAULT now64(9) CODEC(Delta(8), ZSTD(1)),
    TimestampTime DateTime DEFAULT toDateTime(Timestamp),
    TraceId String DEFAULT '' CODEC(ZSTD(1)),
    SpanId String DEFAULT '' CODEC(ZSTD(1)),
    TraceFlags UInt8 DEFAULT 0,
    SeverityText LowCardinality(String) DEFAULT '' CODEC(ZSTD(1)),
    SeverityNumber UInt8 DEFAULT 0,
    ServiceName LowCardinality(String) DEFAULT '' CODEC(ZSTD(1)),
    Body String DEFAULT '' CODEC(ZSTD(1)),
    ResourceSchemaUrl LowCardinality(String) DEFAULT '' CODEC(ZSTD(1)),
    ResourceAttributes Map(LowCardinality(String), String) DEFAULT map() CODEC(ZSTD(1)),
    ScopeSchemaUrl LowCardinality(String) DEFAULT '' CODEC(ZSTD(1)),
    ScopeName String DEFAULT '' CODEC(ZSTD(1)),
    ScopeVersion LowCardinality(String) DEFAULT '' CODEC(ZSTD(1)),
    ScopeAttributes Map(LowCardinality(String), String) DEFAULT map() CODEC(ZSTD(1)),
    LogAttributes Map(LowCardinality(String), String) DEFAULT map() CODEC(ZSTD(1)),
    EventName String DEFAULT '' CODEC(ZSTD(1)),

    -- Product columns: KG apply / UI only (metric→node map stays in code).
    Hostname LowCardinality(String) DEFAULT if(ResourceAttributes['hostname'] != '', ResourceAttributes['hostname'], ResourceAttributes['host.name']) CODEC(ZSTD(1)),
    Tenant LowCardinality(String) DEFAULT ResourceAttributes['tenant.id'] CODEC(ZSTD(1)),
    Component LowCardinality(String) DEFAULT LogAttributes['component'] CODEC(ZSTD(1)),
    Action LowCardinality(String) DEFAULT LogAttributes['action'] CODEC(ZSTD(1)),
    EntityType LowCardinality(String) DEFAULT LogAttributes['entity_type'] CODEC(ZSTD(1)),
    EntityName String DEFAULT LogAttributes['entity_name'] CODEC(ZSTD(1)),
    Summary String DEFAULT LogAttributes['summary'] CODEC(ZSTD(1)),
    IdentityKeys String DEFAULT LogAttributes['identity_keys'] CODEC(ZSTD(1)),
    Payload String DEFAULT LogAttributes['payload'] CODEC(ZSTD(1)),
    Topology String DEFAULT LogAttributes['topology'] CODEC(ZSTD(1)),
    Changes String DEFAULT LogAttributes['changes'] CODEC(ZSTD(1)),
    KgOps String DEFAULT LogAttributes['kg_ops'] CODEC(ZSTD(1)),
    RequestId String DEFAULT LogAttributes['request_id'] CODEC(ZSTD(1)),
    ObservedAt DateTime64(9) DEFAULT parseDateTime64BestEffortOrZero(LogAttributes['observed_at']) CODEC(ZSTD(1)),

    INDEX idx_request_id RequestId TYPE bloom_filter(0.001) GRANULARITY 1,
    INDEX idx_hostname Hostname TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_component Component TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/otel/inventory_change_v1', '{replica}')
PARTITION BY toDate(TimestampTime)
PRIMARY KEY (ServiceName, TimestampTime)
ORDER BY (ServiceName, TimestampTime, Timestamp)
SETTINGS index_granularity = 8192
EOF
)"

echo "OK. Describe:"
run_sql "DESCRIBE TABLE inventory_change FORMAT PrettyCompact"
