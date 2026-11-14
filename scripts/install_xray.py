#!/usr/bin/env python3
"""Install a pinned official Xray binary after checking its SHA256; no latest lookup."""
import argparse
import io
import json
import platform
import re
import sys
import time
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "src"))
from vpngate_toolbox.common import ToolError, atomic_write, digest, get_bytes


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--archive", type=Path, help="Local archive cache: verified and installed when present, downloaded and stored there otherwise")
    parser.add_argument("--output", type=Path, default=ROOT / "tools/bin")
    args = parser.parse_args()
    try:
        metadata = json.loads((ROOT / "config/xray-release.json").read_text())
        machine = {"aarch64": "arm64", "amd64": "x86_64"}.get(platform.machine().lower(), platform.machine().lower())
        asset = metadata["assets"][platform.system().lower() + "-" + machine]
        if not re.fullmatch(r"v[0-9.]+", metadata["version"]) or not re.fullmatch(r"[0-9a-f]{64}", asset["sha256"]):
            raise ToolError("invalid_xray_pin")
        url = "https://github.com/XTLS/Xray-core/releases/download/" + metadata["version"] + "/" + asset["name"]
        if args.archive is not None and args.archive.is_file():
            data = args.archive.read_bytes()
        else:
            # Same release-asset flake as build_vps_package.py; retry locally
            # instead of failing the workflow on one stalled download.
            # GitHub release assets occasionally return transient 5xx (e.g.
            # http_504) for a minute or more; back off longer, but fail fast
            # on non-retryable errors like invalid_xray_pin.
            data = None
            for attempt in range(5):
                try:
                    data = get_bytes(url, timeout=60, limit=60_000_000)
                    break
                except (ToolError, OSError) as exc:
                    retryable = isinstance(exc, OSError) or str(exc).startswith(
                        ("http_429", "http_5", "https_unavailable", "https_timeout"))
                    if not retryable or attempt >= 4:
                        raise
                    time.sleep(10 * (attempt + 1))
            if args.archive is not None:
                args.archive.parent.mkdir(parents=True, exist_ok=True)
                atomic_write(args.archive, data)
        if digest(data) != asset["sha256"]:
            raise ToolError("xray_archive_digest_mismatch")
        with zipfile.ZipFile(io.BytesIO(data)) as archive:
            if archive.getinfo("xray").file_size > 100_000_000:
                raise ToolError("xray_binary_too_large")
            atomic_write(args.output / "xray", archive.read("xray"), private=True)
            (args.output / "xray").chmod(0o700)
            for name in ("LICENSE", "README.md"):
                if name in archive.namelist():
                    atomic_write(args.output / (name + ".xray"), archive.read(name))
        print("Installed Xray " + metadata["version"] + " with verified archive SHA256")
        return 0
    except (ToolError, OSError, ValueError, KeyError, zipfile.BadZipFile) as exc:
        print(str(exc) if isinstance(exc, ToolError) else "xray_install_failed", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
