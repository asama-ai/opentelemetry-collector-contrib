#!/usr/bin/env python3
"""Pulp CLI wrapper for Console: spec paths are /pulp/, the proxy is /pulpui/pulp/.

pulp-cli --api-root is used to fetch api.json. Operation URLs in that spec are still
/pulp/api/v3/..., which Dex owns on console.asama.cloud. Rewrite those requests.
"""
from __future__ import annotations

import os
import sys
from urllib.parse import urlparse


def _netloc(base_url: str) -> str:
    parsed = urlparse(base_url if "://" in base_url else f"https://{base_url}")
    host = parsed.netloc or parsed.path.split("/")[0]
    if not host:
        raise SystemExit("PULP_CLI_BASE_URL is missing a host")
    return host


def _install_rewrite(base_url: str, api_root: str) -> tuple[str, str]:
    import requests

    if not api_root.startswith("/"):
        api_root = "/" + api_root
    if not api_root.endswith("/"):
        api_root = api_root + "/"

    host = _netloc(base_url)
    needle = f"{host}/pulp/"
    already = f"{host}{api_root}"
    orig = requests.Session.request

    def patched(self, method, url, *args, **kwargs):  # noqa: ANN001
        if isinstance(url, str) and needle in url and already not in url:
            url = url.replace(needle, already, 1)
        return orig(self, method, url, *args, **kwargs)

    requests.Session.request = patched  # type: ignore[method-assign]
    return needle, already


def main() -> None:
    cfg = os.environ.get("PULP_CLI_CONFIG", os.path.expanduser("~/.config/pulp/cli.toml"))
    base_url = os.environ.get("PULP_CLI_BASE_URL", "")
    api_root = os.environ.get("PULP_API_ROOT", "")
    if not base_url or not api_root:
        raise SystemExit("PULP_CLI_BASE_URL and PULP_API_ROOT must be set")

    needle, already = _install_rewrite(base_url, api_root)
    print(f"Pulp URL rewrite: {needle} -> {already}", file=sys.stderr)

    from pulp_cli import main as pulp_main

    argv = sys.argv[1:]
    sys.argv = [
        sys.argv[0],
        "--config",
        cfg,
        "--base-url",
        base_url,
        "--api-root",
        api_root,
        *argv,
    ]
    raise SystemExit(pulp_main())


if __name__ == "__main__":
    main()
