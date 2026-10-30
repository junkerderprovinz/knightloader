// @vitest-environment jsdom
import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { setLabelMode } from '../lib/labelModes';
import { Button, IconBadge, LinkBadge } from './ui';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

const glyph = <svg data-glyph />;

beforeEach(() => {
  vi.useFakeTimers();
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  setLabelMode('buttons', 'both');
  vi.useRealTimers();
});

/** Draws the control, rests the pointer on it and says whether a bubble opened. */
function bubbleOver(control: ReactNode, selector: string): boolean {
  act(() => root.render(<I18nProvider>{control}</I18nProvider>));
  const el = host.querySelector(selector) as HTMLElement;
  act(() => {
    el.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
    vi.advanceTimersByTime(1000);
  });
  return document.querySelector('[role="tooltip"]') !== null;
}

it('opens no bubble over a button that shows its own name', () => {
  expect(bubbleOver(<Button labelled icon={glyph} title="Filter" />, 'button')).toBe(false);
});

it('names a button that shows its glyph alone', () => {
  setLabelMode('buttons', 'glyph');
  expect(bubbleOver(<Button labelled icon={glyph} title="Filter" />, 'button')).toBe(true);
});

it('opens no bubble over a badge that shows its own name', () => {
  expect(bubbleOver(<IconBadge labelled icon={glyph} title="Sort" />, 'button')).toBe(false);
});

it('names a badge that shows its glyph alone', () => {
  expect(bubbleOver(<IconBadge icon={glyph} title="Sort" />, 'button')).toBe(true);
});

it('keeps what a badge does beyond its name', () => {
  setLabelMode('buttons', 'glyph');
  expect(bubbleOver(<IconBadge labelled icon={glyph} title="Sort" hint="By size, largest first" />, 'button')).toBe(true);
});

it('opens no bubble over a link that shows its own name', () => {
  expect(bubbleOver(<LinkBadge href="https://example.org" title="Where to find it" />, 'a')).toBe(false);
});

/** Draws a button and returns it. */
function draw(control: ReactNode): HTMLButtonElement {
  act(() => root.render(<I18nProvider>{control}</I18nProvider>));
  return host.querySelector('button')!;
}

it('shows the glyph alone in glyph mode and names the button with its words', () => {
  setLabelMode('buttons', 'glyph');
  const button = draw(<Button icon={glyph}>Paste from clipboard</Button>);
  expect(button.textContent).toBe('');
  expect(button.querySelector('[data-glyph]')).not.toBeNull();
  expect(button.getAttribute('aria-label')).toBe('Paste from clipboard');
  expect(bubbleOver(<Button icon={glyph}>Paste from clipboard</Button>, 'button')).toBe(true);
});

it('shows the glyph alone in hover mode as well', () => {
  setLabelMode('buttons', 'hover');
  expect(draw(<Button icon={glyph}>State</Button>).getAttribute('aria-label')).toBe('State');
});

it('shows the words alone in text mode', () => {
  setLabelMode('buttons', 'text');
  const button = draw(<Button icon={glyph}>Release notes</Button>);
  expect(button.textContent).toBe('Release notes');
  expect(button.querySelector('[data-glyph]')).toBeNull();
});

it('names a button whose words come with more than text through its label', () => {
  setLabelMode('buttons', 'glyph');
  const button = draw(
    <Button icon={glyph} label="New · 3">
      <span>New</span>
      <span>3</span>
    </Button>,
  );
  expect(button.textContent).toBe('');
  expect(button.getAttribute('aria-label')).toBe('New · 3');
});
