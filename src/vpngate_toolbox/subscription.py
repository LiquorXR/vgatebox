"""Original VLESS configuration generator for the audited EdgeTunnel interface."""
from __future__ import annotations

from dataclasses import dataclass
from urllib.parse import quote, urlencode, urlsplit, parse_qs, unquote

from .checker import proxy_authority
from .common import ToolError, digest


@dataclass(frozen=True)
class Connection:
    host: str
    port: int
    uuid: str
    path: str
    node_id: str

    @property
    def sha256(self):
        return digest({"host": self.host, "port": self.port, "uuid": self.uuid, "path": self.path,
                       "transport": "ws", "security": "tls", "scope": "tcp_ipv4"})

    def link(self):
        query = urlencode({"encryption": "none", "security": "tls", "type": "ws", "sni": self.host,
                           "host": self.host, "path": self.path})
        return "vless://" + self.uuid + "@" + self.host + ":" + str(self.port) + "?" + query + "#" + quote(self.node_id)

    def xray_config(self, socks_port: int) -> dict:
        return {"log": {"loglevel": "none"},
                "inbounds": [{"tag": "verify-in", "listen": "127.0.0.1", "port": socks_port,
                              "protocol": "socks", "settings": {"auth": "noauth", "udp": False}}],
                "outbounds": [{"tag": "vpn-exit", "protocol": "vless",
                               "settings": {"vnext": [{"address": self.host, "port": self.port,
                                                        "users": [{"id": self.uuid, "encryption": "none"}]}]},
                               "streamSettings": {"network": "ws", "security": "tls",
                                                  "tlsSettings": {"serverName": self.host, "allowInsecure": False},
                                                  "wsSettings": {"path": self.path, "headers": {"Host": self.host}}}},
                              {"tag": "block", "protocol": "blackhole", "settings": {}}],
                "routing": {"rules": [{"type": "field", "network": "udp", "outboundTag": "block"},
                                      {"type": "field", "ip": ["::/0"], "outboundTag": "block"},
                                      {"type": "field", "inboundTag": ["verify-in"], "network": "tcp", "outboundTag": "vpn-exit"}]}}


def make_connection(node: dict, environment: dict, settings: dict, invalid=False) -> Connection:
    protocol = node["protocols"]["sstp"]
    if not invalid and protocol.get("status") != "passed":
        raise ToolError("sstp_not_verified")
    candidate = {"host": "nonexistent.invalid", "port": 443} if invalid else {"host": protocol["host"], "port": protocol["port"]}
    path = settings["edge_base_path"] + "?" + urlencode({"sstp": proxy_authority(candidate, environment), "globalproxy": "1"})
    return Connection(environment["edge_host"], settings["edge_port"], environment["uuid"], path, node["node_id"])


def connection_from_link(link: str) -> Connection:
    """Only accept the narrow profile this version generates, with no direct fallback."""
    parsed = urlsplit(link)
    query = parse_qs(parsed.query, keep_blank_values=True)
    if parsed.scheme != "vless" or not parsed.hostname or not parsed.username or not parsed.port:
        raise ToolError("invalid_vless_link")
    for key, expected in (("security", "tls"), ("type", "ws"), ("encryption", "none")):
        if query.get(key) != [expected]:
            raise ToolError("unsupported_vless_profile")
    if query.get("host") != [parsed.hostname] or query.get("sni") != [parsed.hostname]:
        raise ToolError("vless_host_mismatch")
    path = query.get("path", [None])[0]
    if not path:
        raise ToolError("vless_path_missing")
    edge_query = parse_qs(urlsplit(path).query)
    if edge_query.get("globalproxy") != ["1"] or len(edge_query.get("sstp", [])) != 1:
        raise ToolError("forced_sstp_required")
    return Connection(parsed.hostname, parsed.port, unquote(parsed.username), path, unquote(parsed.fragment))
