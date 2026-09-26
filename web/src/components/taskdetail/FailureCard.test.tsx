// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import type { Task } from '../../lib/api';
import { I18nProvider } from '../../lib/i18n';
import { ToastProvider } from '../../lib/toast';
import { FailureCard } from './FailureCard';

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

const rejectedAtStart = {
  id: 't1',
  url: 'https://host.example/sample.mkv',
  name: 'sample.mkv',
  status: 'error',
  enabled: true,
  error: 'the server’s own sentence',
  rejectCode: 'filterRule',
  rejectParams: { rule: 'no samples' },
} as unknown as Task;

it('words the reason the queue rejected a link for, beside the untranslated message', () => {
  act(() =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <FailureCard task={rejectedAtStart} />
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  const text = host.textContent ?? '';
  expect(text).toContain('Rejected because');
  expect(text).toContain('rejected by link filter rule "no samples"');
  // The message row stays word for word, as its (i) promises.
  expect(text).toContain('the server’s own sentence');
});
