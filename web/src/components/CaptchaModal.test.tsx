// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { CaptchaChallenge, CaptchaSolverReport } from '../lib/api';
import { I18nProvider, useT } from '../lib/i18n';
import { CaptchaModal, SolverStatus, testResultText } from './CaptchaModal';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

/** Every bubble the window put up. */
const toasts = vi.hoisted(() => [] as { message: string; tone: string; kind?: string }[]);
vi.mock('../lib/toast', async (importOriginal) => {
  const toast = (message: string, tone: string, kind?: string) => toasts.push({ message, tone, kind });
  return { ...(await importOriginal<typeof import('../lib/toast')>()), useToast: () => ({ toast }) };
});

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
    expect(bubble).toContain('2Captcha may charge for it anyway, so KnightLoader does not send it to another captcha account');
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
    expect(line).toBe('No captcha account could take this captcha.');
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
    expect(bubble).toContain('this tab is in the background, this window is minimised or in the notification area');
  });
});

/** A socket that never connects. The last one opened is kept, so a test can
 *  hand the window a message from the instance. */
class QuietSocket {
  static OPEN = 1;
  static last: QuietSocket | null = null;
  readyState = 0;
  onopen = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onclose = null;
  constructor() {
    QuietSocket.last = this;
  }
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
  /** Every call that is not a read, as "METHOD path". */
  let calls: string[];
  /** What the instance lists as pending. */
  let pending: CaptchaChallenge[];

  beforeEach(() => {
    calls = [];
    pending = [widget];
    vi.stubGlobal('WebSocket', QuietSocket);
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        const path = new URL(url, 'http://kl.test').pathname;
        const method = init?.method ?? 'GET';
        if (method !== 'GET') calls.push(`${method} ${path}`);
        if (method !== 'GET' && path !== '/api/captcha/refresh') return new Response(null, { status: 204 });
        return new Response(JSON.stringify(pending), { status: 200, headers: { 'Content-Type': 'application/json' } });
      }),
    );
  });

  afterEach(() => vi.unstubAllGlobals());

  /** Opens the window on the widget. */
  async function open() {
    await act(async () =>
      root.render(
        <I18nProvider>
          <CaptchaModal />
        </I18nProvider>,
      ),
    );
    await act(async () => {});
  }

  /** Has the widget page report kind, as its frame does. */
  async function pageSays(kind: string) {
    await act(async () =>
      window.dispatchEvent(
        new MessageEvent('message', {
          origin: window.location.origin,
          data: { source: 'knightloader-captcha-widget', id: 'w1', kind, detail: kind === 'error' ? 'network' : null },
        }),
      ),
    );
  }

  it('tells the instance when a widget will not load in this window', async () => {
    await open();
    await pageSays('error');
    expect(calls).toEqual(['POST /api/captcha/w1/unanswerable']);
  });

  it('says nothing when the widget loads', async () => {
    await open();
    await pageSays('ready');
    await pageSays('loaded');
    expect(calls).toEqual([]);
  });

  it('takes its report back once a refresh loads the widget after all', async () => {
    await open();
    await pageSays('error');
    const refresh = [...document.querySelectorAll('button')].find((b) => b.textContent === 'Refresh');
    await act(async () => refresh!.click());
    expect(document.querySelector('iframe')).not.toBeNull();
    await pageSays('ready');
    await pageSays('loaded');
    expect(calls).toEqual([
      'POST /api/captcha/w1/unanswerable',
      'POST /api/captcha/refresh',
      'DELETE /api/captcha/w1/unanswerable',
    ]);
  });

  it('does not report the next widget for the one before it that would not load', async () => {
    pending = [widget, { ...widget, id: 'w2' }];
    await open();
    await pageSays('error');
    await act(async () =>
      QuietSocket.last!.onmessage!({
        data: JSON.stringify({ type: 'captchaResolved', data: { id: 'w1', host: 'example.net', reason: 'solved' } }),
      }),
    );
    expect(document.querySelector('iframe')).not.toBeNull();
    expect(calls).toEqual(['POST /api/captcha/w1/unanswerable']);
  });
});

describe('a test captcha', () => {
  const test: CaptchaChallenge = {
    id: 'test-1',
    source: 'test',
    host: 'KnightLoader',
    kind: 'image',
    payload: { dataUrl: 'data:image/png;base64,iVBORw0KGgo=' },
    expiresAt: new Date(Date.now() + 180_000).toISOString(),
    test: true,
  };

  const result = { correct: false, want: 'K7PQX', given: 'K7PQ' };

  beforeEach(() => {
    toasts.length = 0;
    vi.stubGlobal('WebSocket', QuietSocket);
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        const body = init?.method === 'POST' && url.endsWith('/answer') ? { stillValid: true, test: result } : [test];
        return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });
      }),
    );
  });

  afterEach(() => vi.unstubAllGlobals());

  async function open() {
    await act(async () =>
      root.render(
        <I18nProvider>
          <CaptchaModal />
        </I18nProvider>,
      ),
    );
    await act(async () => {});
  }

  async function instanceSays(type: string, data: unknown) {
    await act(async () => QuietSocket.last!.onmessage!({ data: JSON.stringify({ type, data }) }));
  }

  it('says whether the answer was right without waiting for the socket, and only once', async () => {
    await open();
    const input = document.querySelector<HTMLInputElement>('input')!;
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, 'K7PQ');
    act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
    const go = [...document.querySelectorAll('button')].find((b) => b.textContent === 'Continue')!;
    await act(async () => go.click());

    const wrong = 'Wrong. The test captcha said K7PQX, not K7PQ.';
    expect(toasts.map((t) => t.message)).toEqual([wrong]);
    const end = { id: test.id, host: test.host, reason: 'solved', test: result, testCaptcha: true };
    await instanceSays('captchaResolved', end);
    expect(toasts.map((t) => t.message)).toEqual([wrong]);
  });

  it('still says how a captcha account did when the socket brings it', async () => {
    await open();
    await instanceSays('captchaResolved', {
      id: test.id,
      host: test.host,
      reason: 'solved',
      test: { correct: true, want: 'K7PQX', given: 'K7PQX', solver: '2Captcha' },
      testCaptcha: true,
    });
    expect(toasts.map((t) => t.message)).toEqual(['2Captcha solved the test captcha: K7PQX.']);
  });

  it('offers no way to stop asking, which a test captcha would only take as a skip', async () => {
    await open();
    expect(document.body.textContent).not.toContain('More options');
  });

  it('does not report a timed-out test captcha as a stuck download', async () => {
    await open();
    await instanceSays('captchaResolved', { id: test.id, host: test.host, reason: 'timedOut', testCaptcha: true });
    expect(toasts).toHaveLength(1);
    expect(toasts[0].kind).not.toBe('captcha-failed');
  });

  it('says it is a test no download waits on, not a hoster asking', async () => {
    await act(async () =>
      root.render(
        <I18nProvider>
          <CaptchaModal />
        </I18nProvider>,
      ),
    );
    await act(async () => {});
    const text = document.body.textContent ?? '';
    expect(text).toContain('A test captcha from KnightLoader. No download waits on it.');
    expect(text).not.toContain('is asking for a captcha');
  });

  it('says whether the answer was right, and which captcha account gave it', async () => {
    let said: string[] = [];
    function Read() {
      const { t } = useT();
      said = [
        testResultText(t, { correct: true, want: 'K7PQX', given: 'k7pqx' }),
        testResultText(t, { correct: false, want: 'K7PQX', given: 'K7PQ' }),
        testResultText(t, { correct: true, want: 'K7PQX', given: 'K7PQX', solver: '2Captcha' }),
        testResultText(t, { correct: false, want: 'K7PQX', given: 'X7PQK', solver: '2Captcha' }),
      ];
      return null;
    }
    await act(async () =>
      root.render(
        <I18nProvider>
          <Read />
        </I18nProvider>,
      ),
    );
    expect(said).toEqual([
      'Right. The test captcha said K7PQX.',
      'Wrong. The test captcha said K7PQX, not K7PQ.',
      '2Captcha solved the test captcha: K7PQX.',
      '2Captcha got the test captcha wrong: it said K7PQX, not X7PQK.',
    ]);
  });
});
