# inventorydiff processor

Watches configured metric names on the **collector VM** otel-collector metrics pipeline.
Keeps an in-memory last snapshot per `(hostname, metric)` (deep-copied on write).
When the snapshot changes, emits an OTLP log event to `changelog.endpoint`.
Metrics are always forwarded unchanged (fail-open if changelog export fails).

## Config

```yaml
processors:
  inventorydiff:
    metrics:
      - node_md_member_info
    changelog:
      endpoint: https://YOUR_PLATFORM_HOST/api/otel
      headers:
        Authorization: "Basic ..."

service:
  pipelines:
    metrics:
      receivers: [otlp]
      processors: [inventorydiff, batch]
      exporters: [prometheusremotewrite]
```

Rebuild the collector image/binary from `cmd/otel-collector/builder-config.yaml` so `inventorydiff` is compiled in. YAML alone is not enough.

## Change detection

Compares label sets + values only (timestamps ignored):

- series added / removed
- label change
- value change
- watched metric present with zero series → `action=delete`, empty post series

Watched metrics **absent** from a batch are skipped (partial domain×tier OTLP pushes must not look like deletes).

## Event

OTLP log with `service.name=asama-inventory-changelog` and attributes:

- `request_id`, `action`, `hostname`, `metric`
- `prechange_data`, `postchange_data` (JSON snapshots)
- `pre_observed_at`, `post_observed_at`

## ClickHouse + platform routing

**1. Recreate table** (logs-shaped, like `otel_configfiles`):

```bash
export CLICKHOUSE_HOST=YOUR_CLICKHOUSE_HOST
./migrations/001_identity_change.sh
```

**2. Platform `otel-exporter.yaml`** — add filter, exporter, pipeline; exclude from `logs/general` (same pattern as configfiles). See README section below / deploy notes.

**3. Restart** `otel-exporter`, then query:

```sql
SELECT Timestamp, Hostname, Metric, Action, RequestId, PrechangeData, PostchangeData
FROM otel.identity_change
ORDER BY Timestamp DESC
LIMIT 20
```
