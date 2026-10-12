#!/usr/bin/env bash
set -euo pipefail

: "${PULP_BASE_URL:?PULP_BASE_URL is required}"
: "${PULP_PUBLISH_USERNAME:?PULP_PUBLISH_USERNAME is required}"
: "${PULP_PUBLISH_PASSWORD:?PULP_PUBLISH_PASSWORD is required}"

# Package publish uses nginx basic auth on /pulp-publish/. These are not the
# container-registry PULP_USERNAME / PULP_PASSWORD used by docker login.
if [[ "$PULP_PUBLISH_USERNAME" =~ [[:space:]] ]]; then
  echo "ERROR: PULP_PUBLISH_USERNAME must not contain spaces" >&2
  exit 1
fi

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
# /pulp-publish/pulp/ would be proxied to /pulp/pulp/ and 404. Nginx already adds /pulp/.
path_part=$(printf '%s' "$path_part" | sed -E 's|/*$||; s|/pulp-publish/pulp$|/pulp-publish|')

if [ -n "${PULP_API_ROOT:-}" ]; then
  api_root="$PULP_API_ROOT"
elif [ -n "$path_part" ] && [ "$path_part" != "/" ]; then
  # The path in PULP_BASE_URL is the API root. Do not append /pulp/.
  api_root=$(printf '%s' "$path_part" | sed -E 's|^/*|/|; s|/*$|/|')
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
python3 - "$cfg_home" "$click_cfg" "$base_url" "$api_root" "$PULP_PUBLISH_USERNAME" "$PULP_PUBLISH_PASSWORD" "$verify_ssl" << 'PY'
import base64
import sys
from pathlib import Path

cfg_home, click_cfg, base_url, api_root, username, password, verify_ssl = sys.argv[1:]

if not username or not password:
    raise SystemExit("PULP_PUBLISH_USERNAME and PULP_PUBLISH_PASSWORD are required")
if any(c.isspace() for c in username):
    raise SystemExit("PULP_PUBLISH_USERNAME must not contain spaces")

try:
    import tomli_w
except ImportError:
    raise SystemExit("tomli-w is required to write pulp-cli config")

# pulp-cli adds username/password only after it has read api.json.
# Nginx basic-auth protects that download, so the password must be on every request.
basic = base64.b64encode(f"{username}:{password}".encode()).decode()
doc = {
    "cli": {
        "base_url": base_url,
        "api_root": api_root,
        "domain": "default",
        "verify_ssl": verify_ssl == "true",
        "format": "json",
        "username": username,
        "password": password,
        "headers": [f"Authorization:Basic {basic}"],
    }
}

for path in {cfg_home, click_cfg}:
    p = Path(path)
    p.parent.mkdir(parents=True, exist_ok=True)
    p.parent.chmod(0o700)
    p.touch(mode=0o600, exist_ok=True)
    p.chmod(0o600)
    p.write_bytes(tomli_w.dumps(doc).encode())
    print(f"Wrote {p}")
PY

echo "Configured Pulp CLI for package user ${PULP_PUBLISH_USERNAME}"

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
    echo "Hint: PULP_BASE_URL must be https://console.asama.cloud/pulp-publish (nginx adds /pulp/)." >&2
    exit 1
  }
else
  echo "Notice: 'pulp' executable not yet found in PATH. Skipping local status check."
fi
