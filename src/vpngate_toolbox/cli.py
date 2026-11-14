from __future__ import annotations

import argparse
import json
import sys
from contextlib import contextmanager
from pathlib import Path

try:
    import fcntl
except ImportError:  # Windows 无 fcntl：仅 Linux/WSL 支持构建锁
    fcntl = None

from .common import ToolError, write_json
from .config import load_env, load_settings
from .pool import build_pool
from .intelligence import describe_intelligence_config, profile_measured_exit
from .country import attach_country
from .pool_schedule import scheduled_pool


@contextmanager
def output_lock(output: Path, wait=False):
    if fcntl is None:
        raise ToolError("output_lock_requires_posix")
    output.mkdir(parents=True, exist_ok=True)
    with (output / ".build.lock").open("w") as handle:
        try:
            fcntl.flock(handle, fcntl.LOCK_EX | (0 if wait else fcntl.LOCK_NB))
        except BlockingIOError:
            raise ToolError("another_build_is_running") from None
        try:
            yield
        finally:
            fcntl.flock(handle, fcntl.LOCK_UN)


def _drop_cached_ip(cache_path: Path, ip: str):
    """Remove cached intelligence entries for a single IP so --refresh re-queries."""
    try:
        raw = json.loads(cache_path.read_text())
    except (FileNotFoundError, ValueError):
        return
    if not isinstance(raw, dict):
        return
    suffix = ":" + ip.strip()
    kept = {k: v for k, v in raw.items() if not k.endswith(suffix)}
    if len(kept) != len(raw):
        write_json(cache_path, kept, private=True)


def main(argv=None):
    parser = argparse.ArgumentParser(description="VPN Gate shared pool builder and egress profiler")
    parser.add_argument("--config", type=Path, default=Path("config/settings.json"))
    parser.add_argument("--env-file", type=Path, default=Path(".env"))
    sub = parser.add_subparsers(dest="command", required=True)
    pool = sub.add_parser("pool")
    pool.add_argument("--csv-file", type=Path, help="Use a local official-format CSV instead of fetching")
    pool.add_argument("--output", type=Path, default=Path("data"))
    pool.add_argument("--runtime", type=Path, default=Path("runtime"))
    pool.add_argument("--schedule-dir", type=Path)
    pool.add_argument("--scheduled", action="store_true")
    profile = sub.add_parser("profile-openvpn", help="Profile a measured OpenVPN exit IP; does not verify a tunnel")
    profile.add_argument("--ip", required=True)
    profile.add_argument("--cache", type=Path, default=Path("runtime/intelligence/cache.json"))
    sstp_profile = sub.add_parser("profile-sstp", help="Profile a measured SSTP exit; not a tunnel check")
    sstp_profile.add_argument("--ip", required=True)
    sstp_profile.add_argument("--cache", type=Path, default=Path("runtime/intelligence/sstp-cache.json"))
    socks_profile = sub.add_parser("profile-socks5", help="Profile a measured third-party SOCKS5 exit")
    socks_profile.add_argument("--ip", required=True)
    socks_profile.add_argument("--cache", type=Path, default=Path("runtime/intelligence/socks5-cache.json"))
    for command in (profile, sstp_profile, socks_profile):
        command.add_argument("--country", action="store_true", help="Independently locate the measured IP without inferring residential type")
        command.add_argument("--refresh", action="store_true", help="Bypass cached intelligence for this IP and re-query")
    check = sub.add_parser("check-intelligence", help="Validate intelligence env config without leaking secrets")
    check.add_argument("--ip", required=False, default=None, help="Optional measured IPv4 to live-query after validation")
    check.add_argument("--cache", type=Path, default=Path("runtime/intelligence/cache.json"))
    check.add_argument("--refresh", action="store_true", help="Bypass cache when --ip is given")
    args = parser.parse_args(argv)
    try:
        if args.command == "check-intelligence":
            settings = load_settings(args.config)
            load_env(args.env_file)
            info = describe_intelligence_config()
            if not args.ip:
                print(json.dumps(info, ensure_ascii=False))
                return 0
            if args.refresh:
                with output_lock(args.cache.parent, wait=True):
                    _drop_cached_ip(args.cache, args.ip)
            with output_lock(args.cache.parent, wait=True):
                image = profile_measured_exit(args.ip, settings, args.cache, "openvpn")
            # Never echo secrets; image already hides token/URL, keep only the verdict.
            print(json.dumps({"config": info, "result": image}, ensure_ascii=False))
            return 0
        if args.command in ("profile-openvpn", "profile-sstp", "profile-socks5"):
            settings = load_settings(args.config)
            load_env(args.env_file)
            if args.refresh:
                with output_lock(args.cache.parent, wait=True):
                    _drop_cached_ip(args.cache, args.ip)
            # Multiple Slots can finish their baselines concurrently. Serialize
            # cache read/query/write without locking or rewriting the node pool.
            with output_lock(args.cache.parent, wait=True):
                image = profile_measured_exit(args.ip, settings, args.cache, args.command.removeprefix("profile-"))
            if args.country:
                with output_lock(args.cache.parent / "country", wait=True):
                    image = attach_country(image, args.cache.parent / "country/cache.json")
            print(json.dumps(image, ensure_ascii=False))
            return 0
        with output_lock(args.output):
            settings = load_settings(args.config)
            load_env(args.env_file)
            if args.command == "pool":
                if args.scheduled and args.schedule_dir is None:
                    raise ToolError('invalid_schedule_configuration')
                build = lambda: build_pool(settings, args.output, args.csv_file)
                pool = scheduled_pool(args.schedule_dir, args.output, args.scheduled, build) if args.schedule_dir else build()
                if pool is None:
                    print(json.dumps({'status': 'source_refresh_skipped'}))
                    return 0
                print(json.dumps({"status": "candidate_pool_built", "nodes": len(pool["nodes"])}))
                return 0
    except ToolError as exc:
        print("Error: " + str(exc), file=sys.stderr)
        return 2
    except (OSError, ValueError, KeyError, TypeError) as exc:
        print("Error: invalid_input_or_filesystem_error (" + type(exc).__name__ + ")", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
