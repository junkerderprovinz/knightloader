// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { en } from '../../../lib/locales/en';
import { SettingsProvider, type SettingsDraft } from '../context';
import { SettingsTransfer } from './SettingsTransfer';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('../../../lib/toast', () => ({ useToast: () => ({ toast: () => {} }) }));

let root: Root;
let host: HTMLDivElement;
let passwordSet: boolean;

const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });

beforeEach(() => {
  passwordSet = false;
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path === '/api/auth') return Promise.resolve(json({ enabled: passwordSet, authenticated: true }));
      return Promise.resolve(
        json({
          dir: '/data/backups',
          defaultDir: '/data/backups',
          files: [{ name: 'knightloader-auto-20261001-030000.zip', bytes: 2048, createdAt: '2026-10-01T03:00:00Z' }],
          running: false,
          nextRunAt: null,
          last: null,
        }),
      );
    }),
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function open() {
  const draft = {
    cfg: { backupAuto: false },
    saved: { backupAuto: false },
    patch: () => {},
    dirty: false,
    fieldError: () => undefined,
    showRefusalsAt: () => () => {},
  } as unknown as SettingsDraft;
  const features = { features: { modules: [], pages: [] }, toggle: async () => {} };
  await act(async () =>
    root.render(
      <SettingsProvider draft={draft} features={features}>
        <SettingsTransfer hue={0} onShutdown={() => {}} />
      </SettingsProvider>,
    ),
  );
}

const button = (label: string) => [...host.querySelectorAll('button')].find((b) => b.textContent === label);
const autoDownload = () =>
  [...host.querySelectorAll('a')].find((a) => a.textContent === en['settings.diagnostics.fileDownload']);

describe('backup and transfer', () => {
  it('downloads every backup without a password and says kindly what a password would add', async () => {
    await open();

    expect(button(en['settings.system.backupButton'])!.disabled).toBe(false);
    expect(autoDownload()).toBeDefined();
    expect(host.textContent).toContain(en['settings.transfer.noPasswordNote'].split('{path}')[0]);
  });

  it('leaves the note out once a password is set', async () => {
    passwordSet = true;
    await open();

    expect(button(en['settings.system.backupButton'])!.disabled).toBe(false);
    expect(autoDownload()).toBeDefined();
    expect(host.textContent).not.toContain(en['settings.transfer.noPasswordNote'].split('{path}')[0]);
  });
});
