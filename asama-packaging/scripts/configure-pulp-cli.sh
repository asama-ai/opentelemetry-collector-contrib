#!/usr/bin/env bash
set -euo pipefail

: "${PULP_BASE_URL:?PULP_BASE_URL is required}"
: "${PULP_USERNAME:?PULP_USERNAME is required}"
: "${PULP_PASSWORD:?PULP_PASSWORD is required}"

export PATH="$HOME/.local/bin:$PATH"
if [ -n "${GITHUB_PATH:-}" ]; then
  echo "$HOME/.local/bin" >> "$GITHUB_PATH"
fi

raw_url="$PULP_BASE_URL"
if [[ "$raw_url" != http://* && "$raw_url" != https://* ]]; then
  raw_url="https://${raw_url}"
fi

# pulp-cli requires base_url to be '<scheme>://<netloc>' with no path.
base_url=$(printf '%s' "$raw_url" | sed -E 's|^(https?://[^/]+).*|\1|')
path_part=$(printf '%s' "$raw_url" | sed -E 's|^https?://[^/]+(/.*)?$|\1|')

if [ -n "${PULP_API_ROOT:-}" ]; then
  api_root="$PULP_API_ROOT"
elif echo "$path_part" | grep -q "pulpui"; then
  api_root="/pulpui/pulp/"
elif [ -n "$path_part" ] && [ "$path_part" != "/" ]; then
  clean_path=$(printf '%s' "$path_part" | sed -E 's|^/*|/|; s|/*$|/|')
  if [[ "$clean_path" == */pulp/ ]]; then
    api_root="$clean_path"
  else
    api_root="${clean_path}pulp/"
  fi
else
  api_root="/pulp/"
fi

case "$api_root" in /*/) ;; */) api_root="/${api_root}";; /*) api_root="${api_root}/";; *) api_root="/${api_root}/";; esac

verify_ssl="true"
if [ "${PULP_VERIFY_SSL:-true}" = "false" ] || [ "${PULP_VERIFY_SSL:-}" = "0" ]; then
  verify_ssl="false"
fi

# Click looks in get_app_dir("pulp")/cli.toml, which is ~/.config/pulp on Linux
# unless XDG_CONFIG_HOME is set. Write both that path and ~/.config/pulp.
click_cfg="$(python3 -c 'from pathlib import Path; import click; print(Path(click.utils.get_app_dir("pulp")) / "cli.toml")')"
cfg_home="${HOME}/.config/pulp/cli.toml"

# Bare `format = json` is invalid TOML; pulp-cli then uses default api_root=/pulp/
# and Console Dex-redirects /pulp/api/v3/status/.
python3 - "$cfg_home" "$click_cfg" "$base_url" "$api_root" "$PULP_USERNAME" "$verify_ssl" << 'PY'
import os
import sys
from pathlib import Path

cfg_home, click_cfg, base_url, api_root, username, verify_ssl = sys.argv[1:]
password = os.environ["PULP_PASSWORD"]
try:
    import tomli_w
except ImportError:
    tomli_w = None

doc = {
    "cli": {
        "base_url": base_url,
        "username": username,
        "password": password,
        "api_root": api_root,
        "domain": "default",
        "verify_ssl": verify_ssl == "true",
        "format": "json",
    }
}
if tomli_w is None:
    raise SystemExit("tomli-w is required to write pulp-cli config")
for path in {cfg_home, click_cfg}:
    p = Path(path)
    p.parent.mkdir(parents=True, exist_ok=True)
    p.parent.chmod(0o700)
    p.touch(mode=0o600, exist_ok=True)
    p.chmod(0o600)
    p.write_bytes(tomli_w.dumps(doc).encode())
    print(f"Wrote {p}")
PY

echo "Configured Pulp CLI (password omitted):"
python3 - "$cfg_home" << 'PY'
import sys
from pathlib import Path
try:
    import tomllib
except ImportError:
    import tomli as tomllib
cfg = tomllib.loads(Path(sys.argv[1]).read_text())
cfg.get("cli", {}).pop("password", None)
for k, v in cfg.get("cli", {}).items():
    print(f"  {k} = {v!r}")
PY

export PULP_CLI_CONFIG="$cfg_home"
export PULP_CLI_BASE_URL="$base_url"
export PULP_API_ROOT="$api_root"
if [ -n "${GITHUB_ENV:-}" ]; then
  echo "PULP_CLI_CONFIG=${cfg_home}" >> "$GITHUB_ENV"
  echo "PULP_CLI_BASE_URL=${base_url}" >> "$GITHUB_ENV"
  echo "PULP_API_ROOT=${api_root}" >> "$GITHUB_ENV"
fi

echo "Testing Pulp server status via Pulp CLI (${base_url}${api_root}api/v3/status/)..."
if command -v pulp >/dev/null 2>&1; then
  SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
  "$SCRIPT_DIR/pulp-asama.sh" status || {
    echo "ERROR: 'pulp status' failed to connect to Pulp at ${base_url}${api_root}" >&2
    echo "Hint: Console serves Pulp at /pulpui/pulp/; /pulp/ is Dex. Pass --api-root, not only cli.toml." >&2
    exit 1
  }
else
  echo "Notice: 'pulp' executable not yet found in PATH. Skipping local status check."
fi
