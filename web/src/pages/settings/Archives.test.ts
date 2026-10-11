import { describe, expect, it } from 'vitest';

import { stationSettings } from './Archives';

const where = (state: Parameters<typeof stationSettings>[0]) => {
  const s = stationSettings(state);
  return Object.fromEntries(Object.entries(s).map(([step, to]) => [step, `${to.page}: ${to.label ?? to.title}`]));
};

describe('where the steps after a download are set', () => {
  it('leads verifying to the checksum switch and repairing to the Usenet servers', () => {
    const to = where({ unpacking: true, workDir: '', moveTo: '' });
    expect(to.verify).toBe('downloads: settings.module.checksums');
    expect(to.repair).toBe('accounts: accounts.usenet.title');
    expect(to.unpack).toBe('archives: settings.module.extraction');
  });

  it('leads moving to the place for unpacked files while archives are unpacked', () => {
    expect(where({ unpacking: true, workDir: '', moveTo: '' }).move).toBe('archives: settings.archives.moveTo');
    expect(where({ unpacking: true, workDir: '/work', moveTo: '/media' }).move).toBe('archives: settings.archives.moveTo');
  });

  it('leads moving to the working folder where that is what moves the files', () => {
    expect(where({ unpacking: true, workDir: '/work', moveTo: ' ' }).move).toBe('downloads: settings.downloads.workDir');
  });

  it('leads moving to the working folder while nothing is unpacked, since the other row is absent', () => {
    expect(where({ unpacking: false, workDir: '', moveTo: '/media' }).move).toBe('downloads: settings.downloads.workDir');
  });

  it('leads cleaning up to the archive\'s fate, and to the switch while nothing is unpacked', () => {
    expect(where({ unpacking: true, workDir: '', moveTo: '' }).cleanup).toBe('archives: settings.archives.disposal');
    expect(where({ unpacking: false, workDir: '', moveTo: '' }).cleanup).toBe('archives: settings.module.extraction');
  });
});
