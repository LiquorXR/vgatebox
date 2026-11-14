"""govulncheck 第三方依赖门禁（被 .github/workflows/lint.yml 调用）。

用法：govulncheck -json ./... > report.json; python3 scripts/govulncheck_gate.py report.json

规则：
- trace 全在 stdlib/fanout 的 findings：工具链债务，只计数，不红灯；
- 涉及第三方模块的 findings：一律红灯（无 allowlist，有即修）。
"""

import json
import sys

# 第三方模块 findings 无 allowlist：出现即红灯。
# （GO-2026-5024 曾在此放行 x/sys windows-only 问题，已随 x/sys v0.44.0+ 修复，移除。）
ALLOWLIST = {}


def load_stream(path):
    raw = open(path, encoding="utf-8", errors="replace").read()
    dec = json.JSONDecoder()
    idx, n = 0, len(raw)
    while idx < n:
        while idx < n and raw[idx].isspace():
            idx += 1
        if idx >= n:
            break
        obj, end = dec.raw_decode(raw, idx)
        yield obj
        idx = end


def main(path):
    stdlib, allowed, failed = 0, [], []
    for obj in load_stream(path):
        f = obj.get("finding")
        if not f:
            continue
        mods = {fr.get("module", "?") for fr in f.get("trace") or []}
        if mods <= {"stdlib", "fanout"}:
            stdlib += 1
            continue
        if f.get("osv") in ALLOWLIST:
            allowed.append(f.get("osv"))
            continue
        failed.append((f.get("osv"), sorted(mods)))
    print("stdlib findings (toolchain debt, tolerated): %d" % stdlib)
    print("allowlisted third-party findings: %s" % sorted(set(allowed)))
    if failed:
        print("BLOCKING third-party findings:")
        for osv, mods in failed:
            print("  %s via %s" % (osv, mods))
        return 1
    print("third-party gate: PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
