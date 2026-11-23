"""Cross-layer contracts that no single-layer test can catch.

- Backend check `Code` values must all have a frontend `errorLabels` entry,
  otherwise the UI silently falls back to showing the raw English code.
- Naming follows tests/test_*.py and runs under `python3 -m unittest discover -s tests`.
  Pure text scanning, no Linux/root/systemd required.
"""
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FANOUT = ROOT / "vps" / "fanout"
STATUS_TS = FANOUT / "frontend" / "src" / "lib" / "status.ts"

# checkFailure() in console_checks.go only returns bare code strings, so every
# lowercase-quoted return inside that function is a code by construction.
CHECK_FAILURE_FUNC = "func checkFailure"


def backend_check_codes():
    codes = set()
    for path in FANOUT.rglob("*.go"):
        if path.name.endswith("_test.go"):
            continue
        text = path.read_text(encoding="utf-8")
        # result.Code = "x"  — take the last string: tuple assignments put the
        # Code value last (e.g. `Status, Code = "deferred", "candidate_in_use"`).
        for m in re.finditer(r"\.Code\s*=\s*(\"[^\"]*\"(?:\s*,\s*\"[^\"]*\")?)", text):
            strings = re.findall(r"\"([^\"]*)\"", m.group(1))
            if strings and strings[-1]:
                codes.add(strings[-1])
        # CheckResult{... Code: "x" ...} struct literals.
        for m in re.finditer(r"\bCode:\s*\"([^\"]+)\"", text):
            codes.add(m.group(1))
        # checkFailure() bare `return "code"` statements.
        start = text.find(CHECK_FAILURE_FUNC)
        if start != -1:
            end = text.find("\nfunc ", start)
            body = text[start:end if end != -1 else len(text)]
            for m in re.finditer(r"return\s+\"([a-z][a-z0-9_]+)\"", body):
                codes.add(m.group(1))
    return codes


def frontend_error_labels():
    text = STATUS_TS.read_text(encoding="utf-8")
    start = text.find("errorLabels")
    block = text[start:text.find("};", start)]
    return set(re.findall(r"^\s*([a-z][a-z0-9_]*):", block, re.M))


class CrossContractTests(unittest.TestCase):
    def test_backend_check_codes_have_frontend_labels(self):
        codes = backend_check_codes()
        # Guard against silent scanner breakage: these codes must always exist.
        self.assertIn("probe_environment_failed", codes)
        self.assertIn("candidate_in_use", codes)
        labels = frontend_error_labels()
        self.assertTrue(labels, "no frontend errorLabels parsed")
        missing = sorted(codes - labels)
        self.assertEqual(missing, [],
                         "backend check Codes without a frontend errorLabels entry "
                         "(UI would show the raw code): " + ", ".join(missing))
