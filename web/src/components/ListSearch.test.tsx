// @vitest-environment jsdom
import { act, useRef, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { EMPTY_SEARCH, ListSearch, type SearchQuery } from './SearchField';
import type { SearchHandle } from './SearchToggle';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let handle: SearchHandle | null = null;
let query: SearchQuery = EMPTY_SEARCH;

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  query = EMPTY_SEARCH;
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

function Area() {
  const area = useRef<HTMLDivElement>(null);
  const [value, setValue] = useState<SearchQuery>(EMPTY_SEARCH);
  query = value;
  return (
    <div ref={area} tabIndex={-1} data-testid="area" className="relative">
      <ListSearch value={value} onChange={setValue} handle={(h) => {
        handle = h;
      }} area={area} />
      <button type="button">a row</button>
    </div>
  );
}

async function show() {
  await act(async () =>
    root.render(
      <I18nProvider>
        <Area />
      </I18nProvider>,
    ),
  );
}

const field = () => host.querySelector<HTMLInputElement>('[role="search"] input');
const press = (el: Element, key: string, init: KeyboardEventInit = {}) =>
  act(async () => {
    el.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, key, ...init }));
  });

describe('the list search', () => {
  it('stays out of sight until somebody types in the list', async () => {
    await show();
    expect(field()).toBeNull();
    await press(host.querySelector('button')!, 'f');
    expect(field()).not.toBeNull();
    expect(query.text).toBe('f');
    expect(document.activeElement).toBe(field());
  });

  it('leaves a shortcut and the space bar to the list', async () => {
    await show();
    await press(host.querySelector('button')!, 'a', { ctrlKey: true });
    await press(host.querySelector('button')!, ' ');
    expect(field()).toBeNull();
  });

  it('opens on request, for Ctrl+F', async () => {
    await show();
    await act(async () => handle!.open());
    expect(document.activeElement).toBe(field());
  });

  it('closes on Escape and lets the whole list show again', async () => {
    await show();
    await press(host.querySelector('button')!, 'x');
    await press(field()!, 'Escape');
    expect(field()).toBeNull();
    expect(query.text).toBe('');
  });
});
