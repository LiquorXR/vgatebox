// SPDX-License-Identifier: GPL-3.0-only
// Copyright 2026 VPN Gate Toolbox contributors.
// New API boundary; the independent upstream Checker keeps its own notices.
export function toolboxCheckerGuard(request, env) {
  const url = new URL(request.url);
  const reply = (status, error) => Response.json({ error }, { status, headers: { 'Cache-Control': 'no-store' } });
  if (request.method === 'GET' && url.pathname === '/health') {
    return Response.json({ service: 'vpngate-checker', version: 1, configured: typeof env.CHECKER_TOKEN === 'string' && env.CHECKER_TOKEN.length >= 32 });
  }
  if (typeof env.CHECKER_TOKEN !== 'string' || env.CHECKER_TOKEN.length < 32) return reply(503, 'not_configured');
  if (request.headers.get('Authorization') !== `Bearer ${env.CHECKER_TOKEN}`) return reply(401, 'unauthorized');
  if (request.method !== 'GET' || url.pathname !== '/check') return reply(404, 'not_found');
  if ([...url.searchParams.keys()].some(key => key !== 'proxy') || url.searchParams.getAll('proxy').length !== 1) return reply(400, 'invalid_proxy');
  try {
    const proxy = new URL(url.searchParams.get('proxy'));
    // Official VPN Gate hostnames only: no arbitrary/private-network scanner.
    if (proxy.protocol !== 'sstp:' || !/^[a-z0-9-]+\.opengw\.net$/i.test(proxy.hostname) || proxy.search || proxy.hash || (proxy.pathname && proxy.pathname !== '/')) return reply(400, 'invalid_proxy');
    const port = Number(proxy.port || 443);
    if (!Number.isInteger(port) || port < 1 || port > 65535) return reply(400, 'invalid_proxy');
  } catch { return reply(400, 'invalid_proxy'); }
  return null;
}
