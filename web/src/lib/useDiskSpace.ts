import { useEffect, useState } from 'react';
import { type DiskReport, type DiskVolume, fetchDiskSpace } from './api';
import { useT, type TranslationKey } from './i18n';
import { en } from './locales/en';

// The disk report's poll and the rules for reading it, used by
// components/DiskSpaceTile.tsx. Nothing takes a base: the route describes this
// machine's disks and a peer answers 403, so callers gate on the scope.

// Just outside the server's cache, and long enough that a statfs on a hung
// network mount is not repeated while the last one still waits. A poll rather
// than a hub subscription, since free space changes without any task event.
const POLL_MS = 10000;

type Translate = ReturnType<typeof useT>['t'];

/**
 * useDiskSpace keeps the latest report. `active` is false while the scope is
 * a peer, so polling stops without skipping the hook. A failed poll keeps the
 * last good report and raises no toast.
 */
export function useDiskSpace(active = true): DiskReport | null {
  const [report, setReport] = useState<DiskReport | null>(null);

  useEffect(() => {
    if (!active) return;
    let alive = true;
    const load = () =>
      void fetchDiskSpace()
        .then((r) => {
          if (alive) setReport(r);
        })
        .catch(() => {
          /* the last reading stands */
        });
    load();
    const iv = setInterval(load, POLL_MS);
    return () => {
      alive = false;
      clearInterval(iv);
    };
  }, [active]);

  return report;
}

/**
 * fits reports whether what the queue owes a folder fits in its free space,
 * or null when the platform cannot measure it; draw the warning on false
 * only. `queued` is not subtracted from `free`, because the engine reserves a
 * download's full length before the first byte.
 */
export function fits(v: DiskVolume): boolean | null {
  if (!v.known) return null;
  return v.queued <= v.free;
}

/** The row for the folder ordinary downloads land in, whatever it can report. */
export function downloadsVolume(report: DiskReport | null): DiskVolume | null {
  return report?.volumes.find((v) => v.role === 'downloads') ?? null;
}

/**
 * worst picks the one row worth a single line: the tightest folder the queue
 * is aiming at, or the download folder when nothing is owed. Rows that cannot
 * be measured are skipped.
 */
export function worst(report: DiskReport | null): DiskVolume | null {
  if (!report) return null;
  let pick: DiskVolume | null = null;
  for (const v of report.volumes) {
    if (!v.known || v.queued <= 0) continue;
    if (!pick || v.free < pick.free) pick = v;
  }
  if (pick) return pick;
  const home = downloadsVolume(report);
  return home && home.known ? home : null;
}

/**
 * folderName is the last segment of a path, on either separator since the
 * server may run on Windows. A bare root ("/", "C:\") stays whole. Show the
 * full path as a title beside it.
 */
export function folderName(dir: string): string {
  const trimmed = dir.replace(/[/\\]+$/, '');
  if (!trimmed) return dir;
  const cut = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'));
  return cut >= 0 ? trimmed.slice(cut + 1) || trimmed : trimmed;
}

/**
 * roleLabel translates why a folder is listed. A role this build has no word
 * for keeps its raw id rather than a blank. It checks `en`, the dictionary
 * that is always loaded.
 */
export function roleLabel(t: Translate, role: string): string {
  const key = `disk.role.${role}` as TranslationKey;
  return key in en ? t(key) : role;
}

/**
 * spaceHint is the explanatory sentence, with the age of the reading: it is
 * cached for a few seconds and the disk guard checks every fifteen, so it can
 * briefly disagree with a row that waits for space.
 */
export function spaceHint(t: Translate, report: DiskReport | null): string {
  if (!report) return t('disk.hint');
  const taken = Date.parse(report.sampledAt);
  if (Number.isNaN(taken)) return t('disk.hint');
  const secs = Math.max(0, Math.round((Date.now() - taken) / 1000));
  return `${t('disk.hint')} ${t('disk.sampled', { n: secs })}`;
}
