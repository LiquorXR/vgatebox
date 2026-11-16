#!/usr/bin/env python3
"""Documentation gate: links, reachability, forbidden history terms, API drift.

Run from the repository root (or any directory): python3 scripts/check_docs.py
"""
from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DOCS = ROOT / "docs"
ROOT_README = ROOT / "README.md"

# User-facing docs must describe the current product, not the migration story.
FORBIDDEN = (
    "新控制台",
    "旧页面",
    "旧控制台",
    "已移除",
    "基线提交",
    "本轮重构",
    "P0-",
    "P1-",
    "P3-",
)

LINK_RE = re.compile(r"\[[^\]]*\]\(([^)]+)\)")
HEADING_RE = re.compile(r"^(#{1,6})\s+(.*?)\s*$")
ROUTE_ROW_RE = re.compile(r"^\|\s*([A-Z]+(?:/[A-Z]+)*)\s*\|\s*`([^`]+)`\s*\|")


def markdown_files() -> list[Path]:
    files = [ROOT_README] if ROOT_README.exists() else []
    files.extend(sorted(DOCS.rglob("*.md")))
    return files


def strip_code(text: str) -> str:
    text = re.sub(r"```.*?```", "", text, flags=re.S)
    text = re.sub(r"`[^`]*`", "", text)
    return text


def slugify(heading: str, seen: dict[str, int]) -> str:
    text = re.sub(r"<[^>]+>", "", heading)
    text = text.strip().lower()
    text = re.sub(r"[^\w\s\-\u4e00-\u9fff]", "", text)
    text = re.sub(r"\s+", "-", text)
    if not text:
        text = "section"
    count = seen.get(text, 0)
    seen[text] = count + 1
    return text if count == 0 else f"{text}-{count}"


def anchors(path: Path) -> set[str]:
    text = strip_code(path.read_text(encoding="utf-8"))
    seen: dict[str, int] = {}
    out: set[str] = set()
    for line in text.splitlines():
        match = HEADING_RE.match(line)
        if match:
            out.add(slugify(match.group(2), seen))
    return out


def check_links(files: list[Path]) -> list[str]:
    errors: list[str] = []
    anchor_cache: dict[Path, set[str]] = {}
    for source in files:
        text = strip_code(source.read_text(encoding="utf-8"))
        for raw in LINK_RE.findall(text):
            target = raw.strip()
            if not target or target == "#":
                errors.append(f"{source.relative_to(ROOT)}: empty link target")
                continue
            if target.startswith(("http://", "https://", "mailto:", "tel:")):
                continue
            path_part, _, frag = target.partition("#")
            if path_part:
                dest = (source.parent / path_part).resolve()
                if dest.is_dir():
                    dest = dest / "README.md"
                if not dest.exists():
                    errors.append(f"{source.relative_to(ROOT)}: broken link -> {target}")
                    continue
                if dest.suffix == ".md" and frag:
                    if dest not in anchor_cache:
                        anchor_cache[dest] = anchors(dest)
                    if frag not in anchor_cache[dest]:
                        errors.append(f"{source.relative_to(ROOT)}: missing anchor -> {target}")
            elif frag:
                if source not in anchor_cache:
                    anchor_cache[source] = anchors(source)
                if frag not in anchor_cache[source]:
                    errors.append(f"{source.relative_to(ROOT)}: missing anchor -> #{frag}")
    return errors


def check_reachable(files: list[Path]) -> list[str]:
    by_path = {p.resolve(): p for p in files}
    edges: dict[Path, set[Path]] = {p.resolve(): set() for p in files}
    for source in files:
        text = strip_code(source.read_text(encoding="utf-8"))
        for raw in LINK_RE.findall(text):
            target = raw.strip()
            if not target or target.startswith(("http://", "https://", "mailto:", "tel:")):
                continue
            path_part, _, _ = target.partition("#")
            if not path_part:
                continue
            dest = (source.parent / path_part).resolve()
            if dest.is_dir():
                dest = dest / "README.md"
            if dest in by_path:
                edges[source.resolve()].add(dest)
    roots = [ROOT_README.resolve(), (DOCS / "README.md").resolve()]
    seen: set[Path] = set()
    stack = [r for r in roots if r in edges]
    while stack:
        current = stack.pop()
        if current in seen:
            continue
        seen.add(current)
        stack.extend(edges[current] - seen)
    errors = []
    for path in sorted(by_path):
        if path not in seen:
            errors.append(f"{by_path[path].relative_to(ROOT)}: not reachable from README.md or docs/README.md")
    return errors


def check_forbidden(files: list[Path]) -> list[str]:
    errors: list[str] = []
    for source in files:
        if "docs/adr" in source.as_posix().replace("\\", "/"):
            continue
        text = source.read_text(encoding="utf-8")
        for term in FORBIDDEN:
            if term in text:
                errors.append(f"{source.relative_to(ROOT)}: forbidden history term {term!r}")
    return errors


def check_api(files: list[Path]) -> list[str]:
    api_doc = DOCS / "reference" / "api.md"
    golden = ROOT / "vps" / "fanout" / "testdata" / "api" / "routes.json"
    if not api_doc.exists() or not golden.exists():
        return []
    documented: set[tuple[str, str]] = set()
    for line in api_doc.read_text(encoding="utf-8").splitlines():
        match = ROUTE_ROW_RE.match(line)
        if not match:
            continue
        methods, path = match.group(1), match.group(2)
        for method in methods.split("/"):
            documented.add((path, method))
    data = json.loads(golden.read_text(encoding="utf-8"))
    expected = {(r["path"], m) for r in data["routes"] for m in r["methods"]}
    errors = []
    for item in sorted(expected - documented):
        errors.append(f"docs/reference/api.md: missing route {item[1]} {item[0]}")
    for item in sorted(documented - expected):
        errors.append(f"docs/reference/api.md: undocumented route {item[1]} {item[0]}")
    return errors


def main() -> int:
    if not DOCS.exists():
        print("docs/ not found", file=sys.stderr)
        return 1
    files = markdown_files()
    errors: list[str] = []
    errors += check_links(files)
    errors += check_reachable(files)
    errors += check_forbidden(files)
    errors += check_api(files)
    if errors:
        for error in errors:
            print("ERROR " + error, file=sys.stderr)
        print(f"docs check failed: {len(errors)} error(s)", file=sys.stderr)
        return 1
    print(f"docs check passed: {len(files)} markdown files")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
