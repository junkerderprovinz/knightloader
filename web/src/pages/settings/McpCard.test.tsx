// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { McpCard } from './McpCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// jsdom has neither, and the picker's search asks whether there is a pointer.
globalThis.matchMedia ??= ((query: string) => ({ matches: false, media: query, addEventListener() {}, removeEventListener() {} })) as unknown as typeof matchMedia;
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;
let clients: object[];
let posted: unknown;

const json = (v: unknown, status = 200) =>
  Promise.resolve(new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } }));

beforeEach(() => {
  clients = [];
  posted = undefined;
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/mcp/clients' && init?.method === 'POST') {
        posted = JSON.parse(String(init.body));
        const row = { id: 'c1', kind: 'claude-code', tokenId: 't1', tokenName: 'Claude Code', scopes: ['read', 'add'], addedAt: '2026-10-15T00:00:00Z' };
        clients = [row];
        return json({ client: row, secret: 'kl_secret123' }, 201);
      }
      return json({ path: '/mcp', clients, tools: [] });
    }),
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const button = (scope: ParentNode, text: string) =>
  [...scope.querySelectorAll('button')].find((b) => b.textContent?.trim() === text || b.getAttribute('aria-label') === text);

describe('McpCard', () => {
  it('is empty until a client is added, and lists only the clients set up', async () => {
    await act(async () => root.render(<McpCard />));
    expect(host.textContent).toContain('No client connected yet.');
    expect(host.querySelectorAll('.glim-client-mark')).toHaveLength(0);
  });

  it('sets a client up from the picker and shows its token and configuration once', async () => {
    await act(async () => root.render(<McpCard />));
    await act(async () => button(host, 'Add client')!.click());

    const tile = button(document.body, 'Claude Code Terminal');
    expect(tile, 'the picker offers Claude Code').toBeDefined();
    expect(button(document.body, 'Other client')).toBeDefined();
    await act(async () => tile!.click());

    await act(async () => button(document.body, 'Connect')!.click());
    expect(posted).toEqual({ kind: 'claude-code', name: 'Claude Code', scopes: ['read', 'add'] });
    expect(document.body.textContent).toContain('kl_secret123');
    expect(document.body.textContent).toContain('claude mcp add knightloader');
    expect(document.body.textContent).toContain('Waiting for the first call.');
    // The configuration keeps the token in its file, not on the command line.
    expect(document.body.querySelector('pre')!.textContent).not.toContain('kl_secret123');
  });

  it('marks a client whose token was revoked', async () => {
    clients = [{ id: 'c1', kind: 'cursor', tokenId: 't1', tokenName: '', scopes: [], addedAt: '2026-10-15T00:00:00Z', tokenGone: true }];
    await act(async () => root.render(<McpCard />));
    expect(host.textContent).toContain('Cursor');
    expect(host.textContent).toContain('Token revoked');
  });
});
