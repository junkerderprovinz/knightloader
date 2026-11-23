// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it } from 'vitest';

import type { Channel, EventTargetStatus } from '../../../lib/eventtargets';
import { I18nProvider } from '../../../lib/i18n';
import { SettingsProvider, type SettingsDraft } from '../context';
import { TargetRow } from './TargetRow';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

it('names the kind and the host of the address, without its query', () => {
  const host = document.createElement('div');
  document.body.append(host);
  const root = createRoot(host);
  const draft = { cfg: {}, fieldError: () => undefined, showRefusalsAt: () => () => {} } as unknown as SettingsDraft;
  const status: EventTargetStatus = { id: '1', name: 'ntfy', kind: 'ntfy', enabled: true, lastStatus: 0, attempts: 1, sent: 1, dropped: 0 };
  act(() =>
    root.render(
      <I18nProvider>
        <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: async () => {} }}>
          <ul>
            <TargetRow
              row={{ id: '1', kind: 'ntfy', name: 'phone', enabled: true, ntfy: { url: 'https://new.example/topic?auth=secret' } }}
              index={0}
              last
              open={false}
              triggers={[]}
              placeholders={[]}
              status={status}
              onToggle={() => {}}
              onChange={() => {}}
              onRemove={() => {}}
            />
          </ul>
        </SettingsProvider>
      </I18nProvider>,
    ),
  );
  expect(host.textContent).toContain('ntfy · new.example');
  expect(host.textContent).not.toContain('secret');
  act(() => root.unmount());
  host.remove();
});

/** A webhook row that takes its own edits, as the settings draft does. */
function WebhookRow() {
  const [row, setRow] = useState<Channel>({ id: '1', kind: 'webhook', name: 'ntfy', enabled: false, webhook: { url: '' } });
  return (
    <TargetRow
      row={row}
      index={0}
      last
      open
      triggers={[]}
      placeholders={[]}
      onToggle={() => {}}
      onChange={setRow}
      onRemove={() => {}}
    />
  );
}

/** A webhook row with its window open, and the box whose caption reads `caption`. */
function openRow(caption: string) {
  const host = document.createElement('div');
  document.body.append(host);
  const root = createRoot(host);
  const draft = { cfg: {}, fieldError: () => undefined, showRefusalsAt: () => () => {} } as unknown as SettingsDraft;
  act(() =>
    root.render(
      <I18nProvider>
        <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: async () => {} }}>
          <ul>
            <WebhookRow />
          </ul>
        </SettingsProvider>
      </I18nProvider>,
    ),
  );
  const label = [...document.querySelectorAll('label')].find((l) => l.textContent?.startsWith(caption))!;
  const box = label.querySelector<HTMLInputElement | HTMLTextAreaElement>('input, textarea')!;
  return {
    box,
    leaveWith(text: string) {
      // React tracks the value it last rendered, so the native setter is what
      // makes the input event count as a change.
      const proto = box instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(box, text);
      act(() => box.dispatchEvent(new Event('input', { bubbles: true })));
      act(() => box.dispatchEvent(new FocusEvent('focusout', { bubbles: true })));
    },
    /** The line the box names in aria-describedby. */
    reason() {
      const id = box.getAttribute('aria-describedby');
      return id ? document.getElementById(id)?.textContent : undefined;
    },
    close() {
      act(() => root.unmount());
      host.remove();
    },
  };
}

it('says under the address why it cannot be used', () => {
  const url = openRow('Address');
  url.leaveWith('ntfy.example/my-topic');
  expect(url.box.getAttribute('aria-invalid')).toBe('true');
  expect(url.reason()).toBe('Use a full address that starts with http:// or https://, and close every placeholder with %%.');
  url.close();
});

it('says under the headers how a line is written', () => {
  const headers = openRow('Headers');
  headers.leaveWith('Authorization Bearer abc');
  expect(headers.box.getAttribute('aria-invalid')).toBe('true');
  expect(headers.reason()).toBe('Write each line as a name, a colon and a value, like Content-Type: text/plain.');
  headers.close();
});

it('says under the headers that a placeholder is left open', () => {
  const headers = openRow('Headers');
  headers.leaveWith('X-Title: %%task.name');
  expect(headers.reason()).toBe('A placeholder is not closed. Each one starts and ends with %%, like %%task.name%%.');
  headers.close();
});

it('says under the body that a placeholder is left open', () => {
  const body = openRow('Body template');
  body.leaveWith('Done: %%task.name');
  expect(body.box.getAttribute('aria-invalid')).toBe('true');
  expect(body.reason()).toBe('A placeholder is not closed. Each one starts and ends with %%, like %%task.name%%.');
  body.close();
});
