#!/usr/bin/env bash
set -eu
ROOT="${1:-$(cd "$(dirname "$0")/../.." && pwd)}"
BINDIR="${ROOT}/build/bin"
f="${BINDIR}/otelcontribcol-otel-collector"
[ -f "$f" ] || exit 0
command -v strip >/dev/null 2>&1 || exit 0
strip --strip-unneeded "$f" 2>/dev/null || strip "$f" 2>/dev/null || true
