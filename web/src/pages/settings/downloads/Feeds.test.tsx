// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../../../lib/i18n';
import { SettingsProvider, type SettingsDraft } from '../context';
import { FeedsCard } from './Feeds';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  // The feed table and the priority ladder come from the server, which is not here.
  vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  const draft = {
    cfg: { feeds: [] },
    patch: () => {},
    fieldError: () => undefined,
    showRefusalsAt: () => () => {},
  } as unknown as SettingsDraft;
  act(() =>
    root.render(
      <I18nProvider>
        <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: async () => {} }}>
          <FeedsCard hue={0} />
        </SettingsProvider>
      </I18nProvider>,
    ),
  );
  const add = [...host.querySelectorAll('button')].find((b) => b.textContent === 'Add subscription')!;
  act(() => add.click());
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

/** The input of the row whose caption reads `caption`, in the open window. */
function field(caption: string): HTMLInputElement {
  const label = [...document.querySelectorAll('label')].find((l) => l.textContent?.startsWith(caption))!;
  return label.querySelector('input')!;
}

function typeAndLeave(input: HTMLInputElement, text: string) {
  // React tracks the value it last rendered, so the native setter is what
  // makes the input event count as a change.
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text);
  act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
  act(() => input.dispatchEvent(new FocusEvent('focusout', { bubbles: true })));
}

/** The line the field names in aria-describedby. */
function reason(input: HTMLInputElement): string | undefined {
  const id = input.getAttribute('aria-describedby');
  return id ? (document.getElementById(id)?.textContent ?? undefined) : undefined;
}

it('says under the address why it cannot be used', () => {
  const url = field('Feed address');
  typeAndLeave(url, 'example.org/feed.xml');
  expect(url.getAttribute('aria-invalid')).toBe('true');
  expect(reason(url)).toBe('Use a full address that starts with http:// or https://, like https://example.org/feed.xml.');
});

it('says under the title filter that the pattern does not compile', () => {
  const filter = field('Title filter');
  typeAndLeave(filter, '(1080p');
  expect(filter.getAttribute('aria-invalid')).toBe('true');
  expect(reason(filter)).toBe('This is not a valid regular expression, so the filter is not saved.');
});

it('takes the reason away once the address is typed again', () => {
  const url = field('Feed address');
  typeAndLeave(url, 'example.org/feed.xml');
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(url, 'https://example.org/feed.xml');
  act(() => url.dispatchEvent(new Event('input', { bubbles: true })));
  expect(url.getAttribute('aria-invalid')).toBe('false');
  expect(reason(url)).toBeUndefined();
});
