// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it } from 'vitest';

import type { EventTargetStatus } from '../../../lib/eventtargets';
import { I18nProvider } from '../../../lib/i18n';
import { SettingsProvider, type SettingsDraft } from '../context';
import { TargetRow } from './TargetRow';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

it('names the host of the address as it stands, not the one the server last reported', () => {
  const host = document.createElement('div');
  document.body.append(host);
  const root = createRoot(host);
  const draft = { cfg: {}, fieldError: () => undefined, showRefusalsAt: () => () => {} } as unknown as SettingsDraft;
  const status: EventTargetStatus = {
    id: '1',
    name: 'ntfy',
    enabled: true,
    host: 'old.example',
    lastStatus: 200,
    attempts: 1,
    sent: 1,
    dropped: 0,
  };
  act(() =>
    root.render(
      <I18nProvider>
        <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: async () => {} }}>
          <ul>
            <TargetRow
              row={{ id: '1', name: 'ntfy', enabled: true, url: 'https://new.example/topic', attempts: 0, timeoutSeconds: 0 }}
              index={0}
              last
              stored
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
  expect(host.textContent).toContain('new.example');
  expect(host.textContent).not.toContain('old.example');
  act(() => root.unmount());
  host.remove();
});
