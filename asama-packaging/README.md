# Asama packaging (host-agent collector)

This directory is **Asama-only**. It is not part of upstream OpenTelemetry Collector Contrib.
Do not merge these paths into upstream PRs.

It builds the **on-host** `asama-otel-collector` package (OTLP ingest `127.0.0.1:4319` →
`ASAMA_OTEL_ENDPOINT`). That distro is **not** `cmd/otel-collector/builder-config.yaml`
(stage-2 / ClickHouse / remote_write).

## Git tags (Debian-friendly)

Upstream collector components in this fork are **v0.141.0**. Asama package tags:

```text
v0.141.0-asama-001
v0.141.0-asama.1
```

Debian `Version` becomes `0.141.0-asama.001` (one hyphen: upstream `0.141.0`, revision `asama.001`).
RPM cannot use a hyphen in `Version`; CI uses `Version: 0.141.0` and `Release: asama.001`.

Workflow: `.github/workflows/publish-asama-host-otelcol.yml`
(does not replace `asama-build.yml`, which still builds GHCR images on plain `v*` tags).
