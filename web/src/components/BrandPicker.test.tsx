// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { HOSTER_LOGOS } from '../lib/hosterLogos';
import { I18nProvider } from '../lib/i18n';
import { BrandPicker, PickerSearch, brandMatches, siteOf, type Brand } from './BrandPicker';
import { Modal } from './ui';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let touch = false;

beforeEach(() => {
  touch = false;
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: query === '(hover: none)' && touch }));
  // The settings behind the window come from the server, which is not here.
  vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
  // The logos are fetched for tiles in view, which jsdom has none of.
  vi.stubGlobal('IntersectionObserver', class {
    observe() {}
    disconnect() {}
  });
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const BRANDS: Brand[] = [
  { id: 'torbox', name: 'TorBox', host: 'https://torbox.app/settings', sub: 'Connected', held: true },
  { id: 'alldebrid', name: 'AllDebrid', host: 'https://alldebrid.com/apikeys', sub: 'alldebrid.com' },
];

function Window({ onPick, onClose = () => {} }: { onPick: (id: string) => void; onClose?: () => void }) {
  const [query, setQuery] = useState('');
  return (
    <I18nProvider>
      <Modal
        title="Pick a service"
        height="capped"
        holdHeight={query !== ''}
        right={<PickerSearch value={query} onChange={setQuery} placeholder="Search this list" />}
        onClose={onClose}
      >
        <BrandPicker brands={BRANDS.filter((b) => brandMatches(b, query))} onPick={onPick} none="Nothing matches" />
      </Modal>
    </I18nProvider>
  );
}

const tiles = () => [...host.querySelectorAll('li')];
const field = () => host.querySelector<HTMLInputElement>('input[type="search"]');
const escape = () => act(() => field()!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));

function type(text: string) {
  const input = host.querySelector('input')!;
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  act(() => {
    set.call(input, text);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

it('lights a tile in its brand colour, never the accent', () => {
  act(() =>
    root.render(
      <I18nProvider>
        <BrandPicker
          brands={[
            ...BRANDS,
            { id: 'vpn', name: 'Some VPN', mark: <span />, paint: { tile: '#4ce576', ink: '#161616' } },
            { id: 'mcp', name: 'Zed', mark: <span />, tile: 'glim-tile-zed' },
          ]}
          onPick={() => {}}
          none=""
        />
      </I18nProvider>,
    ),
  );
  const [, alldebrid, vpn, mcp] = tiles();
  expect(alldebrid.style.getPropertyValue('--tile')).toBe(HOSTER_LOGOS.get('alldebrid.com')!.tile);
  expect(vpn.style.getPropertyValue('--tile')).toBe('#4ce576');
  expect(vpn.style.getPropertyValue('--tile-ink')).toBe('#161616');
  expect(mcp.className).toContain('glim-tile-zed');
  for (const tile of tiles()) {
    expect(tile.className).not.toContain('glim-tile-house');
    expect(tile.getAttribute('style') ?? '').not.toContain('accent');
  }
});

it('leaves a greyed-out brand without anything to light', () => {
  act(() =>
    root.render(
      <I18nProvider>
        <BrandPicker brands={[{ ...BRANDS[1], unavailable: 'Not here' }]} onPick={() => {}} none="" />
      </I18nProvider>,
    ),
  );
  const [tile] = tiles();
  expect(tile.className).toContain('glim-readme-btn-soon');
  expect(tile.querySelector('.glim-brand-tile, .glim-mark-hover, .glim-readme-btn-sheen')).toBeNull();
  expect(tile.getAttribute('style')).toBeNull();
});

it('draws every brand as a tile and hands back the one picked', () => {
  const picked: string[] = [];
  act(() => root.render(<Window onPick={(id) => picked.push(id)} />));
  expect(tiles().map((t) => t.querySelector('button')!.getAttribute('aria-label'))).toEqual([
    'TorBox Connected',
    'AllDebrid alldebrid.com',
  ]);
  act(() => tiles()[1].querySelector('button')!.click());
  expect(picked).toEqual(['alldebrid']);
});

it('keeps a second line that says a state in view', () => {
  act(() => root.render(<Window onPick={() => {}} />));
  expect(tiles().map((t) => t.className.includes('glim-readme-btn-unit--note'))).toEqual([true, false]);
});

it('opens with its search as a field that has the caret and narrows the tiles', () => {
  act(() => root.render(<Window onPick={() => {}} />));
  expect(host.querySelector('button[aria-label="Search"]')).toBeNull();
  expect(document.activeElement).toBe(field());
  type('all');
  expect(tiles()).toHaveLength(1);
  type('nothing like it');
  expect(host.textContent).toContain('Nothing matches');
});

it('leaves the caret out of the field under a finger, where the keyboard would cover the tiles', () => {
  touch = true;
  act(() => root.render(<Window onPick={() => {}} />));
  expect(field()).not.toBeNull();
  expect(document.activeElement).not.toBe(field());
});

it('empties its search on Escape and closes on the next one', () => {
  const close = vi.fn();
  act(() => root.render(<Window onPick={() => {}} onClose={close} />));
  type('all');
  escape();
  expect(field()!.value).toBe('');
  expect(tiles()).toHaveLength(2);
  expect(close).not.toHaveBeenCalled();
  escape();
  expect(close).toHaveBeenCalledOnce();
});

it('keeps the height it had before the search narrowed the tiles', () => {
  const height = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'offsetHeight')!;
  Object.defineProperty(HTMLElement.prototype, 'offsetHeight', {
    configurable: true,
    get(this: HTMLElement) {
      return 100 + this.querySelectorAll('li').length * 40;
    },
  });
  try {
    act(() => root.render(<Window onPick={() => {}} />));
    const dialog = host.querySelector<HTMLElement>('[role="dialog"]')!;
    expect(dialog.style.height).toBe('');
    type('all');
    expect(dialog.style.height).toBe('180px');
    type('');
    expect(dialog.style.height).toBe('');
  } finally {
    Object.defineProperty(HTMLElement.prototype, 'offsetHeight', height);
  }
});

it('names the site an address belongs to', () => {
  expect(siteOf('https://www.premiumize.me/account')).toBe('premiumize.me');
  expect(siteOf('')).toBe('');
});
