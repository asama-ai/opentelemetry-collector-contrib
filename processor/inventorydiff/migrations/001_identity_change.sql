-- otel.identity_change — inventorydiff changelog events (one row per change).
-- Prod ClickHouse is a 3-node cluster behind HTTP LB — use ON CLUSTER.
--
-- Note: creating this table alone does not route platform otel-exporter here.
-- Until routing is configured, live change events still land in otel.logs
-- (filter: ResourceAttributes['service.name'] = 'asama-inventory-changelog').

CREATE TABLE IF NOT EXISTS otel.identity_change ON CLUSTER default
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
SETTINGS index_granularity = 8192;
