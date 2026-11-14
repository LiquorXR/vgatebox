"""Disposable core verification using the actual generated Clash subscription."""
from contextlib import contextmanager
import re
import socket
import subprocess
import tempfile
import time
from pathlib import Path

PROXY_NAME = re.compile(r'(?m)^  - name: "((?:[^"\\]|\\.)*)"$')
GROUP_NAME = re.compile(r'(?m)^      - "((?:[^"\\]|\\.)*)"$')
MIXED_PORT = re.compile(r'(?m)^mixed-port:.*$')


def _proxies_section(profile):
    start = profile.find('\nproxies:\n')
    assert start >= 0, 'generated profile lacks proxies'
    tail = profile[start + 1:]
    end = tail.find('\nproxy-groups:\n')
    assert end >= 0, 'generated profile lacks proxy-groups'
    return tail[:end]


def _group_section(profile):
    start = profile.find('\nproxy-groups:\n')
    assert start >= 0, 'generated profile lacks proxy-groups'
    tail = profile[start + 1:]
    end = tail.find('\nrules:\n')
    assert end >= 0, 'generated profile lacks rules'
    return tail[:end]


def _redact(text, profile):
    for pattern in (r'(?m)^\s+uuid: "([^"]+)"$',
                    r'(?m)^\s+password: "([^"]+)"$',
                    r'(?m)^\s+encryption: "([^"]+)"$',
                    r'(?m)^\s+public-key: "([^"]+)"$',
                    r'(?m)^\s+short-id: "([^"]+)"$'):
        for value in re.findall(pattern, profile):
            if value:
                text = text.replace(value, '[redacted]')
    return text


@contextmanager
def client(binary, profile):
    with tempfile.TemporaryDirectory(prefix='vgb-mihomo-') as tmp:
        root = Path(tmp)
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0)); port = listener.getsockname()[1]
        config, replaced = MIXED_PORT.subn('mixed-port: %d' % port, profile, count=1)
        assert replaced == 1, 'generated profile lacks the documented mixed-port field'
        path = root / 'profile.yaml'; path.write_text(config); path.chmod(0o600)
        check = subprocess.run([str(binary), '-t', '-d', tmp, '-f', str(path)], capture_output=True, timeout=20)
        assert check.returncode == 0, 'Mihomo rejected generated subscription'
        with (root / 'client.log').open('wb') as log:
            proc = subprocess.Popen([str(binary), '-d', tmp, '-f', str(path)], stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 10
                while True:
                    assert proc.poll() is None, 'Mihomo exited before listening'
                    try:
                        with socket.create_connection(('127.0.0.1', port), timeout=.2):
                            break
                    except OSError:
                        assert time.monotonic() < deadline, 'Mihomo did not listen'
                        time.sleep(.05)
                try:
                    yield port
                except AssertionError as error:
                    log.flush()
                    detail = _redact((root / 'client.log').read_text(errors='replace')[-3000:], profile)
                    raise AssertionError(str(error) + '; sanitized core log: ' + detail) from None
            finally:
                proc.terminate()
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    proc.kill(); proc.wait()


def request(port, url):
    return subprocess.run(['curl', '-q', '--noproxy', '', '--proxy', 'socks5h://127.0.0.1:' + str(port), '--connect-timeout', '5', '--max-time', '15', '-fsS', url], capture_output=True, timeout=20)


def verify_mihomo(binary, body, expected):
    profile = body.decode('utf-8') if isinstance(body, (bytes, bytearray)) else body
    assert profile.startswith('mixed-port: '), 'generated profile is not the documented YAML layout'
    assert 'allow-lan: false' in profile
    assert 'skip-cert-verify: false' in profile
    assert len(PROXY_NAME.findall(_proxies_section(profile))) == 1, 'expected exactly one ready entry'
    assert 'DIRECT' not in GROUP_NAME.findall(_group_section(profile)), 'DIRECT fallback added'
    with client(binary, profile) as port:
        reply = request(port, 'https://api.ipify.org')
        assert reply.returncode == 0 and reply.stdout.strip() == expected, 'Mihomo REALITY egress mismatch (curl rc=' + str(reply.returncode) + ', error=' + reply.stderr.decode(errors='replace')[:200] + ')'
        reply = request(port, 'https://www.example.com/')
        assert reply.returncode == 0 and b'Example Domain' in reply.stdout, 'Mihomo HTTPS content failed'
    # Leave this local port bound but not listening: the selected server fails,
    # while the host's direct control remains available. No fallback is allowed.
    with socket.socket() as blocked:
        blocked.bind(('127.0.0.1', 0))
        broken = re.sub(r'(?m)^(\s+)server: "[^"]*"$', r'\g<1>server: "127.0.0.1"', profile, count=1)
        broken = re.sub(r'(?m)^(\s+)port: \d+$', r'\g<1>port: ' + str(blocked.getsockname()[1]), broken, count=1)
        with client(binary, broken) as port:
            assert request(port, 'https://www.example.com/').returncode != 0, 'Clash profile fell back to DIRECT'
    print('PASS: actual Clash subscription accepted by pinned Mihomo, REALITY HTTPS egress/content and no DIRECT fallback')
