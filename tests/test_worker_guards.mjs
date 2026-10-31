import assert from 'node:assert/strict';
import { toolboxCheckerGuard as checker } from '../worker/checker/guard.mjs';
import { toolboxEdgeGuard as edge } from '../worker/edge/guard.mjs';

const token = 'a'.repeat(64);
const uuid = '01234567-89ab-4cde-8123-456789abcdef';
const backend = 'vpn:vpn@vpn123.opengw.net:443';
const request = (path, headers = {}, method = 'GET') => new Request('https://test.workers.dev' + path, { headers, method });
const auth = { Authorization: 'Bearer ' + token };
const ws = { Upgrade: 'websocket' };
const validChecker = '/check?proxy=' + encodeURIComponent('sstp://' + backend);
const validEdge = '/?sstp=' + encodeURIComponent(backend) + '&globalproxy=1';
let count = 0;
const status = (response, expected) => { assert.equal(response?.status ?? null, expected); count++; };

status(checker(request(validChecker, auth), {}), 503);
status(checker(request(validChecker), { CHECKER_TOKEN: token }), 401);
status(checker(request(validChecker, { Authorization: 'Bearer wrong' }), { CHECKER_TOKEN: token }), 401);
status(checker(request(validChecker, auth), { CHECKER_TOKEN: token }), null);
status(checker(request(validChecker, auth, 'POST'), { CHECKER_TOKEN: token }), 404);
status(checker(request('/resolve?host=example.com', auth), { CHECKER_TOKEN: token }), 404);
for (const proxy of ['http://' + backend, 'sstp://127.0.0.1:443', 'sstp://vpn.opengw.net.evil.test:443', 'sstp://opengw.net:443', 'sstp://' + backend + '/admin']) {
  status(checker(request('/check?proxy=' + encodeURIComponent(proxy), auth), { CHECKER_TOKEN: token }), 400);
}
status(checker(request(validChecker + '&proxy=x', auth), { CHECKER_TOKEN: token }), 400);
status(checker(request(validChecker + '&sstp=x', auth), { CHECKER_TOKEN: token }), 400);
const checkerHealth = await checker(request('/health'), { CHECKER_TOKEN: token }).json();
assert.equal(checkerHealth.configured, true);
assert.ok(!JSON.stringify(checkerHealth).includes(token)); count++;

status(edge(request(validEdge, ws), {}), 503);
status(edge(request(validEdge, ws), { UUID: uuid }), null);
status(edge(request(validEdge), { UUID: uuid }), 404);
status(edge(request('/admin', ws), { UUID: uuid }), 404);
status(edge(request(validEdge, ws, 'POST'), { UUID: uuid }), 404);
for (const path of ['/', '/?sstp=' + backend, '/?sstp=' + backend + '&globalproxy=0', validEdge + '&proxyip=evil.test', validEdge + '&sstp=x', validEdge + '&globalproxy=1', '/?sstp=vpn@127.0.0.1:443&globalproxy=1', '/?sstp=vpn@vpn.opengw.net.evil.test:443&globalproxy=1']) {
  assert.ok([400, 404].includes(edge(request(path, ws), { UUID: uuid }).status)); count++;
}
status(edge(request('/?sstp=vpn%3Avpn%40nonexistent.invalid%3A443&globalproxy=1', ws), { UUID: uuid }), null);
const edgeHealth = await edge(request('/health'), { UUID: uuid }).json();
assert.equal(edgeHealth.configured, true);
assert.ok(!JSON.stringify(edgeHealth).includes(uuid)); count++;
console.log('Worker boundary checks passed:', count);
