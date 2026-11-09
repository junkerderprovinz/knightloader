// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it } from 'vitest';

import type { EventTargetStatus } from '../../../lib/eventtargets';
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
