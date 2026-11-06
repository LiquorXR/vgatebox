#!/usr/bin/env bash
set -euo pipefail
[[ -f /.dockerenv && $(cat /proc/1/comm) == systemd && -d /package ]] || exit 1
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
run_install() {
  if ! bash /src/vps/install.sh --package /package/vgatebox-linux-amd64.tar.gz >/tmp/install.log 2>&1; then
    tail -120 /tmp/install.log
    journalctl -u vgatebox-fanout --no-pager -n 40
    /usr/local/bin/vgatebox-fanout -check-state -dir /var/lib/vgatebox/fanout || true
    exit 1
  fi
}
# Deterministic upstream outage: a closed loopback HTTPS port, no TLS bypass or
# public host modification. Keep it configured through install and update.
mkdir -p /etc/vgatebox
# Keep source-outage acceptance deterministic even with default mirror fallback.
# The test starts the source service explicitly after saving its local settings.
mkdir -p /run/systemd/system/vgatebox-pool.timer.d
printf '%s\n' '[Timer]' 'OnBootSec=' 'OnUnitActiveSec=' 'OnActiveSec=1d' > /run/systemd/system/vgatebox-pool.timer.d/fixture.conf
# Python is an installer dependency and may not exist in the pristine image.
sed -e 's#"source_urls":.*#"source_urls": ["https://127.0.0.1:9/"],#' \
    -e 's/"node_timeout_seconds": 30/"node_timeout_seconds": 2/' \
    /src/config/settings.json > /etc/vgatebox/pool.json
chmod 600 /etc/vgatebox/pool.json
run_install
grep -q '先完成后台安装' /tmp/install.log
test ! -e /var/lib/vgatebox/pool/node_pool.json
systemctl is-active --quiet vgatebox-fanout.service
systemctl is-enabled --quiet vgatebox-pool.timer
python3 /src/tests/distro_verify.py save
ip netns add vgb-ci
ip netns exec vgb-ci ip link set lo up
ip netns exec vgb-ci iptables -A OUTPUT -j REJECT
ip netns exec vgb-ci iptables -C OUTPUT -j REJECT
ip netns delete vgb-ci
run_install
python3 /src/tests/distro_verify.py compare-empty
test ! -e /var/lib/vgatebox/pool/node_pool.json
python3 - <<'PY'
import json
from pathlib import Path
status = json.loads(Path('/var/lib/vgatebox-deploy/status.json').read_text())
assert status['status'] == 'completed' and status['warning'] == 'pool_not_ready'
PY
# Recover through a controlled HTTPS source. Trust only its generated CI CA in
# this disposable service; TLS verification remains enabled. No live VPN claim.
# Auto-allocation may dial the synthetic profile. Keep that exact destination
# local to this disposable test; no VPN traffic goes to the public fixture IP.
iptables -I OUTPUT -d 8.8.8.8 -p tcp --dport 1194 -j REJECT
iptables -I FORWARD -d 8.8.8.8 -p tcp --dport 1194 -j REJECT
command -v openssl >/dev/null || apt-get install -y openssl
python3 /src/tests/pool_source_fixture.py >/tmp/source-fixture.log 2>&1 &
source_pid=$!
trap 'kill "$source_pid" 2>/dev/null || true' EXIT
for attempt in {1..30}; do
  test ! -f /tmp/vgb-ci-source/port || break
  sleep 1
done
test -f /tmp/vgb-ci-source/port || { cat /tmp/source-fixture.log; exit 1; }
python3 - <<'PY'
import json
from pathlib import Path
config = json.loads(Path('/etc/vgatebox/pool.json').read_text())
config['source_urls'] = ['https://127.0.0.1:' + Path('/tmp/vgb-ci-source/port').read_text() + '/']
Path('/etc/vgatebox/pool.json').write_text(json.dumps(config))
PY
mkdir -p /run/systemd/system/vgatebox-pool.service.d
if env -u SSL_CERT_FILE -u SSL_CERT_DIR python3 /usr/local/lib/vgatebox/scripts/toolbox.py --config /etc/vgatebox/pool.json pool --output /tmp/vgb-untrusted-source >/tmp/untrusted-source.log 2>&1; then
  echo 'FAIL: untrusted source certificate was accepted'
  exit 1
fi
test ! -e /tmp/vgb-untrusted-source/node_pool.json
printf '%s\n' '[Service]' 'Environment=SSL_CERT_FILE=/tmp/vgb-ci-source/cert.pem' > /run/systemd/system/vgatebox-pool.service.d/fixture.conf
systemctl daemon-reload
if ! systemctl start vgatebox-pool.service; then
  journalctl -u vgatebox-pool.service --no-pager -n 35
  systemctl show vgatebox-pool.service -p Result -p ExecMainStatus -p ActiveState -p SubState
  exit 1
fi
python3 - <<'PY'
import json
from pathlib import Path
p=Path('/var/lib/vgatebox/fanout')
s=p/'state'
Path('/tmp/vgb-ci-automation-settings').write_bytes((s/'automation.json').read_bytes())
Path('/tmp/vgb-ci-before-refresh').write_bytes((s/'refresh-state.json').read_bytes())
settings=json.loads((s/'automation.json').read_text());settings['refresh_enabled']=False
(s/'automation.json').write_text(json.dumps(settings))
PY
systemctl start vgatebox-pool.service
cmp /tmp/vgb-ci-before-refresh /var/lib/vgatebox/fanout/state/refresh-state.json
SSL_CERT_FILE=/tmp/vgb-ci-source/cert.pem vgatebox refresh >/tmp/manual-source.log
python3 - <<'PY'
from pathlib import Path
p=Path('/var/lib/vgatebox/fanout')
s=p/'state'
assert (s/'refresh-state.json').read_bytes()!=Path('/tmp/vgb-ci-before-refresh').read_bytes(), 'Manual CLI refresh was gated by disabled automatic schedule'
(s/'automation.json').write_bytes(Path('/tmp/vgb-ci-automation-settings').read_bytes())
PY
python3 - <<'PY'
import json
from pathlib import Path
pool = json.loads(Path('/var/lib/vgatebox/pool/node_pool.json').read_text())
assert pool['nodes'] and pool['generated_at'] and pool['source']['name'].startswith('https://')
assert pool['nodes'][0]['hostname'] == 'ci-synthetic.example.invalid'
PY
python3 /src/tests/distro_verify.py bootstrap
run_install
python3 /src/tests/distro_verify.py compare
python3 /src/tests/distro_verify.py disable
run_install
python3 /src/tests/distro_verify.py disabled
rm -rf /run/systemd/system/vgatebox-pool.service.d
rm -rf /run/systemd/system/vgatebox-pool.timer.d
systemctl daemon-reload
test -n "$(find /var/backups/vgatebox -name project.tar.gz -print -quit)"
vgatebox uninstall --yes --purge --legacy
iptables -D OUTPUT -d 8.8.8.8 -p tcp --dport 1194 -j REJECT
iptables -D FORWARD -d 8.8.8.8 -p tcp --dport 1194 -j REJECT
test ! -e /usr/local/lib/vgatebox
test ! -e /var/lib/vgatebox
test ! -e /var/backups/vgatebox
echo 'PASS: source outage install/update, real subscription, controlled HTTPS recovery, normal update, network isolation, uninstall'
