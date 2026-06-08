#!/usr/bin/env bash
# Verify bmc-registries has the JSON assets required for otel-exporter images.
set -euo pipefail

ROOT="${1:-${BMC_REGISTRIES_PATH:-bmc-registries}}"

required=(
	"asama-bmc-events.json"
	"fault-eligible-events.json"
	"mappings/index.json"
	"mappings/bundles/dell/idrac9/2.8.0.json"
	"mappings/bundles/hpe/ilo6/3.14.0.json"
	"mappings/bundles/lenovo/xcc/1.0.0.json"
)

if [[ ! -d "${ROOT}" ]]; then
	echo "bmc-registries not found at ${ROOT}" >&2
	echo "Set BMC_REGISTRIES_PATH or checkout asama-ai/bmc-registries next to this repo." >&2
	exit 1
fi

for rel in "${required[@]}"; do
	if [[ ! -f "${ROOT}/${rel}" ]]; then
		echo "missing required BMC asset: ${ROOT}/${rel}" >&2
		exit 1
	fi
done

echo "BMC registry assets OK at ${ROOT}"
