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

it('is the only search field the interface draws', () => {
  const own = Object.entries(sources)
    .filter(([path, text]) => !path.endsWith('/SearchBox.tsx') && /type="search"/.test(text))
    .map(([path]) => path);
  expect(own).toEqual([]);
});
