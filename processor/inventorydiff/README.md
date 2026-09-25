# inventorydiff processor

Watches inventory metrics on the **collector VM** otel-collector metrics pipeline.
Keeps an in-memory last snapshot per `(hostname, metric)` (deep-copied on write).
When the snapshot changes, **expands** the series diff into UI/KG-ready events and
emits one OTLP log record per entity change to `changelog.endpoint`.
Metrics are always forwarded unchanged (fail-open if changelog export fails).

The metric → component / entity / identity-label map lives in code
(`metric_map.go`). Config `metrics` is an optional allow-list; omit it to watch
every registered metric.

## Config

```yaml
processors:
  inventorydiff:
    # metrics: optional allow-list; omit to watch every metric in metric_map.go
    changelog:
      endpoint: https://YOUR_PLATFORM_HOST/api/otel
      headers:
        Authorization: "Basic ..."
    # Optional: start ComponentSyncWorkflow on Temporal (async, fail-open).
    component_sync:
      temporal_address: temporal:7233
      namespace: default
      task_queue: ciss-inventory-tasks
      workflow: ComponentSyncWorkflow
      tenant: nxtgen
      timeout: 10s

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [inventorydiff, batch]
      exporters: [prometheusremotewrite]
```

Rebuild the collector image/binary from `cmd/otel-collector/builder-config.yaml` so `inventorydiff` is compiled in. YAML alone is not enough.

## Change detection

Compares label sets + values only (timestamps ignored) to decide *whether* to emit.
Expansion then diffs by **identity labels** per metric:

- series identity gone → `action=remove`
- series identity new → `action=create`
- same identity, value or other labels changed → `action=update`

Watched metrics **absent** from a batch are skipped (partial domain×tier OTLP pushes must not look like deletes).

## Event (UI / KG)

OTLP log with `service.name=asama-inventory-changelog`. Attributes are **KG-apply
fields only** — metric→node mapping stays in `metric_map.go` and is not stored.

| Attribute | Meaning |
|-----------|---------|
| `request_id` | Shared across all entity events from one change batch |
| `hostname` | Device root (until Device matching is hostname-free) |
| `component` | storage / network / memory / … (component sync + UI filter) |
| `action` | create / update / remove |
| `entity_type` | RAID, OsDisk, Memory, NIC, … |
| `entity_name` | md0, nvme0n1, B11, … |
| `summary` | Human-readable UI string |
| `identity_keys` | JSON MERGE keys for the leaf node |
| `payload` | JSON props to set on the node |
| `topology` | JSON path hops Device → … → leaf |
| `kg_ops` | JSON targeted graph ops (edges) |
| `observed_at` | Observation time |

Example RAID member remove summary: `Removed nvme0n1 from RAID md0`.

## Component sync (optional)

When `component_sync` is set, each successful changelog export also starts
`ComponentSyncWorkflow` on Temporal (async, fail-open).

Metric → component comes from `metric_map.go` (aligned with CISS component names).

## ClickHouse

**Deprecated:** `otel.identity_change` (raw pre/post dumps). Use migration `002`.

```bash
export CLICKHOUSE_HOST=YOUR_CLICKHOUSE_HOST
./processor/inventorydiff/migrations/002_inventory_change.sh
```

Platform `otel-exporter.yaml` should route `asama-inventory-changelog` into
`otel.inventory_change` (same pattern as configfiles).

```sql
SELECT Timestamp, Hostname, Component, Action, EntityType, EntityName, Summary,
       IdentityKeys, Payload, Topology, KgOps, RequestId
FROM otel.inventory_change
ORDER BY Timestamp DESC
LIMIT 20
```
