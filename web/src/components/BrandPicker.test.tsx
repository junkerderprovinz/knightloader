// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { BrandPicker, PickerSearch, brandMatches, siteOf, type Brand } from './BrandPicker';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
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

function Window({ onPick }: { onPick: (id: string) => void }) {
  const [query, setQuery] = useState('');
  return (
    <I18nProvider>
      <PickerSearch value={query} onChange={setQuery} placeholder="Search this list" />
      <BrandPicker brands={BRANDS.filter((b) => brandMatches(b, query))} onPick={onPick} none="Nothing matches" />
    </I18nProvider>
  );
}

const tiles = () => [...host.querySelectorAll('li')];

function type(text: string) {
  const input = host.querySelector('input')!;
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  act(() => {
    set.call(input, text);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

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

it('narrows the tiles with its field, which has the caret from the start', () => {
  act(() => root.render(<Window onPick={() => {}} />));
  expect(document.activeElement).toBe(host.querySelector('input[type="search"]'));
  type('all');
  expect(tiles()).toHaveLength(1);
  type('nothing like it');
  expect(host.textContent).toContain('Nothing matches');
});

it('names the site an address belongs to', () => {
  expect(siteOf('https://www.premiumize.me/account')).toBe('premiumize.me');
  expect(siteOf('')).toBe('');
});
