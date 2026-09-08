import { useT } from '../lib/i18n';
import { useInstanceScope } from '../lib/instance';
import { fits, fmtSpace, folderName, spaceHint, useDiskSpace, worst } from '../lib/useDiskSpace';
import { InfoBubble } from './ui';

/**
 * One line in the shell bar: how much room is left where the queue is pointed,
 * and how much it still wants to write there.
 *
 * ONE ROW, chosen upstairs. Which folder that is - the tightest one the queue
 * is actually aiming at, or the download folder when nothing is owed - is
 * `worst()` in lib/useDiskSpace.ts, so the bar and the Overview tile cannot
 * quietly pick differently and read as two disagreeing numbers on one screen.
 *
 * LOCAL ONLY, AND GUARDED TWICE. GET /api/diskspace is not forwarded to a peer
 * and is not on the relay allowlist, so this describes THIS machine's disks
 * whatever list is on screen; drawn over somebody else's downloads it would be
 * an answer about the wrong box under the right name. ShellStrip mounts it
 * inside its own `local &&` branch, and it checks again here - the widening
 * that would put it back on screen for a peer is one careless edit in a file
 * that knows nothing about this route, and the same pair of controls has been
 * through that argument once already (QueueBar's speed limit). The fix is
 * never to widen the allowlist instead: what a sibling may reach is
 * deliberately narrow.
 *
 * Nothing is drawn where the platform cannot be asked. There is no honest
 * one-line version of "this system has no call to ask with, so nothing is
 * being held back anywhere" - that needs a sentence, and the tile has one.
 */
export function DiskSpaceStrip() {
  const { t } = useT();
  const { instance } = useInstanceScope();
  const local = instance === '';
  const report = useDiskSpace(local);
  const v = worst(report);
  if (!local || !v) return null;
  const room = fits(v);

  return (
    // A group with a name, not a live region: this figure changes on its own
    // every few seconds, and announced each time it would talk over whatever
    // the reader is actually doing. The overview strip beside it settled the
    // same question the same way.
    <span
      role="group"
      aria-label={t('disk.stripLabel')}
      className="flex min-w-0 flex-wrap items-baseline gap-x-1.5 gap-y-0.5 text-[11px] text-carbon-textMuted"
    >
      <span dir="auto" title={v.dir} className="min-w-0 max-w-full truncate text-carbon-textSub">
        {folderName(v.dir)}
      </span>

      <span className="flex items-baseline gap-1">
        <span>{t('disk.free')}</span>
        <span className="glim-num text-carbon-text">{fmtSpace(v.free)}</span>
      </span>

      {/* Only when something is actually owed here. The tile keeps its zero so
          its row cannot reflow; a bar that is one line high has nothing to
          steady, and "still to write 0 B" in it is a word about nothing. */}
      {v.queued > 0 && (
        <span className="flex items-baseline gap-1">
          <span>{t('disk.queued')}</span>
          <span className={`glim-num ${room === false ? 'text-statusFail' : 'text-carbon-text'}`}>
            {fmtSpace(v.queued)}
          </span>
        </span>
      )}

      {/* In words as well as in colour: the ink alone says something is wrong
          without saying what, and it says nothing at all to a reader who cannot
          tell the two apart. */}
      {room === false && <span className="text-statusFail">{t('disk.tight')}</span>}

      {/* The figures above describe a folder nobody asked about, so the bar
          says which one before it says anything else about it. A mount that did
          not come up leaves the reading on a completely different disk, and a
          line that stayed silent about that would be the most confident wrong
          number in the app. */}
      {v.measured !== v.dir && (
        <span dir="auto" className="flex min-w-0 max-w-full items-baseline">
          <span className="truncate">{t('disk.measured', { path: v.measured })}</span>
          <InfoBubble
            tip={v.exists ? t('disk.elsewhereHint') : t('disk.missingHint', { path: v.measured })}
            label={t('disk.measured', { path: v.measured })}
          />
        </span>
      )}

      <InfoBubble tip={spaceHint(t, report)} label={t('disk.stripLabel')} />
    </span>
  );
}
