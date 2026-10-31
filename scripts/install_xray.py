#!/usr/bin/env python3
"""Install a pinned official Xray binary after checking its SHA256; no latest lookup."""
import argparse
import io
import json
import platform
import re
import sys
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "src"))
from vpngate_toolbox.common import ToolError, atomic_write, digest, get_bytes


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--archive", type=Path, help="Verify and install a previously downloaded archive")
    parser.add_argument("--output", type=Path, default=ROOT / "tools/bin")
    args = parser.parse_args()
    try:
        metadata = json.loads((ROOT / "config/xray-release.json").read_text())
        machine = {"aarch64": "arm64", "amd64": "x86_64"}.get(platform.machine().lower(), platform.machine().lower())
        asset = metadata["assets"][platform.system().lower() + "-" + machine]
        if not re.fullmatch(r"v[0-9.]+", metadata["version"]) or not re.fullmatch(r"[0-9a-f]{64}", asset["sha256"]):
            raise ToolError("invalid_xray_pin")
        url = "https://github.com/XTLS/Xray-core/releases/download/" + metadata["version"] + "/" + asset["name"]
        data = args.archive.read_bytes() if args.archive else get_bytes(url, timeout=60, limit=60_000_000)
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
