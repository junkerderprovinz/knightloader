import { Card, Field, NumberInput, SectionTitle } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { useDraft } from '../context';

/**
 * The three disk floors that decide whether a download may start, and whether a
 * running one is put back in the queue.
 *
 * They are one card and not three rows in the limits card because they are read
 * against one thing the rest of that page never touches: free space on the
 * volume the download folder sits on. Everything else up there is a count.
 *
 * NO FREE-SPACE READOUT, AND THAT IS DELIBERATE. Nothing on the server can tell
 * this page how much room the volume has, or even whether this build can ask:
 * internal/diskspace answers (0, false) on any platform it has no call for, and
 * every caller then fails open, so the guard simply never holds anything back
 * there. That answer is unexported and reaches no route, and no endpoint serves
 * the free bytes of the download folder either. So the card cannot grey itself
 * out on such a system and cannot print "your volume has X free" beside the
 * boxes. The honest substitute is the sentence in the title's own bubble, which
 * says exactly that. Do not invent a readout here; invent the route first.
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

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.downloads.diskHint')}>{t('settings.downloads.diskTitle')}</SectionTitle>

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
