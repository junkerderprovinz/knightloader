import { beforeEach, describe, expect, it } from 'vitest';

import { ApiError, type VpnState } from './api';
import { CONNECT_MS, mockVpn, resetMockVpn } from './vpnMock';

const post = (fields: Record<string, string>) => ({
  method: 'POST',
  body: JSON.stringify({ fields }),
});
const find = (s: VpnState, id: string) => s.providers.find((p) => p.id === id)!;
const CONF = '[Interface]\nPrivateKey = x\nAddress = 10.2.0.2/32\n[Peer]\n';

describe('mockVpn', () => {
  beforeEach(resetMockVpn);

  it('starts with torrents on no VPN and every provider idle', async () => {
    const s = await mockVpn('', undefined, 0);
    expect(s.active).toBe('');
    expect(s.providers.length).toBeGreaterThanOrEqual(20);
    expect(s.providers.every((p) => p.state === 'idle' && !p.configured)).toBe(true);
  });

  it('connects after a while, with an address and a port where the provider forwards one', async () => {
    const started = await mockVpn('/protonvpn/connect', post({ config: CONF }), 0);
    expect(started.active).toBe('protonvpn');
    expect(find(started, 'protonvpn').state).toBe('connecting');

    const up = find(await mockVpn('', undefined, CONNECT_MS), 'protonvpn');
    expect(up.state).toBe('connected');
    expect(up.exitIp).toMatch(/^203\.0\.113\.\d+$/);
    expect(up.forwardedPort).toBeGreaterThan(0);
    expect(up.configured).toBe(true);
  });

  it('keeps one provider active, so a second connect ends the first', async () => {
    await mockVpn('/protonvpn/connect', post({ config: CONF }), 0);
    const s = await mockVpn('/mullvad/connect', post({ accountNumber: '1234 5678' }), 0);
    expect(s.active).toBe('mullvad');
    expect(find(s, 'protonvpn').state).toBe('idle');
    expect(find(s, 'protonvpn').configured).toBe(true);
    expect(find(await mockVpn('', undefined, CONNECT_MS), 'mullvad').forwardedPort).toBeUndefined();
  });

  it('fails a config that is not one, with a code the page words itself', async () => {
    await mockVpn('/custom/connect', post({ config: 'hello' }), 0);
    const p = find(await mockVpn('', undefined, CONNECT_MS), 'custom');
    expect(p.state).toBe('failed');
    expect(p.errorCode).toBe('vpn-config-invalid');
  });

  it('refuses a connect that leaves a needed field empty and names the field', async () => {
    const refused = await mockVpn('/pia/connect', post({ username: 'me' }), 0).catch((e: unknown) => e);
    expect(refused).toBeInstanceOf(ApiError);
    expect((refused as ApiError).field).toBe('password');
  });

  it('reuses a stored login when the fields come empty', async () => {
    await mockVpn('/pia/connect', post({ username: 'me', password: 'secret' }), 0);
    await mockVpn('/disconnect', { method: 'POST' }, 1);
    const s = await mockVpn('/pia/connect', post({ username: '', password: '' }), 2);
    expect(s.active).toBe('pia');
  });

  it('forgets a login and ends its connection', async () => {
    await mockVpn('/pia/connect', post({ username: 'me', password: 'secret' }), 0);
    const s = await mockVpn('/pia', { method: 'DELETE' }, 1);
    expect(s.active).toBe('');
    expect(find(s, 'pia').configured).toBe(false);
  });
});
