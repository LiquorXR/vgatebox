#!/usr/bin/env python3
"""Publish the credential-free manifest to one Cloudflare KV key.

The request is intentionally a single PUT: readers either see the previous
complete manifest or the new complete manifest, never a partially written
file.  The script never prints the API token or manifest contents.
"""
from __future__ import annotations

import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path


def fail(message: str):
    raise SystemExit("publish_cf_manifest: " + message)


def main() -> int:
    path = Path(os.getenv("CF_MANIFEST_PATH", "data/cf_manifest.json"))
    if not path.is_file():
        fail("manifest_missing")
    try:
        manifest = json.loads(path.read_text())
    except (OSError, ValueError):
        fail("manifest_invalid_json")
    if manifest.get("schema_version") != 1 or manifest.get("kind") != "vpngate_cf_validated_nodes":
        fail("manifest_schema_mismatch")
    if not isinstance(manifest.get("nodes"), list) or not manifest["nodes"]:
        fail("manifest_empty")
    try:
        expiry = datetime.fromisoformat(manifest["expires_at"].replace("Z", "+00:00"))
    except (AttributeError, TypeError, ValueError):
        fail("manifest_expiry_invalid")
    if expiry <= datetime.now(timezone.utc):
        fail("manifest_expired")
    flattened = json.dumps(manifest, ensure_ascii=False).lower()
    if any(secret in flattened for secret in ('"password"', '"token"', '"secret"', '"vless_uuid"')):
        fail("manifest_contains_secret_field")
    token = os.getenv("CF_API_TOKEN", "")
    account = os.getenv("CF_ACCOUNT_ID", "")
    namespace = os.getenv("CF_KV_NAMESPACE_ID", "")
    if not token or not re.fullmatch(r"[A-Za-z0-9_-]{8,128}", account) or not re.fullmatch(r"[A-Za-z0-9_-]{8,128}", namespace):
        fail("cloudflare_credentials_missing")
    key = urllib.parse.quote("manifest:current", safe="")
    url = f"https://api.cloudflare.com/client/v4/accounts/{account}/storage/kv/namespaces/{namespace}/values/{key}"
    request = urllib.request.Request(url, data=path.read_bytes(), method="PUT", headers={
        "Authorization": "Bearer " + token,
        "Content-Type": "application/json",
        "User-Agent": "VPNGate-Toolbox/0.1",
    })
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            payload = json.loads(response.read(1_000_000))
    except (urllib.error.URLError, TimeoutError, ValueError, OSError):
        fail("cloudflare_request_failed")
    if not isinstance(payload, dict) or payload.get("success") is not True:
        fail("cloudflare_rejected")
    print(f"Published {len(manifest['nodes'])} validated nodes to manifest:current")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except SystemExit:
        raise
