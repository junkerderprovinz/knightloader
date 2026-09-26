// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import { I18nProvider } from '../../../lib/i18n';
import { TargetEvents } from './TargetEvents';

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

it('offers every event but the manual run, which never reaches a target or a program', () => {
  act(() =>
    root.render(
      <I18nProvider>
        <TargetEvents triggers={['task.done', 'queue.idle', 'manual']} picked={[]} hue={0} onChange={() => {}} />
      </I18nProvider>,
    ),
  );
  const text = host.textContent ?? '';
  expect(text).toContain('A download finishes');
  expect(text).not.toContain('Manual (on demand only)');
});
