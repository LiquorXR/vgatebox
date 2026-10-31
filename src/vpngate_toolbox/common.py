"""Small shared IO and evidence primitives; no protocol implementations."""
from __future__ import annotations

import hashlib
import ipaddress
import json
import os
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timedelta, timezone
from pathlib import Path


class ToolError(Exception):
    """A safe, static error code suitable for public reports."""


class ServiceError(ToolError):
    """An upstream outage/rate limit, distinct from a rejected VPN node."""


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def expires_at(seconds: int) -> str:
    return (datetime.now(timezone.utc) + timedelta(seconds=seconds)).isoformat().replace("+00:00", "Z")


def is_fresh(value: str) -> bool:
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00")) > datetime.now(timezone.utc)
    except (ValueError, TypeError, AttributeError):
        return False


def digest(value) -> str:
    data = value if isinstance(value, bytes) else json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(data).hexdigest()


def public_ipv4(value) -> str:
    try:
        ip = ipaddress.ip_address(str(value).strip())
    except ValueError:
        raise ToolError("invalid_exit_ip") from None
    if ip.version != 4 or not ip.is_global:
        raise ToolError("non_public_ipv4")
    return str(ip)


def https_url(value: str) -> str:
    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme != "https" or not parsed.hostname or parsed.username or parsed.password or parsed.fragment:
        raise ToolError("https_url_required")
    return value


def atomic_write(path: Path, data: bytes, private=False):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=".tmp-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as handle:
            os.fchmod(handle.fileno(), 0o600 if private else 0o644)
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def write_json(path: Path, value, private=False):
    atomic_write(path, (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode(), private)


class HttpsRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        https_url(newurl)
        # Avoid forwarding credentials to a redirected host.
        sensitive_headers = {"authorization", "x-api-key", "cf-access-client-id", "cf-access-client-secret"}
        if (urllib.parse.urlsplit(req.full_url).netloc != urllib.parse.urlsplit(newurl).netloc
                and sensitive_headers.intersection(key.lower() for key in req.headers)):
            raise ServiceError("cross_host_redirect_rejected")
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def get_bytes(url: str, timeout=30, limit=12_000_000, headers=None) -> bytes:
    https_url(url)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), HttpsRedirect())
    deadline = time.monotonic() + timeout
    for attempt in range(2):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise ServiceError("https_timeout")
        request = urllib.request.Request(url, headers={"User-Agent": "VPNGate-Toolbox/0.1", **(headers or {})})
        try:
            with opener.open(request, timeout=remaining) as response:
                data = response.read(limit + 1)
            if len(data) > limit:
                raise ServiceError("response_too_large")
            return data
        except urllib.error.HTTPError as exc:
            if exc.code == 429:
                raise ServiceError("http_429") from None
            if exc.code >= 500 and attempt == 0:
                time.sleep(0.25)
                continue
            raise ServiceError("http_" + str(exc.code)) from None
        except (urllib.error.URLError, TimeoutError, OSError):
            if attempt == 0:
                time.sleep(0.25)
                continue
            raise ServiceError("https_unavailable") from None
    raise ServiceError("https_unavailable")


def get_json(url: str, timeout=30, headers=None):
    try:
        value = json.loads(get_bytes(url, timeout, 1_000_000, headers))
    except (ValueError, UnicodeError):
        raise ServiceError("invalid_json_response") from None
    if not isinstance(value, dict):
        raise ServiceError("object_response_required")
    return value
