import { DiskVolumeRow } from '../../../components/DiskSpaceTile';
import { Card, Field, NumberInput, SectionTitle } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { downloadsVolume, spaceHint, useDiskSpace } from '../../../lib/useDiskSpace';
import { useDraft } from '../context';

/**
 * The three disk floors that decide whether a download may start, and whether a
 * running one is put back in the queue.
 *
 * They are one card and not three rows in the limits card because they are read
 * against one thing the rest of that page never touches: free space on the
 * volume the download folder sits on. Everything else up there is a count.
 *
 * THE READOUT ABOVE THE BOXES IS THE ROUTE'S, NOT THIS CARD'S. A paragraph
 * stood here saying no endpoint served the free bytes of the download folder,
 * and it ended "Do not invent a readout here; invent the route first." The
 * route was invented: GET /api/diskspace reports free, used and total per
 * target folder from the same call this guard is checked against, plus what
 * the queue still owes each one. So the row is a real reading, and the three
 * numbers below are unchanged - still floors on FREE bytes, still zero-means-
 * off, still saved settings rather than anything the row derives.
 *
 * WHAT THE ROW STILL CANNOT DO IS GREY THESE BOXES OUT. A platform this build
 * has no call for answers `known: false`, and the row then says so in words
 * instead of printing zeros; the floors stay editable anyway, because they
 * travel with the instance and the next machine to read them may well be one
 * that can answer. A disabled control here would be this card claiming a
 * setting is pointless on the strength of one machine's kernel.
 *
 * It is also a reading a few seconds old, not a gauge, and it is one FOLDER's
 * volume rather than the machine's - see DiskSpaceTile.tsx, which owns the row
 * and the reasons. No `base` anywhere: settings are this machine's, and so are
 * the disks the row describes.
 *
 * NO MASTER TOGGLE. 0 is the off state of each of the three, the way historyMax,
 * keepFinishedDays and speedLimit already work on this page. A switch above them
 * would be a control with no field behind it on the wire.
 */

// GiB in the boxes, bytes on the wire. The repo reads byte counts in binary
// units everywhere (lib/format.ts, and the Go side's own tests measure in
// 1 << 30), so a "GB" spinner would disagree with the numbers the app prints
// two pages away.
const GIB = 1024 ** 3;

// Same idiom as speedLimit in DownloadsSettings.tsx, which divides by 1024 for
// the box and multiplies back on write: round on the way OUT, never on the way
// in, and refuse to send a negative. The rounding is display only. It lives
// here and not in an effect on purpose, because rewriting the draft on mount
// would turn opening this page into an edit: a raw byte figure someone typed on
// the Advanced page reads here as its nearest two decimals and stays untouched
// on the wire until somebody actually moves the spinner.
const toGiB = (b: number) => Math.round((b / GIB) * 100) / 100;
// Math.max(0, ...) mirrors clampDiskBytes, which reads any negative as 0,
// meaning off. The server never refuses the save, so a negative would come back
// as a silently different number in the box; simpler not to send one.
const fromGiB = (v: number) => Math.max(0, Math.round(v * GIB));

// The server's own typo guard, mirrored rather than picked here: maxDiskBytes is
// 1 << 50, which is this many GiB. Anything above it is clamped down on save
// without a word, so a spinner that went higher would lie about what saving did.
const MAX_GIB = 1024 * 1024;

// Half a GiB, the granularity these floors are actually set at. It is also the
// shipped value of the reserve, so the first press of the stepper from the
// default lands on a round number rather than on 1.5 minus a rounding error.
const STEP_GIB = 0.5;

export function DiskSpaceCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  // The floor the "do not start" box may not step below. sanitizeDiskSpace
  // raises diskLowSpace to diskCriticalSpace whenever the stop floor is the
  // higher of the two, and never the other way round. That is not tidiness: with
  // the stop floor above the start floor, the watcher stops a transfer and the
  // dispatcher starts it again on the next tick, once every fifteen seconds,
  // throwing away the bytes of every non-resumable one each time round.
  const lowFloorGiB = toGiB(cfg.diskCriticalSpace);

  // The download folder only. The other folders on the report - categories, the
  // working folder, a path set on one download - belong on the Overview tile,
  // which is about where things are landing; this card is about three numbers,
  // and the one volume worth putting beside them is the one they are read
  // against by default.
  const report = useDiskSpace();
  const here = downloadsVolume(report);

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.downloads.diskHint')}>{t('settings.downloads.diskTitle')}</SectionTitle>

      {/* The DRAFT is handed to the row, not the saved document, so the two
          marks on the track move with the spinners as they are turned. A mark
          that only jumped once the auto-save came back would make a floor look
          like it had not taken.

          The hint is passed here and not on the Overview tile: there the card
          title carries it once for every row, and this card's own title is
          already explaining three settings that are not this reading. */}
      {here && (
        <div className="glim-well p-0">
          <DiskVolumeRow v={here} cfg={cfg} hint={spaceHint(t, report)} />
        </div>
      )}

      {/* The reserve is per file and per dispatch pass: what is free, minus what
          this pass has already promised to other downloads, has to cover this
          download's REMAINING bytes plus this number. So a resumed transfer is
          measured on what is left of it, not on its full size, and a task whose
          size the host has not stated yet skips the arithmetic entirely while
          still being subject to the start floor below. Both facts are in the
          hint, and both have to stay true if anyone edits it. */}
      <Field label={t('settings.downloads.diskReserve')} hint={t('settings.downloads.diskReserveHint')}>
        <NumberInput
          value={toGiB(cfg.diskReserve)}
          min={0}
          max={MAX_GIB}
          step={STEP_GIB}
          onValue={(v) => patch({ diskReserve: fromGiB(v) })}
        />
      </Field>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {/* min is the stop floor, not 0. Stepping below it would offer a value
            the very next save repairs, and the repair is visible: settings
            auto-save shortly after an edit and the server's sanitized document
            REPLACES the draft, so this box jumps up on its own a moment after
            the stop floor is set above it. That is correct and the hint says so.
            Do not pre-empt it here, and do not clamp the stop floor down to this
            one instead: that is the repair the Go side deliberately refuses to
            make, because it would quietly lower a number somebody typed to
            protect a nearly full volume. */}
        <Field label={t('settings.downloads.diskLowSpace')} hint={t('settings.downloads.diskLowSpaceHint')}>
          <NumberInput
            value={toGiB(cfg.diskLowSpace)}
            min={lowFloorGiB}
            max={MAX_GIB}
            step={STEP_GIB}
            onValue={(v) => patch({ diskLowSpace: fromGiB(v) })}
          />
        </Field>

        {/* The only one of the three the fifteen-second watcher reads, and the
            expensive one: a held-back transfer goes back to the wait queue and
            resumes on its own, but one that cannot resume loses the bytes it had
            already fetched. Its own value always survives a save unchanged
            unless it hit a clamp, since the interlock rewrites the other field. */}
        <Field label={t('settings.downloads.diskCriticalSpace')} hint={t('settings.downloads.diskCriticalSpaceHint')}>
          <NumberInput
            value={toGiB(cfg.diskCriticalSpace)}
            min={0}
            max={MAX_GIB}
            step={STEP_GIB}
            onValue={(v) => patch({ diskCriticalSpace: fromGiB(v) })}
          />
        </Field>
      </div>
    </Card>
  );
}
