// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { WindowBar } from './WindowBar';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

it('draws no title bar in a browser', async () => {
  const host = document.createElement('div');
  document.body.append(host);
  const root = createRoot(host);
  await act(async () =>
    root.render(
      <I18nProvider>
        <WindowBar />
      </I18nProvider>,
    ),
  );
  expect(host.innerHTML).toBe('');
  act(() => root.unmount());
  host.remove();
});
