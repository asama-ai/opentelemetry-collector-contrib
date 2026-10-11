#!/usr/bin/env bash
set -euo pipefail

: "${PULP_BASE_URL:?PULP_BASE_URL is required}"
: "${PULP_REMOTE_USER:?PULP_REMOTE_USER is required}"

# Nginx authenticates this identity and sets REMOTE-USER for Pulp.
# Do not send Pulp basic-auth credentials from CI.
if [[ ! "$PULP_REMOTE_USER" =~ ^[A-Za-z0-9._+@-]+$ ]]; then
  echo "ERROR: invalid PULP_REMOTE_USER" >&2
  exit 1
fi
if [ -n "${PULP_GITHUB_ACTOR:-}" ] && [[ ! "$PULP_GITHUB_ACTOR" =~ ^[A-Za-z0-9-]+$ ]]; then
  echo "ERROR: invalid PULP_GITHUB_ACTOR" >&2
  exit 1
fi
if [ -n "${PULP_USER_EMAIL:-}" ] && [[ ! "$PULP_USER_EMAIL" =~ ^[A-Za-z0-9._+@-]+$ ]]; then
  echo "ERROR: invalid PULP_USER_EMAIL" >&2
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
python3 - "$cfg_home" "$click_cfg" "$base_url" "$api_root" "$PULP_REMOTE_USER" "${PULP_GITHUB_ACTOR:-}" "${PULP_USER_EMAIL:-}" "$verify_ssl" << 'PY'
import sys
from pathlib import Path

cfg_home, click_cfg, base_url, api_root, remote_user, actor, email, verify_ssl = sys.argv[1:]
try:
    import tomli_w
except ImportError:
    tomli_w = None

# Nginx pulp-ci-auth.lua validates X-Asama-User-Email with CISS, then sets
# REMOTE-USER. pulp-cli raises "No suitable auth scheme found" unless some
# Authorization header is present, because Pulp's OpenAPI spec still lists
# basic auth. The value is not a Pulp password. The publish location clears
# Authorization before the request reaches Pulp.
publisher = email or remote_user
if "@" not in publisher:
    raise SystemExit("publisher email is required for CISS validation")
headers = [
    f"X-Asama-User-Email:{publisher}",
    "Authorization:Bearer external",
]
if actor:
    headers.append(f"X-Asama-GitHub-Actor:{actor}")

doc = {
    "cli": {
        "base_url": base_url,
        "api_root": api_root,
        "domain": "default",
        "verify_ssl": verify_ssl == "true",
        "format": "json",
        "headers": headers,
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

echo "Configured Pulp CLI (no basic-auth credentials):"
python3 - "$cfg_home" << 'PY'
import sys
from pathlib import Path
try:
    import tomllib
except ImportError:
    import tomli as tomllib
cfg = tomllib.loads(Path(sys.argv[1]).read_text())
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
    echo "Hint: PULP_BASE_URL must include the nginx API path. /pulp/ on this host is Dex." >&2
    exit 1
  }
else
  echo "Notice: 'pulp' executable not yet found in PATH. Skipping local status check."
fi
