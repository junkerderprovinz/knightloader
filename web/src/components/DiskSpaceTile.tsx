import { Link } from 'react-router-dom';
import type { DiskVolume, Settings } from '../lib/api';
import { useT } from '../lib/i18n';
import { fits, fmtSpace, folderName, roleLabel, spaceHint, useDiskSpace } from '../lib/useDiskSpace';
import { ProgressBar } from './ProgressBar';
import { Card, InfoBubble, SectionTitle } from './ui';

/**
 * How much room is left where the downloads are going, one row per FOLDER.
 *
 * Per folder and not per disk, and that is not a shortcut: nothing in this
 * build can tell whether two folders sit on the same volume - the platform
 * calls behind this report hand back no volume identity at all - so the
 * perfectly ordinary install with the download folder and the working folder
 * on one disk shows the same gigabytes twice. Hence no total across the rows,
 * no donut of the whole machine, and the warning written into the title's own
 * bubble rather than left for the reader to work out afterwards.
 *
 * Three figures, never derived from each other. Free excludes the slice a
 * filesystem keeps back for root and honours a per-user quota, so free plus
 * used is routinely LESS than the volume's size; a bar built to make them add
 * up would paint that reserve as somebody's files.
 */

// A chip is drawn here rather than with LabelBadge because these sit inside a
// row of text, not in a header: the shared badge is a 32px control-height
// object and three of them would set the height of every row in the list.
// The neutral ground is surface3 and not surface2 for the reason ProgressBar's
// own track already is - a well IS surface2, so a chip painted in it would
// have no edge at all.
function Chip({ label, hint, tone }: { label: string; hint?: string; tone: 'neutral' | 'warn' | 'fail' }) {
  const ground =
    tone === 'fail'
      ? 'bg-statusFailBg text-statusFail'
      : tone === 'warn'
        ? 'bg-statusWarnBg text-statusWarn'
        : 'bg-carbon-surface3 text-carbon-textSub';
  return (
    <span
      dir="auto"
      className={`inline-flex min-w-0 shrink items-center rounded-[var(--radius-control)] px-2 py-0.5 text-[11px] ${ground}`}
    >
      <span className="truncate">{label}</span>
      {hint && <InfoBubble tip={hint} label={label} />}
    </span>
  );
}

/**
 * The two floors, marked on the track where they fall.
 *
 * Both are FREE-byte floors, so each one lands at the point the fill would have
 * to reach for that much room to be gone. Drawn only above zero: both ship at
 * 0, and 0 there is the server holding no opinion at all rather than a limit
 * that happens to sit at the left edge - a line drawn for it would be this card
 * inventing a rule nobody set. Nothing is drawn or labelled in its place
 * either, for the same reason.
 */
function Marks({ v, cfg }: { v: DiskVolume; cfg: Settings | null }) {
  const { t } = useT();
  if (!cfg || v.total <= 0) return null;
  const marks: { at: number; label: string; colour: string }[] = [];
  const add = (bytes: number, label: string, colour: string) => {
    if (bytes <= 0 || bytes >= v.total) return;
    marks.push({ at: ((v.total - bytes) / v.total) * 100, label, colour });
  };
  add(cfg.diskLowSpace, t('disk.markLow'), 'bg-statusWarnSolid');
  add(cfg.diskCriticalSpace, t('disk.markStop'), 'bg-statusFailSolid');
  return (
    <>
      {marks.map((m) => (
        // insetInlineStart, not left: the fill underneath grows from the
        // inline start too, so in a right-to-left language both move together
        // instead of the mark drifting to the wrong end of its own bar.
        <span
          key={m.label}
          role="img"
          aria-label={m.label}
          title={m.label}
          style={{ insetInlineStart: `${m.at}%` }}
          className={`absolute inset-y-0 w-[2px] ${m.colour}`}
        />
      ))}
    </>
  );
}

/**
 * One folder.
 *
 * Exported because the Downloads settings card shows this same row for the
 * download folder beside the three floors it is compared against, and a second
 * renderer there would be the same numbers with a second set of rules about
 * when to hide them.
 *
 * `hint` is for a caller that has no card title to hang the general
 * explanation on. The tile below does have one, so it passes nothing rather
 * than repeating the same paragraph once per row.
 */
export function DiskVolumeRow({ v, cfg, hint }: { v: DiskVolume; cfg: Settings | null; hint?: string }) {
  const { t } = useT();
  const room = fits(v);
  const usedPct = v.total > 0 ? Math.min(100, Math.round((v.used / v.total) * 100)) : 0;

  return (
    <div className="flex flex-col gap-2 px-5 py-3">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
        {/* dir="auto" and the whole path in the title: the name is cut down to
            its last segment to fit, and a path can be written in a right-to-
            left script, which the shell bar's peer name already allows for. */}
        <span dir="auto" title={v.dir} className="min-w-0 truncate text-[13.5px] text-carbon-text">
          {folderName(v.dir)}
        </span>
        <span className="flex shrink-0 items-center text-[11px] text-carbon-textMuted">
          {roleLabel(t, v.role)}
          {hint && <InfoBubble tip={hint} label={t('disk.title')} />}
        </span>
        <span className="flex-1" />

        {/* The folder is not there yet, which is ordinary: whoever writes the
            first file into it creates it. */}
        {!v.exists && <Chip tone="neutral" label={t('disk.missing')} />}

        {/* WHENEVER THE MEASURED FOLDER IS NOT THE ONE ASKED ABOUT, THE PATH IS
            SHOWN - not hinted at, shown. The report climbs to the nearest
            existing folder above, which for a folder nobody has written into
            yet is harmless and on the right disk; but if a mount did not come
            up, the climb reaches the volume root and the figures then describe
            a completely different disk with total confidence. That is the one
            way this row can be a confident wrong number, so the difference gets
            its own chip in warning ink and the sentence that says what it can
            mean. */}
        {v.measured !== v.dir && (
          <Chip
            tone={v.exists ? 'warn' : 'neutral'}
            label={t('disk.measured', { path: v.measured })}
            hint={v.exists ? t('disk.elsewhereHint') : t('disk.missingHint', { path: v.measured })}
          />
        )}

        {/* On `false` alone. `fits` answers null where nothing could be
            measured, and a warning drawn for that would be a claim about a
            volume nobody read. */}
        {room === false && <Chip tone="fail" label={t('disk.tight')} hint={t('disk.tightHint')} />}
      </div>

      {v.known ? (
        <>
          {/* No bar without a size to draw it against. A track filled to 0%
              would read as an empty disk, which is the opposite of "there is
              no figure here". */}
          {v.total > 0 && (
            <div className="relative">
              <ProgressBar percent={usedPct} active />
              <Marks v={v} cfg={cfg} />
            </div>
          )}
          <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-[11px] text-carbon-textMuted">
            <span className="flex items-baseline gap-1.5">
              <span>{t('disk.free')}</span>
              <span className="glim-num text-carbon-text">{fmtSpace(v.free)}</span>
              {/* strip.of, the word the queue header already pairs two byte
                  figures with. A second "of" would be one sentence translated
                  twice in forty-two files. */}
              <span>{t('strip.of')}</span>
              <span className="glim-num text-carbon-textSub" title={t('disk.size')}>
                {fmtSpace(v.total)}
              </span>
            </span>
            <span className="flex items-baseline gap-1.5">
              <span>{t('disk.used')}</span>
              <span className="glim-num text-carbon-textSub">{fmtSpace(v.used)}</span>
            </span>
            {/* Beside the free figure and never taken off it: a download that
                is already running had its room claimed on the disk when it
                started, so those bytes are missing from `free` already. The
                bubble is where that gets said, along with the fact that a
                download whose size nobody stated counts as nothing here, which
                makes this a floor rather than a promise.

                Shown at zero as well. An idle folder reading "0 B" is an
                answer; a figure that vanishes when there is nothing owed is a
                row that reflows every time the queue empties, and Counters.tsx
                keeps its zeros visible for exactly that reason. */}
            <span className="flex items-baseline gap-1.5">
              <span>{t('disk.queued')}</span>
              <span className="glim-num text-carbon-text">{fmtSpace(v.queued)}</span>
              <span>{t('disk.tasks', { n: v.tasks })}</span>
              <InfoBubble tip={t('disk.queuedHint')} />
            </span>
          </div>
        </>
      ) : (
        // NO BAR AND NO FIGURES. This platform has no call to ask with, so
        // free, used and total are zeros that mean nothing - and a bar filled
        // to 0% would be this row inventing an empty disk. What is worth saying
        // is that nothing is being held back here at all, which is a different
        // situation from a disk that is genuinely full and needs a different
        // reaction, so it gets said in words.
        <span className="flex items-center text-[11px] text-carbon-textMuted">
          {t('disk.unknown')}
          <InfoBubble tip={t('disk.unknownHint')} />
        </span>
      )}
    </div>
  );
}

/**
 * The Overview card.
 *
 * `settings` is handed down rather than fetched: the page already has the
 * document, and the two floors marked on each track have to be the ones the
 * rest of that page is reading.
 *
 * Nothing at all until a report has arrived, and nothing if it has no folders
 * to describe - the same way StatusStrip draws nothing while nothing is
 * happening. A card standing empty on the busiest page in the app is furniture
 * somebody has to read past on the way to the figures that mean something.
 */
export function DiskSpaceTile({ settings }: { settings: Settings | null }) {
  const { t } = useT();
  const report = useDiskSpace();
  const volumes = report?.volumes ?? [];
  if (volumes.length === 0) return null;

  return (
    // Card and not a bare div: SectionTitle's badge is absolutely positioned
    // against the nearest positioned ancestor, and only .glim-card is one. The
    // two cards above this on the page carry the same note, having been the
    // place the bug was found twice.
    <Card className="flex flex-col gap-3">
      <SectionTitle hint={spaceHint(t, report)}>{t('disk.title')}</SectionTitle>
      <div className="glim-well divide-y divide-carbon-border/60 p-0">
        {volumes.map((v, i) => (
          // The path alone is not a key: one folder can be on the list under
          // two roles, and the server's order is what the rows are read in.
          <DiskVolumeRow key={`${v.role}|${v.dir}|${i}`} v={v} cfg={settings} />
        ))}
      </div>
      {/* The server left folders out, and says so rather than presenting a
          short list as the whole list. Only the tail of destinations sitting
          outside every configured folder can be cut, and only the ones owed
          least - so this is never the reason a folder somebody configured is
          missing, which is the thing a reader would otherwise suspect. */}
      {report?.truncated && (
        <span className="flex items-center text-[11px] text-carbon-textMuted">
          {t('disk.truncated')}
          <InfoBubble tip={t('disk.truncatedHint')} />
        </span>
      )}

      {/* The way to the numbers that actually hold a download back. Quiet, at
          the end: this card reports, it does not configure. */}
      <Link
        to="/settings/downloads"
        className="self-start text-[11px] text-carbon-textMuted underline-offset-2 hover:text-carbon-text hover:underline"
      >
        {t('disk.limits')}
      </Link>
    </Card>
  );
}
