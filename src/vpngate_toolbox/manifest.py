"""Build the public Cloudflare manifest from already verified Mode A nodes.

The manifest is deliberately smaller than ``mode_a_validated.json``.  It is
safe to publish to a KV namespace: credentials, VLESS UUIDs, checker tokens,
raw profiles, and Xray configuration never enter this file.
"""
from __future__ import annotations

from datetime import datetime
from typing import Optional


def _timestamp(value: Optional[str]) -> datetime:
    return datetime.fromisoformat((value or "").replace("Z", "+00:00"))


def _minimum_expiry(nodes: list[dict]) -> Optional[str]:
    values = [node.get("mode_a", {}).get("expires_at") for node in nodes]
    values = [value for value in values if value]
    return min(values, key=_timestamp) if values else None


def _public_node(node: dict) -> dict:
    protocol = node["protocols"]["sstp"]
    egress = protocol.get("egress") or {}
    mode_a = node["mode_a"]
    # Keep this list explicit.  A future field added to the internal pool must
    # not silently become public (profiles and checker evidence are private).
    return {
        "node_id": node["node_id"],
        "hostname": node["hostname"],
        "country": node.get("advertised_country"),
        "sstp_host": protocol["host"],
        "sstp_port": protocol["port"],
        "expected_exit_ip": protocol["expected_exit_ip"],
        "actual_exit_ip": mode_a["actual_exit_ip"],
        "asn": egress.get("asn"),
        "isp": egress.get("isp"),
        "ip_type": egress.get("ip_type", "unknown"),
        "latency_ms": protocol.get("latency_ms"),
        "verified_at": mode_a["verified_at"],
        "expires_at": mode_a["expires_at"],
    }


def build_cf_manifest(nodes: list[dict], generated_at: str, settings: dict, environment: dict) -> dict:
    """Return a versioned, credential-free manifest for the control Worker."""
    public_nodes = [_public_node(node) for node in nodes]
    return {
        "schema_version": 1,
        "kind": "vpngate_cf_validated_nodes",
        "generated_at": generated_at,
        "expires_at": _minimum_expiry(nodes),
        "scope": "tcp_ipv4_ws",
        "data_plane": {
            "host": environment["edge_host"],
            "port": settings["edge_port"],
            "base_path": settings["edge_base_path"],
        },
        "nodes": public_nodes,
    }


def empty_cf_manifest(status: str, generated_at: str) -> dict:
    """Return an empty publication that makes a failed batch fail closed."""
    return {
        "schema_version": 1,
        "kind": "vpngate_cf_validated_nodes",
        "generated_at": generated_at,
        "expires_at": None,
        "scope": "tcp_ipv4_ws",
        "status": status,
        "nodes": [],
    }
