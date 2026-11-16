#!/bin/sh
# Local gate for contributors. Linux runs the full suite; other platforms run
# the cross-compile checks and the frontend suite, and skip Linux-only tests.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

echo "== docs =="
python3 scripts/check_docs.py

echo "== go vet (linux/amd64) =="
(cd vps/fanout && GOOS=linux GOARCH=amd64 go vet ./...)

echo "== go test compile (linux/amd64) =="
tmp=$(mktemp "${TMPDIR:-/tmp}/vgatebox-check.XXXXXX")
trap 'rm -f "$tmp"' EXIT
(cd vps/fanout && GOOS=linux GOARCH=amd64 go test -c -o "$tmp" .)

case "$(uname -s)" in
  Linux*)
    echo "== go test =="
    (cd vps/fanout && go test ./...)

    echo "== python unittest =="
    python3 -m unittest discover -s tests -v
    ;;
  *)
    echo "== skip go test / python unittest: Linux-only (netns/pidfd/POSIX permissions) =="
    ;;
esac

if [ -d vps/fanout/frontend/node_modules ]; then
  echo "== frontend =="
  (cd vps/fanout/frontend && npm run typecheck && npm run test:unit && npm run build)
else
  echo "== skip frontend: node_modules missing (cd vps/fanout/frontend && npm ci --ignore-scripts) =="
fi
