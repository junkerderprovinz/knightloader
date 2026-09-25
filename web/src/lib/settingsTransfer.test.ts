import { describe, expect, it } from 'vitest';

import type { SettingsExportDoc } from './api';
import { diffRows } from './settingsTransfer';

const schema = {
  values: { stallTimeout: 120, stallReconnect: true, autoStart: true, autoConfirm: false },
  kinds: { stallTimeout: 'number', stallReconnect: 'bool', autoStart: 'bool', autoConfirm: 'bool' },
};

function exported(settings: Record<string, unknown>): SettingsExportDoc {
  return { kind: 'knightloader-settings', version: 'v0.9.0', settings } as unknown as SettingsExportDoc;
}

function row(settings: Record<string, unknown>, stored: Record<string, unknown>, key: string) {
  const found = diffRows(exported(settings), stored, schema).find((r) => r.key === key);
  if (!found) throw new Error(`no row for ${key}`);
  return found;
}

describe('the import preview', () => {
  it('shows that an older build’s stall timeout of 0 is taken over as the default', () => {
    const r = row({ stallTimeout: 0, stallRestart: false }, { stallTimeout: 120 }, 'stallTimeout');
    expect(r.incoming).toBe(0);
    expect(r.arrives).toBe(120);
    expect(r.same).toBe(true);
  });

  it('takes over a stall timeout of 0 chosen beside the reconnect switch as it is', () => {
    const r = row({ stallTimeout: 0, stallReconnect: true }, { stallTimeout: 120 }, 'stallTimeout');
    expect(r.arrives).toBe(0);
    expect(r.same).toBe(false);
  });

  it('takes over an older build’s autoStart as the start it always made', () => {
    expect(row({ autoStart: false }, { autoStart: true }, 'autoStart').arrives).toBe(true);
    expect(row({ autoStart: false, autoConfirm: false }, { autoStart: true }, 'autoStart').arrives).toBe(false);
  });

  it('marks event programs whose command line stayed behind', () => {
    const program = (value: string) => ({ eventPrograms: [{ id: 'a1', command: { program: value } }] });
    expect(row(program('********'), {}, 'eventPrograms').secretless).toBe(true);
    expect(row(program('/usr/local/bin/file-it'), {}, 'eventPrograms').secretless).toBe(false);
  });
});
