import { describe, expect, it } from 'vitest';

import type { SettingsExportDoc } from './api';
import { diffRows, parseExport } from './settingsTransfer';

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

/** parsedRow is row for a file read the way the import page reads it. */
function parsedRow(settings: Record<string, unknown>, stored: Record<string, unknown>, key: string) {
  const doc = parseExport(JSON.stringify(exported(settings)));
  const found = diffRows(doc, stored, schema).find((r) => r.key === key);
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

  it('takes over a bottom bar that followed the sidebar as the sidebar’s mode', () => {
    const settings = { navLabels: 'hover', bottomBarLabels: 'follow' };
    expect(parsedRow(settings, { bottomBarLabels: 'hover' }, 'bottomBarLabels').same).toBe(true);
    expect(row({ bottomBarLabels: 'glyph' }, { bottomBarLabels: 'both' }, 'bottomBarLabels').arrives).toBe('glyph');
  });

  it('offers the four label settings of a file written before the split', () => {
    const settings = { navLabels: 'glyph' };
    for (const key of ['buttonLabels', 'sidebarLabels', 'tabLabels', 'bottomBarLabels']) {
      expect(parsedRow(settings, { [key]: 'both' }, key).arrives).toBe('glyph');
    }
    const doc = parseExport(JSON.stringify(exported(settings)));
    expect(diffRows(doc, {}, schema).some((r) => r.key === 'navLabels')).toBe(false);
  });

  it('marks archive passwords as missing only when the export left some behind', () => {
    const doc = (extra: Record<string, unknown>) =>
      ({ ...exported({ archivePasswords: null }), ...extra }) as unknown as SettingsExportDoc;
    const marked = (extra: Record<string, unknown>) =>
      diffRows(doc(extra), {}, schema).find((r) => r.key === 'archivePasswords')?.secretless;
    expect(marked({ secrets: 'omitted', archivePasswordsOmitted: 0 })).toBe(false);
    expect(marked({ secrets: 'omitted', archivePasswordsOmitted: 2 })).toBe(true);
    expect(marked({ secrets: 'omitted' })).toBe(true);
    expect(marked({ secrets: 'included' })).toBe(false);
  });

  it('marks event programs whose command line stayed behind', () => {
    const program = (value: string) => ({ eventPrograms: [{ id: 'a1', command: { program: value } }] });
    expect(row(program('********'), {}, 'eventPrograms').secretless).toBe(true);
    expect(row(program('/usr/local/bin/file-it'), {}, 'eventPrograms').secretless).toBe(false);
  });
});
