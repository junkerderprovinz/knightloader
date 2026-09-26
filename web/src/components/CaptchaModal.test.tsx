// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { CaptchaChallenge, CaptchaSolverReport } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { CaptchaModal, SolverStatus } from './CaptchaModal';

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

/** Renders the status line and opens its bubble, the way a keyboard user does. */
async function show(report: CaptchaSolverReport, answerable = true) {
  await act(async () =>
    root.render(
      <I18nProvider>
        <SolverStatus report={report} now={Date.now()} answerable={answerable} />
      </I18nProvider>,
    ),
  );
  const trigger = host.querySelector<HTMLElement>('[role="note"]');
  if (trigger) act(() => trigger.focus());
  return {
    line: host.querySelector('p')!.textContent,
    bubble: document.querySelector('[role="tooltip"]')?.textContent ?? '',
  };
}

describe('SolverStatus', () => {
  it('says a solver that took the captcha kept it from the next one', async () => {
    const { line, bubble } = await show({
      state: 'stopped',
      refusals: [
        { solver: 'Anti-Captcha', code: 'unsupported' },
        { solver: '2Captcha', code: 'noAnswer', taken: true },
      ],
    });
    expect(line).toBe('2Captcha took this captcha but sent no answer.');
    expect(bubble).toContain('2Captcha may charge for it anyway, so KnightLoader does not send it to another solver');
    expect(bubble).toContain('Anti-Captcha does not solve this kind of captcha.');
    expect(bubble).toContain('2Captcha took it, but no answer came back.');
  });

  it('keeps the provider’s reason when it gave up on a task it had taken', async () => {
    const { line, bubble } = await show({
      state: 'stopped',
      refusals: [
        { solver: 'Anti-Captcha', code: 'ERROR_CAPTCHA_UNSOLVABLE', detail: 'workers could not solve it', taken: true },
      ],
    });
    expect(line).toBe('Anti-Captcha took this captcha but sent no answer.');
    expect(bubble).toContain('Anti-Captcha took it, then gave up: workers could not solve it (ERROR_CAPTCHA_UNSOLVABLE)');
  });

  it('names a decline and an unreachable solver without the raw error', async () => {
    const { line, bubble } = await show({
      state: 'stopped',
      refusals: [
        { solver: '2Captcha', code: 'ERROR_ZERO_BALANCE', detail: 'no money left' },
        { solver: 'Anti-Captcha', code: 'failed', detail: 'anticaptcha createTask: dial tcp: connection refused' },
      ],
    });
    expect(line).toBe('No solver could take this captcha.');
    expect(bubble).toContain('2Captcha declined: no money left (ERROR_ZERO_BALANCE)');
    expect(bubble).toContain('Anti-Captcha could not be reached.');
    expect(bubble).not.toContain('dial tcp');
  });

  it('says which solver is at work', async () => {
    const { line, bubble } = await show({ state: 'solving', solver: '2Captcha' });
    expect(line).toBe('2Captcha is solving this captcha.');
    expect(bubble).toContain('You can still answer it yourself.');
  });

  it('does not tell somebody to answer a captcha nobody can answer here', async () => {
    const { line, bubble } = await show({ state: 'solving', solver: '2Captcha' }, false);
    expect(line).toBe('2Captcha is solving this captcha.');
    expect(bubble).toBe('');
  });

  it('names the window as well as the tab while the solvers wait', async () => {
    const until = new Date(Date.now() + 42_000).toISOString();
    const { bubble } = await show({ state: 'waiting', until });
    expect(bubble).toContain('this tab is in the background, this window is minimised or in the tray');
  });
});

/** A socket that never connects: the window's own stream plays no part here. */
class QuietSocket {
  static OPEN = 1;
  readyState = 0;
  onopen = null;
  onmessage = null;
  onclose = null;
  send() {}
  close() {}
}

describe('CaptchaModal', () => {
  const widget: CaptchaChallenge = {
    id: 'w1',
    source: 'jd',
    host: 'example.net',
    kind: 'widget',
    payload: { vendor: 'recaptcha', siteKey: '6Lc-key', siteUrl: 'https://example.net/dl', contextUrl: '' },
    expiresAt: '0001-01-01T00:00:00Z',
  };
  let posts: string[];

  beforeEach(() => {
    posts = [];
    vi.stubGlobal('WebSocket', QuietSocket);
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        const path = new URL(url, 'http://kl.test').pathname;
        if (init?.method === 'POST') {
          posts.push(path);
          return new Response(null, { status: 204 });
        }
        return new Response(JSON.stringify([widget]), { status: 200, headers: { 'Content-Type': 'application/json' } });
      }),
    );
  });

  afterEach(() => vi.unstubAllGlobals());

  /** Opens the window on the widget and has the widget page report kind. */
  async function widgetReports(kind: string) {
    await act(async () =>
      root.render(
        <I18nProvider>
          <CaptchaModal />
        </I18nProvider>,
      ),
    );
    await act(async () => {});
    await act(async () =>
      window.dispatchEvent(
        new MessageEvent('message', {
          origin: window.location.origin,
          data: { source: 'knightloader-captcha-widget', id: 'w1', kind, detail: 'network' },
        }),
      ),
    );
  }

  it('tells the instance when a widget will not load in this window', async () => {
    await widgetReports('error');
    expect(posts).toEqual(['/api/captcha/w1/unanswerable']);
  });

  it('says nothing when the widget loads', async () => {
    await widgetReports('ready');
    expect(posts).toEqual([]);
  });
});
