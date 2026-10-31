// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { I18nProvider } from '../lib/i18n';
import { SearchBox } from './SearchBox';
import { SearchToggle } from './SearchToggle';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

function Box({ start }: { start: string }) {
  const [value, setValue] = useState(start);
  return <SearchBox value={value} onChange={setValue} label="Search this list" />;
}

const field = () => host.querySelector('input')!;

describe('the search field', () => {
  it('empties on Escape without passing the key on to its window', async () => {
    let reached = false;
    await act(async () =>
      root.render(
        <I18nProvider>
          <div onKeyDown={() => (reached = true)}>
            <Box start="mega" />
          </div>
        </I18nProvider>,
      ),
    );
    await act(async () => field().dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(field().value).toBe('');
    expect(reached).toBe(false);
  });

  it('empties with its cross, which shows only while there is text', async () => {
    await act(async () =>
      root.render(
        <I18nProvider>
          <Box start="mega" />
        </I18nProvider>,
      ),
    );
    await act(async () => host.querySelector<HTMLButtonElement>('button[aria-label="Clear the search"]')!.click());
    expect(field().value).toBe('');
    expect(host.querySelector('button[aria-label="Clear the search"]')).toBeNull();
  });
});

describe('the Search button', () => {
  function Toggle({ start = '' }: { start?: string }) {
    const [value, setValue] = useState(start);
    return (
      <>
        <SearchToggle value={value} onChange={setValue} label="Search this list" />
        <button type="button">elsewhere</button>
      </>
    );
  }

  const searchButton = () => host.querySelector<HTMLButtonElement>('button[aria-label="Search"]');
  const show = () =>
    act(async () =>
      root.render(
        <I18nProvider>
          <Toggle />
        </I18nProvider>,
      ),
    );
  const type = (text: string) =>
    act(async () => {
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
      set.call(field(), text);
      field().dispatchEvent(new Event('input', { bubbles: true }));
    });

  it('turns into the field, with the caret in it', async () => {
    await show();
    expect(host.querySelector('input')).toBeNull();
    await act(async () => searchButton()!.click());
    expect(document.activeElement).toBe(field());
    expect(searchButton()).toBeNull();
  });

  it('comes back on Escape in the empty field and takes the keyboard', async () => {
    await show();
    await act(async () => searchButton()!.click());
    await act(async () => field().dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(host.querySelector('input')).toBeNull();
    expect(document.activeElement).toBe(searchButton());
  });

  it('comes back when the empty field loses the focus', async () => {
    await show();
    await act(async () => searchButton()!.click());
    await act(async () => host.querySelector<HTMLButtonElement>('button:not([aria-label])')!.focus());
    expect(host.querySelector('input')).toBeNull();
  });

  it('keeps the field and the caret when a search kept from before is emptied', async () => {
    await act(async () =>
      root.render(
        <I18nProvider>
          <Toggle start="large" />
        </I18nProvider>,
      ),
    );
    await act(async () => field().focus());
    await act(async () => field().dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(field().value).toBe('');
    expect(document.activeElement).toBe(field());
  });

  it('keeps the field while it holds a search', async () => {
    await show();
    await act(async () => searchButton()!.click());
    await type('large');
    await act(async () => host.querySelector<HTMLButtonElement>('button:not([aria-label])')!.focus());
    expect(field().value).toBe('large');
  });
});

const sources = import.meta.glob(['../components/**/*.tsx', '../pages/**/*.tsx', '!../**/*.test.tsx'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

const others = Object.entries(sources).filter(([path]) => !path.endsWith('/SearchBox.tsx'));

// The places a field stands without a button before it: the settings bar that
// slides in on scrolling up, the command palette, which is nothing but its
// field and opens with the caret in it, a picker's window, which is mostly
// searched, and a list's search, which typing in the list opens.
const FIELD_FIRST = ['/SettingsSearch.tsx', '/CommandPalette.tsx', '/BrandPicker.tsx', '/SearchField.tsx'];

// Menus that name the search without being a button for it: the long press on
// a list's heads, the way in on a touch screen, where nobody types in a list.
const SEARCH_IN_MENU = ['/TaskList.tsx'];

describe('the one search field', () => {
  it('is the only search input the interface draws', () => {
    const own = others.filter(([, text]) => /type="search"/.test(text)).map(([path]) => path);
    expect(own).toEqual([]);
  });

  it('is never drawn as a plain input named for a search', () => {
    const plain = others
      .filter(([, text]) => [...text.matchAll(/<(?:input|TextInput)\b[\s\S]*?\/>/g)].some((m) => /search/i.test(m[0])))
      .map(([path]) => path);
    expect(plain).toEqual([]);
  });

  it('stands behind the Search button everywhere but the settings bar, the command palette, the pickers and the lists', () => {
    const bare = others
      .filter(([path, text]) => /<SearchBox\b/.test(text) && !path.endsWith('/SearchToggle.tsx'))
      .map(([path]) => path)
      .filter((path) => !FIELD_FIRST.some((end) => path.endsWith(end)));
    expect(bare).toEqual([]);
  });

  it('has one Search button, the one SearchToggle draws', () => {
    const own = others
      .filter(([path]) => !path.endsWith('/SearchToggle.tsx') && !SEARCH_IN_MENU.some((end) => path.endsWith(end)))
      .filter(
        ([, text]) =>
          /'search\.toggle'/.test(text) ||
          // A magnifier named for a search, rather than the one on Recheck or Find the router.
          [...text.matchAll(/icon=\{<IconSearch\b/g)].some((m) =>
            /(?:title|aria-label|label)=\{t?x?\('[^']*search/i.test(text.slice(m.index! - 300, m.index! + 300)),
          ),
      )
      .map(([path]) => path);
    expect(own).toEqual([]);
  });
});
