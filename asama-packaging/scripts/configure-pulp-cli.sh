#!/usr/bin/env bash
set -euo pipefail

: "${PULP_BASE_URL:?PULP_BASE_URL is required}"
: "${PULP_USERNAME:?PULP_USERNAME is required}"
: "${PULP_PASSWORD:?PULP_PASSWORD is required}"

# Ensure ~/.local/bin is in PATH for pulp binary
export PATH="$HOME/.local/bin:$PATH"
if [ -n "${GITHUB_PATH:-}" ]; then
  echo "$HOME/.local/bin" >> "$GITHUB_PATH"
fi

raw_url="$PULP_BASE_URL"
if [[ "$raw_url" != http://* && "$raw_url" != https://* ]]; then
  raw_url="https://${raw_url}"
fi

# pulp-cli requires base_url to be '<scheme>://<netloc>' without a path
base_url=$(printf '%s' "$raw_url" | sed -E 's|^(https?://[^/]+).*|\1|')
path_part=$(printf '%s' "$raw_url" | sed -E 's|^https?://[^/]+(/.*)?$|\1|')

# Determine api_root (e.g. /pulpui/pulp/ or /pulp/)
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

# Ensure api_root begins and ends with /
case "$api_root" in /*/) ;; */) api_root="/${api_root}";; /*) api_root="${api_root}/";; *) api_root="/${api_root}/";; esac

verify_ssl="true"
if [ "${PULP_VERIFY_SSL:-true}" = "false" ] || [ "${PULP_VERIFY_SSL:-}" = "0" ]; then
  verify_ssl="false"
fi

mkdir -p ~/.config/pulp
cat <<EOF > ~/.config/pulp/cli.toml
[cli]
base_url = "${base_url}"
username = "${PULP_USERNAME}"
password = "${PULP_PASSWORD}"
api_root = "${api_root}"
domain = "default"
verify_ssl = ${verify_ssl}
format = "json"
EOF

echo "Configured ~/.config/pulp/cli.toml:"
# Do not print password.
sed '/^password = /d' ~/.config/pulp/cli.toml

# pulp-cli api_root is the supported reverse-proxy setting. pulp-glue's OpenAPI
# client still urljoins '/pulp/api/v3/...' onto base_url; rewrite only when the
# configured api_root is not the default /pulp/.
export PULP_PATCH_NETLOC="${base_url#*://}"
export PULP_PATCH_API_ROOT="$api_root"

python3 - << 'PYEOF'
import importlib.util
import os
import re
import site
import sys

netloc = os.environ.get("PULP_PATCH_NETLOC", "").rstrip("/")
api_root = os.environ.get("PULP_PATCH_API_ROOT", "/pulp/")
if not api_root.startswith("/"):
    api_root = "/" + api_root
if not api_root.endswith("/"):
    api_root = api_root + "/"

# Default Pulp path needs no rewrite; cli.toml api_root is enough.
if api_root == "/pulp/" or not netloc:
    print("Pulp API root is /pulp/; skipping reverse-proxy URL rewrite hook")
    sys.exit(0)

needle = f"{netloc}/pulp/"
replacement = f"{netloc}{api_root}"
already = f"{netloc}{api_root}"

patch_code = f"""try:
    import requests
    _orig_send = requests.Session.send

    def _patched_send(self, request, **kwargs):
        if hasattr(request, "url") and request.url:
            url = request.url
            if {needle!r} in url and {already!r} not in url:
                request.url = url.replace({needle!r}, {replacement!r}, 1)
        return _orig_send(self, request, **kwargs)

    requests.Session.send = _patched_send
except Exception:
    pass
"""

site_dirs = []
try:
    user_site = site.getusersitepackages()
    if user_site:
        site_dirs.append(user_site)
except Exception:
    pass
try:
    site_dirs.extend(site.getsitepackages())
except Exception:
    pass
for p in sys.path:
    if ("site-packages" in p or "dist-packages" in p) and p not in site_dirs:
        site_dirs.append(p)

installed = False
for d in site_dirs:
    if not os.path.isdir(d):
        continue
    try:
        with open(os.path.join(d, "pulp_reverse_proxy_patch.py"), "w") as f:
            f.write(patch_code.strip() + "\n")
        with open(os.path.join(d, "pulp_reverse_proxy.pth"), "w") as f:
            f.write("import pulp_reverse_proxy_patch\n")
        print(f"Installed Pulp reverse-proxy routing hook in {d}")
        installed = True
        break
    except OSError:
        continue

patched_openapi = False
spec = None
try:
    spec = importlib.util.find_spec("pulp_glue.common.openapi")
except Exception:
    spec = None

if spec and spec.origin:
    try:
        with open(spec.origin, "r") as f:
            content = f.read()
        if already in content:
            patched_openapi = True
        else:
            match = re.search(r"def _send_request\s*\(", content)
            if match:
                insert = (
                    "\n        if hasattr(request, \"url\") and request.url:\n"
                    f"            if {needle!r} in request.url and {already!r} not in request.url:\n"
                    f"                request.url = request.url.replace({needle!r}, {replacement!r}, 1)\n"
                )
                brace = content.find(":", match.end())
                if brace != -1:
                    content = content[: brace + 1] + insert + content[brace + 1 :]
                    with open(spec.origin, "w") as f:
                        f.write(content)
                    print(f"Directly patched {spec.origin}")
                    patched_openapi = True
    except OSError as e:
        print(f"Notice: Could not patch openapi.py directly: {e}")

if not installed and not patched_openapi:
    print(
        "ERROR: Failed to install reverse-proxy URL rewrite for "
        f"{needle} -> {replacement}. Set PULP_API_ROOT to match the proxy, "
        "or use a Pulp base URL whose path is already /pulp/.",
        file=sys.stderr,
    )
    sys.exit(1)
PYEOF

echo "Testing Pulp server status via Pulp CLI..."
if command -v pulp >/dev/null 2>&1; then
  pulp status || {
    echo "ERROR: 'pulp status' failed to connect to Pulp at ${base_url}${api_root}" >&2
    exit 1
  }
else
  echo "Notice: 'pulp' executable not yet found in PATH. Skipping local status check."
fi
