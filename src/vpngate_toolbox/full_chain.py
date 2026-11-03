"""Xray session helper used by the native VPS acceptance checks."""
from __future__ import annotations

import json
import os
import signal
import socket
import subprocess
import tempfile
import time
from pathlib import Path

from .common import ToolError, https_url, public_ipv4, write_json


def child_environment():
    # Neither curlrc nor shell proxy environment can bypass the local SOCKS endpoint.
    return {key: value for key, value in os.environ.items()
            if key in ("PATH", "LANG", "LC_ALL", "SYSTEMROOT", "SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE")}


class XraySession:
    """Run one temporary Xray client and expose its local SOCKS endpoint."""

    def __init__(self, connection, binary: str, timeout: int):
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
