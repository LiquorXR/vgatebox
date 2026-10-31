// SPDX-License-Identifier: GPL-2.0-only
// Copyright 2026 VPN Gate Toolbox contributors.
// This independently deployed EdgeTunnel modification is not GPL v3 code.
export function toolboxEdgeGuard(request, env) {
  const url = new URL(request.url);
  const reply = (status, error) => Response.json({ error }, { status, headers: { 'Cache-Control': 'no-store' } });
  const configured = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(env.UUID || '');
  if (request.method === 'GET' && url.pathname === '/health') return Response.json({ service: 'vpngate-edge', version: 1, configured });
  if (!configured) return reply(503, 'not_configured');
  if (request.method !== 'GET' || url.pathname !== '/' || (request.headers.get('Upgrade') || '').toLowerCase() !== 'websocket') return reply(404, 'not_found');
  if ([...url.searchParams.keys()].some(key => !['sstp', 'globalproxy'].includes(key)) || url.searchParams.getAll('sstp').length !== 1 || url.searchParams.getAll('globalproxy').length !== 1 || url.searchParams.get('globalproxy') !== '1') return reply(400, 'sstp_global_required');
  try {
    const proxy = new URL('sstp://' + url.searchParams.get('sstp'));
    // The reserved .invalid host is solely for the required failure control.
    if (!/^[a-z0-9-]+\.opengw\.net$/i.test(proxy.hostname) && proxy.hostname !== 'nonexistent.invalid') return reply(400, 'invalid_backend');
    if (proxy.search || proxy.hash || (proxy.pathname && proxy.pathname !== '/')) return reply(400, 'invalid_backend');
    const port = Number(proxy.port || 443);
    if (!Number.isInteger(port) || port < 1 || port > 65535) return reply(400, 'invalid_backend');
  } catch { return reply(400, 'invalid_backend'); }
  return null;
}
