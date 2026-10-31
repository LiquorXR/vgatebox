"""HTTP adapter for an independently deployed CheckSocks5 Worker."""
from __future__ import annotations

import os
import re
import time
from urllib.parse import quote, urlencode

from .classifier import classify
from .common import ServiceError, ToolError, get_json, public_ipv4, utc_now


def proxy_authority(candidate: dict, environment: dict) -> str:
    return (quote(environment["username"], safe="") + ":" + quote(environment["password"], safe="") + "@" +
            candidate["host"] + ":" + str(candidate["port"]))


def checker_headers() -> dict:
    headers = {}
    # Reuse the VLESS UUID when a separate checker token is not configured.
    # This keeps the single-Worker deployment from failing after a token
    # rotation while preserving CHECKER_TOKEN compatibility for old installs.
    token = os.getenv("CHECKER_TOKEN") or os.getenv("VLESS_UUID")
    if token:
        headers["Authorization"] = "Bearer " + token
    for env_key, header in (("CF_ACCESS_CLIENT_ID", "CF-Access-Client-Id"),
                            ("CF_ACCESS_CLIENT_SECRET", "CF-Access-Client-Secret")):
        if os.getenv(env_key):
            headers[header] = os.environ[env_key]
    return headers


def check_node(node: dict, environment: dict, settings: dict, transport=get_json) -> dict:
    protocol = node["protocols"]["sstp"]
    failures = []
    deadline = time.monotonic() + settings["node_timeout_seconds"]
    for candidate in protocol["candidates"]:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            failures.append("node_deadline_exceeded")
            break
        url = environment["checker_url"] + "?" + urlencode({"proxy": "sstp://" + proxy_authority(candidate, environment)})
        try:
            response = transport(url, timeout=remaining, headers=checker_headers())
            if response.get("type") != "sstp":
                raise ServiceError("checker_protocol_mismatch")
            if response.get("success") is not True:
                error = str(response.get("error", ""))
                if any(path in error for path in ("/api/lookup", "/json")) and re.search(r"\b(429|5[0-9]{2})\b", error):
                    raise ServiceError("checker_intelligence_service_unavailable" if "/api/lookup" in error else "checker_echo_service_unavailable")
                # Do not persist upstream error text: it may contain credentials/URLs.
                failures.append("sstp_rejected")
                continue
            if response.get("hostname", "").casefold() != candidate["host"].casefold() or response.get("port") != candidate["port"]:
                raise ServiceError("checker_candidate_mismatch")
            raw = response.get("exit")
            if not isinstance(raw, dict):
                raise ServiceError("checker_exit_missing")
            expected = public_ipv4(raw.get("ip"))
            latency = response.get("responseTime")
            if type(latency) not in (int, float) or not 0 <= latency <= settings["node_timeout_seconds"] * 1000:
                latency = None
            protocol.update({"status": "passed", "host": candidate["host"], "port": candidate["port"],
                             "expected_exit_ip": expected, "latency_ms": latency,
                             "egress": classify(raw, settings, "checker-worker", expected),
                             "observed_by": "checker-worker", "verified_at": utc_now(),
                             "baseline_assurance": "preliminary_checker_tls_T1"})
            protocol["egress"]["identity_assurance"] = "preliminary_checker_tls_T1"
            return node
        except ServiceError as exc:
            protocol.update({"status": "not_tested", "service_error": str(exc)})
            return node
        except ToolError as exc:
            failures.append(str(exc))
    protocol.update({"status": "failed", "failure_codes": failures, "verified_at": utc_now()})
    return node
