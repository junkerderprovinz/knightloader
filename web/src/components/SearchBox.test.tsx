// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { I18nProvider } from '../lib/i18n';
import { SearchBox } from './SearchBox';

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

const sources = import.meta.glob(['../components/**/*.tsx', '../pages/**/*.tsx', '!../**/*.test.tsx'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

const others = Object.entries(sources).filter(([path]) => !path.endsWith('/SearchBox.tsx'));

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

  it('stands as a field and never behind a Search button', () => {
    const buttons = others
      .filter(
        ([, text]) =>
          /'search\.toggle'/.test(text) ||
          [...text.matchAll(/icon=\{<IconSearch\b/g)].some((m) =>
            /(?:title|aria-label|label)=\{t?x?\('[^']*search/i.test(text.slice(m.index! - 300, m.index! + 300)),
          ),
      )
      .map(([path]) => path);
    expect(buttons).toEqual([]);
  });
});
