"""Run the exact generated VLESS profile, with verified HTTPS through SOCKS only."""
from __future__ import annotations

import json
import os
import signal
import socket
import subprocess
import tempfile
import time
from pathlib import Path

from .common import ToolError, https_url, public_ipv4, utc_now, expires_at, write_json
from .subscription import Connection, connection_from_link


def child_environment():
    # Neither curlrc nor shell proxy environment can bypass the local SOCKS endpoint.
    return {key: value for key, value in os.environ.items()
            if key in ("PATH", "LANG", "LC_ALL", "SYSTEMROOT", "SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE")}


class XraySession:
    def __init__(self, connection: Connection, binary: str, timeout: int):
        self.connection = connection
        self.binary = str(Path(binary).resolve())
        self.deadline = time.monotonic() + timeout
        self.process = None
        self.directory = None

    def remaining(self):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise ToolError("chain_deadline_exceeded")
        return remaining

    def __enter__(self):
        self.directory = tempfile.TemporaryDirectory(prefix="vpngate-xray-")
        try:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
                sock.bind(("127.0.0.1", 0))
                self.port = sock.getsockname()[1]
            path = Path(self.directory.name) / "config.json"
            write_json(path, self.connection.xray_config(self.port), private=True)
            result = subprocess.run([self.binary, "run", "-test", "-config", str(path)],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                    timeout=min(10, self.remaining()), env=child_environment())
            if result.returncode:
                raise ToolError("xray_configuration_rejected")
            self.process = subprocess.Popen([self.binary, "run", "-config", str(path)],
                                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                            start_new_session=True, env=child_environment())
            ready_until = min(self.deadline, time.monotonic() + 5)
            while time.monotonic() < ready_until:
                if self.process.poll() is not None:
                    raise ToolError("xray_start_failed")
                try:
                    with socket.create_connection(("127.0.0.1", self.port), timeout=0.1):
                        return self
                except OSError:
                    time.sleep(0.05)
            raise ToolError("xray_start_timeout")
        except (OSError, subprocess.TimeoutExpired):
            self.close()
            raise ToolError("xray_unavailable") from None
        except BaseException:
            self.close()
            raise

    def get(self, url: str) -> bytes:
        https_url(url)
        remaining = self.remaining()
        output = Path(self.directory.name) / "curl-body"
        command = ["curl", "--disable", "--silent", "--show-error", "--fail", "--ipv4",
                   "--proxy", "socks5h://127.0.0.1:" + str(self.port), "--noproxy", "",
                   "--proto", "=https", "--proto-redir", "=https",
                   "--connect-timeout", str(min(10, remaining)), "--max-time", str(remaining),
                   "--max-filesize", "65536", "--output", str(output), "--write-out", "%{http_code}", url]
        try:
            result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                    timeout=remaining + 0.5, env=child_environment())
        except subprocess.TimeoutExpired:
            raise ToolError("curl_failed_28") from None
        except OSError:
            raise ToolError("curl_unavailable") from None
        if result.returncode:
            raise ToolError("curl_failed_" + str(result.returncode))
        status = result.stdout.decode("ascii", errors="replace")
        if not status.isdigit() or not 200 <= int(status) < 300:
            raise ToolError("https_status_rejected")
        if not output.exists() or output.stat().st_size > 65536:
            raise ToolError("https_body_size_rejected")
        return output.read_bytes()

    def close(self):
        if self.process is not None:
            if self.process.poll() is None:
                try:
                    os.killpg(self.process.pid, signal.SIGTERM)
                    self.process.wait(timeout=2)
                except (ProcessLookupError, subprocess.TimeoutExpired):
                    if self.process.poll() is None:
                        os.killpg(self.process.pid, signal.SIGKILL)
                        self.process.wait(timeout=2)
            self.process = None
        if self.directory is not None:
            self.directory.cleanup()
            self.directory = None

    def __exit__(self, *_):
        self.close()


def parse_echo(data: bytes) -> str:
    if len(data) > 4096:
        raise ToolError("echo_response_too_large")
    try:
        text = data.decode("utf-8").strip()
        if text.startswith("{"):
            text = json.loads(text).get("ip")
    except (ValueError, UnicodeError, AttributeError):
        raise ToolError("invalid_echo_response") from None
    return public_ipv4(text)


def verify(connection: Connection, expected_ip: str, settings: dict, binary: str, session_factory=XraySession) -> dict:
    started = time.monotonic()
    evidence = {"status": "failed", "expected_exit_ip": expected_ip, "actual_exit_ips": [],
                "config_sha256": connection.sha256, "verified_at": utc_now(),
                "scope": "tcp_ipv4_ws", "echo_urls": settings["echo_urls"],
                "baseline_assurance": "preliminary_checker_tls_T1"}
    try:
        expected_ip = public_ipv4(expected_ip)
        # Parse what will actually be delivered to users; path is never rewritten for tests.
        published = connection_from_link(connection.link())
        if published.sha256 != connection.sha256:
            raise ToolError("subscription_config_mismatch")
        with session_factory(published, binary, settings["chain_timeout_seconds"]) as session:
            for _ in range(2):
                for url in settings["echo_urls"]:
                    actual = parse_echo(session.get(url))
                    evidence["actual_exit_ips"].append(actual)
                    if actual != expected_ip:
                        raise ToolError("exit_ip_mismatch")
            if settings["content_marker"].encode() not in session.get(settings["content_url"]):
                raise ToolError("content_check_failed")
        evidence.update({"status": "passed", "actual_exit_ip": expected_ip,
                         "expires_at": expires_at(settings["chain_ttl_seconds"])})
    except ToolError as exc:
        evidence["failure_code"] = str(exc)
    evidence["latency_ms"] = round((time.monotonic() - started) * 1000)
    return evidence


def verify_no_fallback(connection: Connection, settings: dict, binary: str, session_factory=XraySession) -> dict:
    """With a nonexistent SSTP backend even Worker-accessible HTTPS must fail."""
    evidence = {"status": "failed", "verified_at": utc_now(), "blocked_requests": 0}
    # An invalid backend can close during target TLS handshake (curl 35).
    # Certificate verification failures (60/77) are never acceptable evidence.
    # Final positive tests still require normal verified HTTPS and exact IPs.
    transport_failures = {"curl_failed_7", "curl_failed_28", "curl_failed_35", "curl_failed_52", "curl_failed_55", "curl_failed_56", "curl_failed_97"}
    try:
        with session_factory(connection, binary, settings["chain_timeout_seconds"]) as session:
            for url in settings["echo_urls"]:
                try:
                    session.get(url)
                except ToolError as exc:
                    if str(exc) not in transport_failures:
                        raise
                    evidence["blocked_requests"] += 1
                else:
                    raise ToolError("invalid_sstp_direct_fallback_detected")
        evidence["status"] = "passed"
        evidence["expires_at"] = expires_at(settings["chain_ttl_seconds"])
    except ToolError as exc:
        evidence["failure_code"] = str(exc)
    return evidence
