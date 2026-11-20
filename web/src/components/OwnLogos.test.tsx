// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { catalogEntry } from '../lib/catalogs/catalog';
import { CAPTCHA_PROVIDERS } from '../lib/catalogs/captchaProviders';
import { DOWNLOAD_PROVIDERS } from '../lib/catalogs/downloadProviders';
import { MCP_PROVIDERS } from '../lib/catalogs/mcpProviders';
import { VPN_PROVIDERS } from '../lib/catalogs/vpnProviders';
import { catalogBrands } from './CatalogTiles';
import { hasOwnLogo, ownTile } from './OwnLogos';

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

const brands = (catalog: typeof VPN_PROVIDERS | typeof MCP_PROVIDERS, ids: string[]) =>
  catalogBrands(catalog as never, {
    ids: ids as never,
    titles: new Proxy({}, { get: () => '' }) as never,
    glyph: () => 'glyph',
    lang: 'en',
  }).flatMap((g) => g.brands);

describe('the logos KnightLoader keeps until GlimStone carries them', () => {
  it('leaves every VPN and AI client to the catalog, which has a mark for each', () => {
    const ids = ['anonine', 'astrill', 'cryptostorm', 'fastestvpn', 'ivacy', 'torguard', 'tunnelbear', 'turbovpn', 'vpnac', 'xeovo'];
    for (const b of [...brands(VPN_PROVIDERS, ids), ...brands(MCP_PROVIDERS, ['jan', 'msty'])]) {
      expect(markup(b.mark), b.id).toContain('<svg');
      expect(markup(b.mark), b.id).not.toContain('.png');
    }
  });

  it('gives a debrid service the catalog has no mark for the picture bundled for its site', () => {
    expect(ownTile(catalogEntry(DOWNLOAD_PROVIDERS, 'bestdebrid')?.mark, 'https://bestdebrid.com/profile')).toEqual({
      mark: undefined,
      paint: undefined,
      tile: undefined,
      host: 'https://bestdebrid.com/profile',
    });
  });

  it('leaves a service with neither a mark nor a bundled picture to the glyph', () => {
    expect(ownTile(undefined, 'https://proleech.link/v2/jdownloader')).toBeUndefined();
    expect(hasOwnLogo('https://proleech.link/v2/jdownloader')).toBe(false);
    expect(hasOwnLogo('https://linksnappy.com/myaccount')).toBe(true);
  });

  it('leaves a service the catalog has a mark for to that mark', () => {
    expect(ownTile(catalogEntry(DOWNLOAD_PROVIDERS, 'alldebrid')?.mark, 'https://alldebrid.com/apikeys')).toBeUndefined();
    expect(ownTile(catalogEntry(CAPTCHA_PROVIDERS, 'anticaptcha')?.mark, 'https://anti-captcha.com/')).toBeUndefined();
  });
});
