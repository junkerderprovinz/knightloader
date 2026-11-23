// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { retryAfterPassword } from './kit';

const stale = (askPassword: boolean) =>
  new Response(JSON.stringify({ status: 403, code: 'signInAgain', params: { askPassword }, detail: 'sign in again' }), {
    status: 403,
    headers: { 'Content-Type': 'application/json' },
  });

function answers(...rs: Response[]) {
  return vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => rs.shift() ?? new Response('{}'));
}

describe('retryAfterPassword', () => {
  it('runs a change again once the password was given', async () => {
    const fetcher = answers(stale(true), new Response('{"ok":true}'));
    const ask = vi.fn(async () => true);
    const r = await retryAfterPassword(fetcher, ask)('/api/fortress/join', { method: 'POST', body: '{}' });
    expect(r.status).toBe(200);
    expect(ask).toHaveBeenCalledOnce();
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(fetcher.mock.calls[1][1]?.body).toBe('{}');
  });

  it('hands the refusal on when the question was closed', async () => {
    const fetcher = answers(stale(true));
    const r = await retryAfterPassword(fetcher, async () => false)('/api/connect', { method: 'PUT' });
    expect(r.status).toBe(403);
    expect(fetcher).toHaveBeenCalledOnce();
  });

  it('asks nothing when the password question is off', async () => {
    const fetcher = answers(stale(false));
    const ask = vi.fn(async () => true);
    const r = await retryAfterPassword(fetcher, ask)('/api/connect', { method: 'PUT' });
    expect(r.status).toBe(403);
    expect(ask).not.toHaveBeenCalled();
    expect(await r.json()).toMatchObject({ code: 'signInAgain' });
  });

  it('leaves the kit its own question and reads alone', async () => {
    const ask = vi.fn(async () => true);
    const kit = answers(stale(true));
    expect((await retryAfterPassword(kit, ask)('/kl/api/signin/v1/me/tokens', { method: 'POST' })).status).toBe(403);
    const read = answers(stale(true));
    expect((await retryAfterPassword(read, ask)('/api/fortress')).status).toBe(403);
    expect(ask).not.toHaveBeenCalled();
  });

  it('lets any other refusal through untouched', async () => {
    const fetcher = answers(new Response(JSON.stringify({ error: 'no', code: 'wrongPassword' }), { status: 403 }));
    const ask = vi.fn(async () => true);
    const r = await retryAfterPassword(fetcher, ask)('/api/connect', { method: 'PUT' });
    expect(r.status).toBe(403);
    expect(ask).not.toHaveBeenCalled();
  });
});
