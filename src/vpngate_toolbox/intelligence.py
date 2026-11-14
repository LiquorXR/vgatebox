"""Optional independent HTTPS intelligence lookup with per-IP cache and budget."""
from __future__ import annotations

import json
import os
import re
from pathlib import Path
from urllib.parse import quote

from .classifier import classify
from .common import ServiceError, ToolError, digest, expires_at, get_json, https_url, is_fresh, public_ipv4, write_json

# Single switch for convenient replacement. Empty/auto keeps legacy behaviour
# (IPPURE_ENABLED + template) so existing deployments keep working.
PROVIDERS = ("auto", "ippure", "custom", "off")
AUTH_MODES = ("auto", "none", "header", "bearer", "query")


def intelligence_provider() -> str:
    raw = os.getenv("INTELLIGENCE_PROVIDER", "").strip().lower()
    if raw in ("", "auto"):
        return "auto"
    if raw == "disabled":
        return "off"
    if raw in PROVIDERS:
        return raw
    raise ToolError("intelligence_provider_invalid")


def auth_config() -> dict:
    mode = os.getenv("INTELLIGENCE_AUTH_MODE", "").strip().lower().replace("_", "-")
    if mode in ("", "auto"):
        mode = "auto"
    elif mode in ("x-api-key", "x-apikey", "api-key"):
        mode = "header"
    if mode not in AUTH_MODES:
        raise ToolError("intelligence_auth_mode_invalid")
    header = os.getenv("INTELLIGENCE_AUTH_HEADER", "").strip() or "X-API-Key"
    if not re.fullmatch(r"[A-Za-z0-9-]{1,64}", header):
        raise ToolError("intelligence_auth_header_invalid")
    param = os.getenv("INTELLIGENCE_TOKEN_PARAM", "").strip() or "token"
    if not re.fullmatch(r"[A-Za-z0-9_\-]{1,64}", param):
        raise ToolError("intelligence_token_param_invalid")
    token = os.getenv("INTELLIGENCE_TOKEN", "")
    if token.strip() == "":
        token = ""
    if mode in ("header", "bearer", "query") and not token:
        raise ToolError("intelligence_token_required")
    if mode == "auto":
        mode = "header" if token else "none"
    if mode == "none":
        return {"mode": "none", "header": header, "param": param, "token": ""}
    return {"mode": mode, "header": header, "param": param, "token": token}


def build_request(template: str, ip: str) -> tuple[str, dict]:
    if "{ip}" not in template:
        raise ToolError("intelligence_ip_placeholder_required")
    https_url(template.replace("{ip}", "1.1.1.1"))
    auth = auth_config()
    base = template.replace("{ip}", quote(ip))
    if auth["mode"] == "query":
        sep = "&" if "?" in base else "?"
        url = base + sep + quote(auth["param"]) + "=" + quote(auth["token"])
        return url, {}
    if auth["mode"] == "bearer":
        return base, {"Authorization": "Bearer " + auth["token"]}
    if auth["mode"] == "header":
        return base, {auth["header"]: auth["token"]}
    return base, {}


def describe_intelligence_config() -> dict:
    provider = intelligence_provider()
    template = os.getenv("INTELLIGENCE_URL_TEMPLATE", "")
    auth = auth_config()
    # Never echo the token or a token-bearing URL.
    info = {"provider": provider, "template_configured": bool(template.strip()),
            "auth_mode": auth["mode"], "auth_header": auth["header"],
            "token_param": auth["param"], "token_configured": bool(auth["token"]),
            "ippure_enabled": os.getenv("IPPURE_ENABLED", "").strip() == "1"}
    if template.strip():
        try:
            https_url(template.replace("{ip}", "1.1.1.1"))
            if "{ip}" not in template:
                raise ToolError("intelligence_ip_placeholder_required")
        except ToolError as exc:
            info["template_error"] = str(exc)
        else:
            info["template_error"] = ""
    else:
        info["template_error"] = "" if provider in ("auto", "ippure", "off") else "intelligence_not_configured"
    return info


def enrich(nodes: list, settings: dict, cache_path: Path, transport=get_json, protocol_name="sstp"):
    provider_mode = intelligence_provider()
    if provider_mode == "off":
        return {"enabled": False, "queries": 0, "service_errors": []}
    template = os.getenv("INTELLIGENCE_URL_TEMPLATE", "")
    if not template.strip():
        if provider_mode == "custom":
            raise ToolError("intelligence_not_configured")
        return {"enabled": False, "queries": 0, "service_errors": []}
    if "{ip}" not in template:
        raise ToolError("intelligence_ip_placeholder_required")
    https_url(template.replace("{ip}", "1.1.1.1"))
    # Validate auth early so misconfiguration fails fast without a query.
    auth_config()
    try:
        cache = json.loads(cache_path.read_text())
    except (FileNotFoundError, ValueError):
        cache = {}
    if not isinstance(cache, dict):
        cache = {}
    stats = {"enabled": True, "queries": 0, "service_errors": []}
    provider = "independent_https_" + digest(template.encode())[:12]
    unavailable = False
    for node in nodes:
        protocol = node["protocols"][protocol_name]
        if protocol["status"] != "passed":
            continue
        ip = protocol["expected_exit_ip"]
        key = provider + ":" + ip
        entry = cache.get(key, {})
        if not isinstance(entry, dict):
            entry = {}
        try:
            cached = is_fresh(entry.get("expires_at")) and isinstance(entry.get("raw"), dict)
            if cached:
                raw = entry["raw"]
            elif not unavailable and stats["queries"] < settings["intelligence_query_limit"]:
                stats["queries"] += 1
                url, headers = build_request(template, ip)
                raw = transport(url, timeout=settings["node_timeout_seconds"], headers=headers)
            else:
                protocol["intelligence_error"] = "intelligence_budget_or_service_unavailable"
                continue
            image = classify(raw, provider, ip)
            image["identity_assurance"] = "normal_https_certificate_validation"
            image["expires_at"] = entry["expires_at"] if cached else expires_at(settings["intelligence_ttl_seconds"])
            if cached:
                image["queried_at"] = entry.get("queried_at")
                image["cache_used"] = True
            protocol["egress"] = image
            if not cached:
                cache[key] = {"raw": raw, "queried_at": image["queried_at"],
                              "expires_at": image["expires_at"]}
        except (ServiceError, ToolError) as exc:
            protocol["intelligence_error"] = str(exc)
            stats["service_errors"].append(str(exc))
            # Do not hammer the provider after 429 or an outage; keep prior evidence labelled.
            unavailable = True
    write_json(cache_path, cache, private=True)
    return stats


def profile_measured_exit(ip: str, settings: dict, cache_path: Path, protocol_name: str, transport=get_json) -> dict:
    if protocol_name not in ("openvpn", "sstp", "socks5"):
        raise ToolError("unsupported_profile_protocol")
    ip = public_ipv4(ip)
    protocol = {"status": "passed", "expected_exit_ip": ip}
    try:
        stats = enrich([{"protocols": {protocol_name: protocol}}], settings, cache_path,
                       transport, protocol_name=protocol_name)
    except ToolError as exc:
        stats = {"enabled": True}
        protocol["intelligence_error"] = str(exc)
    if "egress" in protocol:
        return {**protocol["egress"], "protocol": protocol_name, "intelligence_status": "passed"}
    error = protocol.get("intelligence_error", "intelligence_not_configured" if not stats["enabled"]
                         else "intelligence_unavailable")
    template = os.getenv("INTELLIGENCE_URL_TEMPLATE", "")
    provider = None
    if stats.get("enabled") and template.strip():
        provider = "independent_https_" + digest(template.encode())[:12]
    return {"ip": ip, "protocol": protocol_name, "country": None, "asn": None, "isp": None,
            "ip_type": "unknown", "hosting": None, "mobile": None, "residential": None,
            "proxy": None, "vpn": None, "abuse": None, "confidence": "unknown",
            "reason": error, "intelligence_error": error,
            "provider": provider,
            "intelligence_status": "not_configured" if not stats["enabled"] else "failed"}
