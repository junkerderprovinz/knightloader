// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import type { HealthReport, HealthSubsystem } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { PartsCard } from './Parts';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let at = '';

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

function Where() {
  const { pathname, search } = useLocation();
  at = pathname + search;
  return null;
}

async function draw(subsystems: HealthSubsystem[]) {
  await act(async () =>
    root.render(
      <I18nProvider>
        <MemoryRouter>
          <PartsCard hue={0} report={{ subsystems } as HealthReport} />
          <Where />
        </MemoryRouter>
      </I18nProvider>,
    ),
  );
}

const row = (name: string) => [...host.querySelectorAll('li')].find((li) => li.textContent?.startsWith(name))!;

describe('a part with a fault', () => {
  it('says why the queue is not running', async () => {
    await draw([{ id: 'queue', state: 'degraded', remedy: 'queue.halted' }]);
    expect(row('Queue').textContent).toContain('The queue is stopped');
  });

  it('opens the log at its own lines', async () => {
    await draw([{ id: 'jd', state: 'failed', remedy: 'jd.unreachable', detail: 'connection refused', log: 'JD' }]);
    expect(row('JDownloader').textContent).toContain('connection refused');
    const open = [...row('JDownloader').querySelectorAll('button')].find((b) => b.textContent === 'Open the log')!;
    await act(async () => open.click());
    expect(at).toBe('/settings/diagnostics?log=JD');
  });

  it('says it gave no reason rather than nothing at all', async () => {
    await draw([{ id: 'store', state: 'failed' }]);
    expect(host.textContent).toContain('This part gave no reason.');
  });

  it('leads nowhere while it is fine', async () => {
    await draw([{ id: 'relay', state: 'ok', log: 'relay' }]);
    expect(host.querySelector('button')).toBeNull();
  });
});
