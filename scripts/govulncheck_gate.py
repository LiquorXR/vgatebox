"""govulncheck 第三方依赖门禁（被 .github/workflows/lint.yml 调用）。

用法：govulncheck -json ./... > report.json; python3 scripts/govulncheck_gate.py report.json

规则：
- trace 全在 stdlib/fanout 的 findings：Go 1.24 EOL 工具链债务，只计数，不红灯；
- 涉及第三方模块的 findings：除基线 ALLOWLIST 外一律红灯。
"""

import json
import sys

# 基线 allowlist：第三方模块 findings 中已知、可解释、延后处理的条目。
ALLOWLIST = {
    "GO-2026-5024": "x/sys windows-only NewNTUnicodeString (linux 二进制不受影响)，fixed v0.44.0，依赖升级延后",
}


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
