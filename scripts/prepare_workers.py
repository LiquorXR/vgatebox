#!/usr/bin/env python3
"""Prepare two separately licensed Workers from pinned, verified upstream files.

Does not deploy, create account credentials, or include any secrets in source.
"""
import argparse
import hashlib
import json
import shutil
import sys
import zipfile
from pathlib import Path
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]
SOURCES = {
    'checker': {
        'repo': 'lsh8848/cm-Workers-CheckSocks5',
        'commit': '2f31cf9a242444eb22905d888d4d8871b0625b3a',
        'license': 'GPL-3.0-only',
        'hashes': {'_worker.js': '6cab20b8474fe823740fa020c5e1ad832ace42546fc3fed5c5d5b9f1d8273824', 'LICENSE': '3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986', 'README.md': '66b7622c5bacf581c6855acfd711a6d4715ce43d6df48fd9c55805abc5ba07bd'},
        'guard': 'toolboxCheckerGuard',
    },
    'edge': {
        'repo': 'cmliu/edgetunnel',
        'commit': 'af4f9837e1843e34159018713bc8749ccec3004d',
        'license': 'GPL-2.0-only',
        'hashes': {'_worker.js': 'dc7428d0651f7b0773f96c87b5b764c7dcd86e5fa1f7b326fae65c8cc27cf6b5', 'LICENSE': '8177f97513213526df2cf6184d8ff986c675afb514d4e68a404010521b880643'},
        'guard': 'toolboxEdgeGuard',
    },
}


def replace_once(source, before, after):
    if source.count(before) != 1:
        raise ValueError('upstream_patch_mismatch')
    return source.replace(before, after, 1)


def build(kind, files, destination):
    spec = SOURCES[kind]
    destination.mkdir(parents=True, exist_ok=True)
    for name, payload in files.items():
        if hashlib.sha256(payload).hexdigest() != spec['hashes'][name]:
            raise ValueError('upstream_hash_mismatch:' + kind + ':' + name)
    upstream = files['_worker.js'].decode('utf-8-sig')
    guard = (ROOT / 'worker' / kind / 'guard.mjs').read_text()
    source = replace_once(upstream, 'async fetch(request, env, ctx) {', 'async fetch(request, env, ctx) {\n        const denied = ' + spec['guard'] + '(request, env);\n        if (denied) return denied;')
    modifications = ['API-only entry, fail closed without credentials, VPN Gate SSTP backend restrictions']
    if kind == 'checker':
        # Separate basic tunnel egress from the rate-limited intelligence API.
        # Missing ASN/privacy must stay unknown, never fabricated false flags.
        source = replace_once(source, "const targetHost = 'www.iplocate.io';", "const targetHost = 'api.iplocate.io';")
        source = replace_once(source, "'GET /api/lookup HTTP/1.1'", "'GET /json HTTP/1.1'")
        source = source.replace('Target /api/lookup', 'Target /json')
        start = source.index('// Transform iplocate.io response to maintain compatibility with existing frontend')
        end = source.index('\n\t\t} finally {', start)
        source = source[:start] + "// IP-only baseline; optional intelligence is queried separately.\n\t\t\texit = { ip: exit.ip, baseline_provider: 'iplocate-current-ip', baseline_fields: 'ip_only' };" + source[end:]
        modifications.append('Use the official api.iplocate.io/json IP-only endpoint; no inferred ASN/privacy defaults')
    elif kind == 'edge':
        # No administrator UI/KV logs, no alternate upstream, and no UDP path.
        source = replace_once(source, 'const denied = toolboxEdgeGuard(request, env);', "env = { UUID: env.UUID, DEBUG: 'false', OFF_LOG: '1' };\n        const denied = toolboxEdgeGuard(request, env);")
        source = replace_once(source, "if (cmd === 1) { } else if (cmd === 2) { isUDP = true } else { return { hasError: true, message: 'Invalid command' } }", "if (cmd !== 1) return { hasError: true, message: 'TCP only' };")
        source = replace_once(source, 'async function forwardataudp(udpChunk, webSocket, respHeader, request, 响应封装器 = null) {', "async function forwardataudp(udpChunk, webSocket, respHeader, request, 响应封装器 = null) {\n    throw new Error('UDP disabled by VPN Gate Toolbox');")
        source = replace_once(source, 'async function 转发木马UDP数据(chunk, webSocket, 上下文, request) {', "async function 转发木马UDP数据(chunk, webSocket, 上下文, request) {\n    throw new Error('UDP disabled by VPN Gate Toolbox');")
        modifications.append('Strip optional runtime bindings; reject VLESS UDP and all UDP forwarding helpers')
    source = '// Modified by VPN Gate Toolbox on 2026-10-08.\n// Source: https://github.com/' + spec['repo'] + '/tree/' + spec['commit'] + '\n// SPDX-License-Identifier: ' + spec['license'] + '\n' + guard.replace('export function ', 'function ') + '\n' + source
    (destination / 'worker.mjs').write_text(source)
    (destination / 'upstream.mjs').write_bytes(files['_worker.js'])
    (destination / 'LICENSE').write_bytes(files['LICENSE'])
    if 'README.md' in files:
        (destination / 'UPSTREAM_README.md').write_bytes(files['README.md'])
    (destination / 'SOURCE.json').write_text(json.dumps({**{key: spec[key] for key in ['repo', 'commit', 'license', 'hashes']}, 'modifications': modifications, 'worker_sha256': hashlib.sha256(source.encode()).hexdigest()}, indent=2) + '\n')
    (destination / 'wrangler.toml').write_text('name = "vpngate-' + kind + '"\nmain = "worker.mjs"\ncompatibility_date = "2026-10-08"\nworkers_dev = true\n[observability]\nenabled = false\n')
    # Dashboard advanced-mode upload: only public source/metadata, no .env.
    pages = destination / 'pages'
    pages.mkdir(exist_ok=True)
    (pages / '_worker.js').write_text(source)
    (pages / '_routes.json').write_text('{"version":1,"include":["/*"],"exclude":[]}\n')
    (pages / 'index.html').write_text('<!doctype html><title>VPN Gate Toolbox API</title><p>Authentication required. This deployment is an API service.</p>\n')
    shutil.copyfile(destination / 'LICENSE', pages / 'LICENSE.txt')
    shutil.copyfile(destination / 'SOURCE.json', pages / 'SOURCE.json')
    with zipfile.ZipFile(destination / 'pages-upload.zip', 'w', compression=zipfile.ZIP_DEFLATED) as archive:
        for name in ['_worker.js', '_routes.json', 'index.html', 'LICENSE.txt', 'SOURCE.json']:
            archive.write(pages / name, name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-cache', type=Path, help='Previously audited repo__name directories; otherwise download pinned HTTPS source')
    args = parser.parse_args()
    for kind, spec in SOURCES.items():
        files = {}
        for name in spec['hashes']:
            if args.source_cache:
                payload = (args.source_cache / spec['repo'].replace('/', '__') / name).read_bytes()
            else:
                url = 'https://raw.githubusercontent.com/' + spec['repo'] + '/' + spec['commit'] + '/' + name
                with urlopen(Request(url, headers={'User-Agent': 'VPNGate-Toolbox/0.1'}), timeout=30) as response:
                    if response.url != url:
                        raise ValueError('unexpected_redirect')
                    payload = response.read(2 * 1024 * 1024)
            files[name] = payload
        destination = ROOT / 'runtime' / 'deploy' / kind
        build(kind, files, destination)
        print('Prepared:', destination, '(' + spec['license'] + ')')
    # The GPL v2 notice is explicit for the separately licensed local modifier.
    shutil.copyfile(ROOT / 'runtime/deploy/edge/LICENSE', ROOT / 'worker/edge/LICENSE')
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError) as error:
        print('Preparation failed:', str(error), file=sys.stderr)
        sys.exit(1)
