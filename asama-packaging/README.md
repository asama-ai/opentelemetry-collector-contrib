# Asama packaging (host-agent collector)

This directory is **Asama-only**. It is not part of upstream OpenTelemetry Collector Contrib.
Do not merge these paths into upstream PRs.

It builds the **on-host** `asama-otel-collector` package (OTLP ingest `127.0.0.1:4319` →
`ASAMA_OTEL_ENDPOINT`). That distro is **not** `cmd/otel-collector/builder-config.yaml`
(stage-2 / ClickHouse / remote_write).

## Git tags (Debian-friendly)

Upstream collector components in this fork are **v0.141.0**. Asama package tags:

```text
v0.141.0-asama-test-001   # testing Pulp
v0.141.0-asama-001        # production Pulp
```

`X.Y.Z` is the collector component version (this fork: **0.141.0**), not host-agent `1.x.x`. Debian `Version` uses one hyphen (`0.141.0-asama.test.001` / `0.141.0-asama.001`). RPM `Version` is `0.141.0`; `Release` is `asama.test.001` or `asama.001`.

Workflow: `.github/workflows/publish-asama-host-otelcol.yml`
(does not replace `asama-build.yml`, which still builds GHCR images on plain `v*` tags).
