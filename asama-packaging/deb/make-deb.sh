#!/bin/bash
set -euo pipefail

if [ $# -lt 1 ]; then
    echo "Usage: $0 <working_directory> [bin_directory]"
    echo "Example: $0 deb/asama-otel-collector_0.141.0-asama.001-1_amd64"
    exit 1
fi

DEB_DIR="$1"
BIN_DIR="${2:-build/bin}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
PKG="${ROOT}/asama-packaging"

echo "Creating otel-collector DEB package structure in ${DEB_DIR} (binaries from ${BIN_DIR})..."

mkdir -p "${DEB_DIR}/opt/asama.ai/bin"
mkdir -p "${DEB_DIR}/opt/asama.ai/config"
mkdir -p "${DEB_DIR}/etc/systemd/system"

if [ ! -f "${BIN_DIR}/otelcontribcol-otel-collector" ]; then
    echo "ERROR: ${BIN_DIR}/otelcontribcol-otel-collector not found"
    exit 1
fi

cp "${BIN_DIR}/otelcontribcol-otel-collector" "${DEB_DIR}/opt/asama.ai/bin/"
cp "${PKG}/config/otel-collector-config.yaml" "${DEB_DIR}/opt/asama.ai/config/"
cp "${PKG}/systemd/asama-otel-collector.service" "${DEB_DIR}/etc/systemd/system/"

echo "otel-collector DEB package structure prepared successfully"
