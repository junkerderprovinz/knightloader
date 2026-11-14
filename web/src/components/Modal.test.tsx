// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { Modal } from './ui';

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

// A list row opens its failure advice in place, inside a cell that truncates
// and a row that cannot be selected, and a window takes neither from it.
it('wraps and lets its text be selected when opened inside a truncated row', async () => {
  await act(async () => {
    root.render(
      <I18nProvider>
        <div className="select-none">
          <div className="truncate">
            <Modal title="Why this failed" onClose={() => {}}>
              <p>A sentence long enough to need a second line.</p>
            </Modal>
          </div>
        </div>
      </I18nProvider>,
    );
  });
  const backdrop = document.querySelector('[role=dialog]')!.parentElement!;
  expect(backdrop.classList).toContain('whitespace-normal');
  expect(backdrop.classList).toContain('select-text');
});
