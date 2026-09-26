// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import type { CaptchaSolverReport } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { SolverStatus } from './CaptchaModal';

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
