import { useEffect, useState } from 'react';
import { type DiskReport, type DiskVolume, fetchDiskSpace } from './api';
import { fmtBytes } from './format';
import { useT, type TranslationKey } from './i18n';
import { en } from './locales/en';

/**
 * The disk report's one poll, and every rule the two surfaces read it through.
 *
 * The tile on the Overview page and the line in the shell bar describe the same
 * folders from the same reading, and they are on screen at the same time. A
 * second fetch, or a second "which row matters" rule, is two answers to one
 * question with nothing to reconcile them - the same reasoning useQueueControl
 * (components/QueueBar.tsx) was lifted out of its bar for.
 *
 * NOTHING HERE TAKES A `base`. GET /api/diskspace is not on the federation
 * forwarder's list and not on the relay allowlist, so a peer answers 403 - and
 * that refusal is the point rather than an oversight: this route describes the
 * disks of THIS machine, and a row drawn under a peer's name would be a
 * confident answer to a question about a different box. Whatever is mounted
 * beside a peer's list gates itself on the scope instead (see DiskSpaceStrip).
 */

// Just outside the server's own few-second cache, so a tab is rarely handed the
// same sample twice, and far enough apart that a statfs on a network mount that
// has stopped answering is not being asked again while the last one still
// hangs. Deliberately a poll and NOT a hub subscription: the hub broadcasts on
// task and queue events, and free space moves while nothing at all is
// happening - which is exactly why the disk guard is a watcher on a timer and
// not a branch in the dispatcher.
const POLL_MS = 10000;

type Translate = ReturnType<typeof useT>['t'];

/**
 * useDiskSpace keeps the latest report for as long as the caller wants one.
 *
 * `active` is how a caller says the scope is still local. A component cannot
 * skip a hook, so the switch lives in here rather than in an early return
 * upstairs, and a strip that has scrolled out of its own scope stops asking
 * rather than quietly polling this machine while somebody else's list is up.
 *
 * A failed poll keeps the last good report instead of blanking it. QueueBar
 * settled that one already: a readout that disappears on one dropped request
 * is a readout people stop looking at. No toast either - nobody asked for this
 * number, so failing to refresh it is not an event.
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
          /* the last reading stands; see the doc comment above */
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
 * Whether what the queue still owes a folder actually fits in what is free.
 *
 * THREE ANSWERS, and null is the one that earns the type. A volume this
 * platform cannot be asked about carries zeros that mean nothing, and reading
 * those as "0 owed fits in 0 free" would print a reassurance nobody measured.
 * Callers draw the warning on `false` alone, never on "not true".
 *
 * The comparison is deliberately just that - a comparison. `queued` is never
 * taken off `free`: this build's engine claims a download's full length on the
 * disk before the first byte lands, so a running transfer's room is already
 * missing from the free figure, and subtracting it a second time would report
 * a healthy volume as overcommitted.
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
 * The one row worth a single line in the shell bar.
 *
 * The tightest folder the queue is actually aiming at, because that is the one
 * a person would want to have known about before it ran out; with nothing owed
 * anywhere, the download folder, since that is where the next paste is going.
 *
 * Rows this platform cannot measure are out of both halves. There is nothing
 * to be worst about, and one line in a bar is no place to explain why a whole
 * platform has no answer - the tile says that properly, in a sentence.
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
 * A byte count as room on a disk.
 *
 * fmtBytes answers an em dash for zero, which is right in a table cell and
 * wrong in every reading here: a volume with nothing left is the one state
 * this whole feature exists to make visible, and printing it as the character
 * that means "no data" hands it to the reader as a fault instead of as an
 * answer. Counters.tsx hit the same wall and wrote the same line.
 */
export const fmtSpace = (n: number): string => (n > 0 ? fmtBytes(n) : '0 B');

/**
 * The last segment of a path, for the one place a whole path will not fit.
 *
 * Both separators, because a Windows instance reports Windows paths and this
 * runs in whatever browser is pointed at it. The full path always travels
 * beside this as a title, and a path that is nothing but a root ("/", "C:\")
 * stays whole rather than shortening to nothing.
 */
export function folderName(dir: string): string {
  const trimmed = dir.replace(/[/\\]+$/, '');
  if (!trimmed) return dir;
  const cut = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'));
  return cut >= 0 ? trimmed.slice(cut + 1) || trimmed : trimmed;
}

/**
 * Why a folder is on the list, in the reader's own language.
 *
 * The server sends a stable id and never the word, for the reason Feature.ID
 * carries in its own doc comment: it cannot know which of the shipped locales
 * this browser is showing, and two clients of one instance routinely differ.
 * A role a newer server has learnt before this build has a word for it keeps
 * its raw id, which is at least something a person can search for; a blank
 * would read as a broken row. Tested against `en`, the one dictionary that is
 * always loaded, because every locale is typed as the same total Record.
 */
export function roleLabel(t: Translate, role: string): string {
  const key = `disk.role.${role}` as TranslationKey;
  return key in en ? t(key) : role;
}

/**
 * The sentence both surfaces explain themselves with.
 *
 * It carries the age of the reading rather than animating the figures, because
 * the reading is stale by design: the server caches it for a few seconds and
 * the guard's own watcher runs on a fifteen-second tick. Between a download
 * being put back for want of room and the next refresh here, this can show a
 * volume comfortably clear while the row underneath says it is waiting for
 * disk space, and a gauge that ticked like a live one would be claiming the
 * contradiction is impossible.
 */
export function spaceHint(t: Translate, report: DiskReport | null): string {
  if (!report) return t('disk.hint');
  const taken = Date.parse(report.sampledAt);
  if (Number.isNaN(taken)) return t('disk.hint');
  const secs = Math.max(0, Math.round((Date.now() - taken) / 1000));
  return `${t('disk.hint')} ${t('disk.sampled', { n: secs })}`;
}
