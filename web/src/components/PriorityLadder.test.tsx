// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { PriorityLadder } from './PriorityLadder';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

it('moves a row several places with the arrow keys, one store after the other', async () => {
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  const stores: string[][] = [];
  let finish = () => {};
  function Ladder() {
    const [ids, setIds] = useState(['a', 'b', 'c']);
    const [busy, setBusy] = useState(false);
    return (
      <PriorityLadder
        ids={ids}
        title="Order"
        entry="Link"
        labelFor={(id) => id.toUpperCase()}
        busy={busy}
        onReorder={(next) => {
          stores.push(next);
          setBusy(true);
          return new Promise<void>((done) => {
            finish = () => {
              setIds(next);
              setBusy(false);
              done();
            };
          });
        }}
      />
    );
  }
  const host = document.createElement('div');
  document.body.append(host);
  const root = createRoot(host);
  await act(async () =>
    root.render(
      <I18nProvider>
        <Ladder />
      </I18nProvider>,
    ),
  );
  const grip = () => host.querySelector<HTMLButtonElement>('[data-ladder-id="a"] button')!;
  const down = () => act(async () => grip().dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })));

  grip().focus();
  await down();
  expect(document.activeElement).toBe(grip());
  expect(grip().disabled).toBe(false);
  expect(grip().getAttribute('aria-disabled')).toBe('true');
  // A key while the first store is out is not a second move.
  await down();
  expect(stores).toEqual([['b', 'a', 'c']]);

  await act(async () => finish());
  expect(document.activeElement).toBe(grip());
  expect(grip().getAttribute('aria-disabled')).toBeNull();
  await down();
  expect(stores).toEqual([
    ['b', 'a', 'c'],
    ['b', 'c', 'a'],
  ]);

  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});
