// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { catalogEntry } from '../lib/catalogs/catalog';
import { CAPTCHA_PROVIDERS } from '../lib/catalogs/captchaProviders';
import { MCP_PROVIDERS } from '../lib/catalogs/mcpProviders';
import { VPN_PROVIDERS } from '../lib/catalogs/vpnProviders';
import { catalogBrands } from './CatalogTiles';
import { ownTile } from './OwnLogos';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// A logo is drawn once its tile scrolls into view, which jsdom has none of.
beforeAll(() => {
  vi.stubGlobal('IntersectionObserver', class {
    observe() {}
    disconnect() {}
  });
});

const markup = (node: unknown) => {
  const host = document.createElement('div');
  const root = createRoot(host);
  act(() => root.render(node as never));
  const html = host.innerHTML;
  act(() => root.unmount());
  return html;
};

const brands = (catalog: typeof VPN_PROVIDERS | typeof MCP_PROVIDERS, kind: 'vpn' | 'mcp', ids: string[]) =>
  catalogBrands(
    catalog as never,
    { ids: ids as never, titles: new Proxy({}, { get: () => '' }) as never, glyph: () => 'glyph', lang: 'en' },
    undefined,
    (id, mark) => ownTile(kind, id, mark),
  ).flatMap((g) => g.brands);

describe('the logos KnightLoader keeps until GlimStone carries them', () => {
  it('gives a VPN the catalog has only a glyph for the logo it had', () => {
    const [ivacy] = brands(VPN_PROVIDERS, 'vpn', ['ivacy']);
    expect(markup(ivacy.mark)).toContain('/vpn/ivacy.png');
    expect(ivacy.paint).toMatchObject({ tile: '#24aaf6', ink: '#ffffff' });
    expect(ivacy.tile).toBeUndefined();
  });

  it('leaves the catalog mark where the catalog has one', () => {
    for (const id of ['nordvpn', 'unlocator', 'windscribe']) {
      const [b] = brands(VPN_PROVIDERS, 'vpn', [id]);
      expect(markup(b.mark), id).not.toContain('.png');
      expect(b.paint?.tile, id).toBe(catalogEntry(VPN_PROVIDERS, id)!.mark!.tile);
    }
  });

  it('gives Jan, Msty, Antigravity and Visual Studio their marks', () => {
    for (const b of brands(MCP_PROVIDERS, 'mcp', ['jan', 'msty', 'antigravity', 'visualstudio'])) {
      expect(markup(b.mark), b.id).toContain('<svg');
      expect(b.tile, b.id).toBeUndefined();
    }
  });

  it('draws a captcha solver by the logo its site has', () => {
    expect(ownTile('captcha', 'anticaptcha', catalogEntry(CAPTCHA_PROVIDERS, 'anticaptcha')?.mark, 'https://anti-captcha.com/')).toEqual({
      mark: undefined,
      paint: undefined,
      tile: undefined,
      host: 'https://anti-captcha.com/',
    });
  });

  it('leaves a captcha solver the catalog has a mark for to that mark', () => {
    expect(ownTile('captcha', 'capsolver', catalogEntry(CAPTCHA_PROVIDERS, 'capsolver')?.mark, 'https://dashboard.capsolver.com/')).toBeUndefined();
  });
});
