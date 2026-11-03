"""Shared IP intelligence normalization. Missing booleans remain null."""
from __future__ import annotations

import re

from .common import ToolError, public_ipv4, utc_now


RULE_VERSION = "residential-2"


def flag(value):
    return value if type(value) is bool else None


def classify(raw: dict, provider: str, expected_ip: str) -> dict:
    if public_ipv4(raw.get("ip")) != expected_ip:
        raise ToolError("intelligence_ip_mismatch")
    privacy = raw.get("privacy") if isinstance(raw.get("privacy"), dict) else {}
    network = raw.get("asn") if isinstance(raw.get("asn"), dict) else {}
    company = raw.get("company") if isinstance(raw.get("company"), dict) else {}
    hosting = flag(privacy.get("is_hosting"))
    mobile = flag(privacy.get("is_mobile", raw.get("is_mobile")))
    residential = flag(privacy.get("is_residential", raw.get("is_residential")))
    kinds = {str(value).lower() for value in (network.get("type"), company.get("type"), raw.get("network_type")) if value}
    asn = str(network.get("asn") or "").upper()
    if asn.isdigit():
        asn = "AS" + asn
    if not re.fullmatch(r"AS[0-9]+", asn):
        asn = None
    isp = network.get("name") or network.get("org") or company.get("name") or raw.get("isp")
    if not isinstance(isp, str):
        isp = None
    country = raw.get("country_code")
    if isinstance(raw.get("country"), dict):
        country = country or raw["country"].get("iso_code")
    if not isinstance(country, str) or not re.fullmatch(r"[A-Z]{2}", country.upper()):
        country = None
    else:
        country = country.upper()
    non_residential = bool(kinds.intersection(("hosting", "datacenter", "data center", "mobile", "cellular", "business", "enterprise", "education", "government")))
    conflict = residential is True and (hosting is True or mobile is True or non_residential)
    if conflict:
        ip_type, reason, confidence = "unknown", "conflicting_network_evidence", "unknown"
    elif residential is True and hosting is not True and mobile is not True and country and asn and isp:
        ip_type, reason, confidence = "strict_residential", "explicit_residential", "high"
    elif residential is False:
        ip_type, reason, confidence = "unknown", "explicit_not_residential", "high"
    elif not (country and asn and isp):
        ip_type, reason, confidence = "unknown", "insufficient_fields", "unknown"
    else:
        ip_type, reason, confidence = "unknown", "residential_unproven", "unknown"
    city = raw.get("city")
    if isinstance(city, dict):
        names = city.get("names")
        city = city.get("name") or (names.get("en") if isinstance(names, dict) else None)
    if not isinstance(city, str) or not city.strip() or len(city) > 100 or any(ord(c) < 32 for c in city):
        city = None
    return {"ip": expected_ip, "country": country, "city": city, "asn": asn, "isp": isp, "ip_type": ip_type,
            "hosting": hosting, "mobile": mobile, "residential": residential,
            "proxy": flag(privacy.get("is_proxy")), "vpn": flag(privacy.get("is_vpn")),
            "abuse": flag(privacy.get("is_abuser")), "reason": reason, "confidence": confidence,
            "provider": provider, "queried_at": utc_now(), "rule_version": RULE_VERSION,
            "raw": raw}
