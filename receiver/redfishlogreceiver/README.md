# Redfish log receiver

Author: **[@sri-vinay-asama](https://github.com/sri-vinay-asama)** (Asama).

The **redfishlog** receiver polls BMC Redfish `LogService` entries (typically under `Managers` → `LogServices`) using the [gofish](https://github.com/stmcginnis/gofish) library. Credentials are loaded from an external YAML file (similar to a Prometheus `redfish` exporter style file) so secrets are not embedded in the main collector config.

## Configuration

```yaml
receivers:
  redfishlog:
    credentials_file: /etc/otel/redfish.yml
    storage: file_storage
    poll_interval: 1m
    timeout: 60s
    insecure_skip_verify: true
    initial_last_n: 500
    log_reset_clock_skew: 5m
    targets:
      - endpoint: https://10.0.0.1
        credential_key: "10.0.0.1"  # optional; defaults to hostname from endpoint

extensions:
  file_storage:
    directory: /var/lib/otelcol/file_storage

service:
  extensions: [file_storage]
  pipelines:
    logs:
      receivers: [redfishlog]
      exporters: [otlphttp]
```

### Credentials file

```yaml
default:
  username: readonly
  password: "secret"

hosts:
  "10.0.0.2":
    username: other
    password: "othersecret"
```

`credential_key` on a target must match a key under `hosts`; otherwise `default` is used.

### Checkpoints and log reset

Configure `storage` with the [file storage extension](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/storage/filestorage) so checkpoints survive restarts.

When there is no checkpoint, or when a **log clear / wrap** is detected (timestamp regression past skew, or numeric entry id regression), the receiver ingests only the **`initial_last_n`** newest entries, then resumes incremental collection.

Each log record includes a stable `redfish.entry_fingerprint` attribute for downstream deduplication (for example with the ClickHouse exporter `logs_dedup_key_attribute`).

## Telemetry mapping

- **Instrumentation scope** `ScopeName` and `Scope.Version` follow the OpenTelemetry logs data model for [InstrumentationScope](https://opentelemetry.io/docs/specs/otel/logs/data-model/#field-instrumentationscope) (values come from `mdatagen` and the collector build).
- **Resource attributes** use the `bmc.*` and `redfish.*` namespaces for the BMC endpoint and Redfish resources (not yet mapped to semantic conventions because Redfish-specific keys are clearer for operators).

## Stability

Alpha; logs pipeline only.
