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
    const [unlocator] = brands(VPN_PROVIDERS, 'vpn', ['unlocator']);
    expect(markup(unlocator.mark)).toContain('/vpn/unlocator.png');
    expect(unlocator.paint).toMatchObject({ tile: '#668efe', ink: '#ffffff' });
    expect(unlocator.tile).toBeUndefined();
  });

  it('leaves the catalog mark where the catalog has one', () => {
    const [nord] = brands(VPN_PROVIDERS, 'vpn', ['nordvpn']);
    expect(markup(nord.mark)).not.toContain('.png');
    expect(nord.paint).toEqual({ tile: catalogEntry(VPN_PROVIDERS, 'nordvpn')!.mark!.tile, ink: '#ffffff' });
  });

  it('gives Jan, Msty, Antigravity and Visual Studio their marks', () => {
    for (const b of brands(MCP_PROVIDERS, 'mcp', ['jan', 'msty', 'antigravity', 'visualstudio'])) {
      expect(markup(b.mark), b.id).toContain('<svg');
      expect(b.tile, b.id).toBeUndefined();
    }
  });

  it('turns Cline white on the dark tile, where the lifted grey all but vanishes', () => {
    const [cline] = brands(MCP_PROVIDERS, 'mcp', ['cline']);
    expect(markup(cline.mark)).toContain('light-dark(#18181B, #ffffff)');
  });

  it('turns Windsurf white on the dark tile instead of the teal its near-black was lifted to', () => {
    const [windsurf] = brands(MCP_PROVIDERS, 'mcp', ['windsurf']);
    expect(markup(windsurf.mark)).toContain('light-dark(#0B100F, #ffffff)');
  });

  it('draws a captcha solver by the logo its site has', () => {
    expect(ownTile('captcha', 'capsolver', catalogEntry(CAPTCHA_PROVIDERS, 'capsolver')?.mark, 'https://dashboard.capsolver.com/')).toEqual({
      mark: undefined,
      paint: undefined,
      tile: undefined,
      host: 'https://dashboard.capsolver.com/',
    });
  });
});
