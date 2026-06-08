#!/usr/bin/env bash
# Verify embedded BMC registry JSON under processor modules.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NORMALIZE_ROOT="${1:-${REPO_ROOT}/processor/bmceventnormalizeprocessor/registries}"
FAULT_ROOT="${2:-${REPO_ROOT}/processor/bmcfaultsignalprocessor/registries}"

normalize_required=(
	"asama-bmc-events.json"
	"mappings/index.json"
	"mappings/bundles/dell/idrac9/2.8.0.json"
	"mappings/bundles/hpe/ilo6/3.14.0.json"
	"mappings/bundles/lenovo/xcc/1.0.0.json"
)

fault_required=(
	"fault-eligible-events.json"
)

verify_tree() {
	local root="$1"
	shift
	if [[ ! -d "${root}" ]]; then
		echo "registry directory not found: ${root}" >&2
		exit 1
	fi
	for rel in "$@"; do
		if [[ ! -f "${root}/${rel}" ]]; then
			echo "missing required BMC asset: ${root}/${rel}" >&2
			exit 1
		fi
	done
}

verify_tree "${NORMALIZE_ROOT}" "${normalize_required[@]}"
verify_tree "${FAULT_ROOT}" "${fault_required[@]}"
echo "BMC registry assets OK"
