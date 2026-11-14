// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from './i18n';
import { ToastProvider, useToast, type ToastAction, type ToastTone } from './toast';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let toast: ReturnType<typeof useToast>['toast'];

function Grab() {
  toast = useToast().toast;
  return null;
}

beforeEach(async () => {
  vi.useFakeTimers();
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <Grab />
        </ToastProvider>
      </I18nProvider>,
    ),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
});

const bubbles = () => host.querySelectorAll('.kl-toast').length;
const region = (live: 'polite' | 'assertive') => host.querySelector(`[aria-live="${live}"]`)!.textContent;

function show(message: string, tone: ToastTone, action?: ToastAction) {
  act(() => toast(message, tone, undefined, action));
}

it('reads a failure out assertively and anything else politely', () => {
  show('Saved', 'ok');
  act(() => vi.advanceTimersByTime(200));
  expect(region('polite')).toBe('Saved');
  expect(region('assertive')).toBe('');

  show('Could not save', 'fail');
  act(() => vi.advanceTimersByTime(200));
  expect(region('assertive')).toBe('Could not save');
});

it('gives the bubble no live role of its own', () => {
  show('Saved', 'ok');
  expect(host.querySelector('.kl-toast')!.hasAttribute('role')).toBe(false);
});

it('keeps a bubble with an action until it is dismissed', () => {
  show('Removed', 'info', { label: 'Undo', icon: null, run: () => {} });
  act(() => vi.advanceTimersByTime(60_000));
  expect(bubbles()).toBe(1);
});

it('lets a bubble with an action go when its hold runs out', () => {
  show('Removed', 'info', { label: 'Undo', icon: null, run: () => {}, holdMs: 10_000 });
  act(() => vi.advanceTimersByTime(9_000));
  expect(bubbles()).toBe(1);
  act(() => vi.advanceTimersByTime(2_000));
  expect(bubbles()).toBe(0);
});

it('lets a plain bubble go on its own', () => {
  show('Saved', 'ok');
  act(() => vi.advanceTimersByTime(5_000));
  expect(bubbles()).toBe(0);
});
