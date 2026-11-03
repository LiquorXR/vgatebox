from __future__ import annotations

import argparse
import fcntl
import json
import sys
from contextlib import contextmanager
from pathlib import Path

from .common import ToolError
from .config import load_env, load_settings
from .pool import build_pool
from .intelligence import profile_measured_exit
from .country import attach_country
from .pool_schedule import scheduled_pool


@contextmanager
def output_lock(output: Path, wait=False):
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
    args = parser.parse_args(argv)
    try:
        if args.command in ("profile-openvpn", "profile-sstp", "profile-socks5"):
            settings = load_settings(args.config)
            load_env(args.env_file)
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
    except (OSError, ValueError, KeyError, TypeError):
        print("Error: invalid_input_or_filesystem_error", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
