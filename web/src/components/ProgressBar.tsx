// A progress track. Determinate fills with the accent; indeterminate loops a
// short segment - and ONLY while something is genuinely in flight.
//
// It is drawn at --radius-control, which is the token every badge and button on
// the page already uses, so it takes their corner and follows the shape setting
// all the way to square (jdp, 2026-09-06: "der fortschrittsbalken soll kräftiger
// sein und sich auch der eingestellten form anpassen", and again on 2026-09-07:
// "der download progressbar soll höher sein und sich der badge form anpassen" -
// the corner was already right, the height was not).
//
// h-5, up from h-4, h-2.5 and h-1.5 before those (jdp, 2026-09-07: "#2198 auf
// h-5 stellen"): it is the download list's own
// rightmost column and the one thing on the row somebody watches, so it is
// drawn at the weight of a control rather than of a hairline. Deliberately
// under the 32px of an IconBadge - it sits INSIDE a table row, and a bar as
// tall as a button would set the row height for every list.
export function ProgressBar({
  percent,
  active,
  indeterminate,
  moving = false,
  tone = 'accent',
}: {
  percent: number;
  active: boolean;
  /**
   * A moving bar with no number behind it. Never defaulted from `percent`:
   * "nothing downloaded yet" is the state of every row in a stopped queue, and
   * animating those was a whole list pretending to work while the queue was
   * halted (jdp, 2026-09-06, with a screen recording: "die fortschrittsbalken
   * die ganze zeit laden. auch wenn die downloadfunktion gestoppt ist"). The
   * caller says when something is actually running.
   */
  indeterminate?: boolean;
  /**
   * Whether bytes are actually moving RIGHT NOW, which is the only thing that
   * may breathe on this bar. Exactly the same question `indeterminate` above
   * exists to stop the bar answering on its own, asked for the other half of
   * the component: the determinate fill.
   *
   * FALSE BY DEFAULT, because most bars in this app are not transfers at all -
   * a quota on an account, the free space on a disk - and a caller that has no
   * opinion must not get a moving one.
   */
  moving?: boolean;
  tone?: 'accent' | 'ok';
}) {
  if (!active) return null;
  const isIndet = indeterminate === true;
  const clamped = Math.max(0, Math.min(100, percent));
  const fill = tone === 'ok' ? 'var(--status-ok-solid)' : 'var(--accent)';
  return (
    <div
      className="relative h-5 w-full overflow-hidden rounded-[var(--radius-control)] bg-carbon-surface3/70"
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={isIndet ? undefined : Math.round(clamped)}
    >
      {isIndet ? (
        <div
          className="absolute inset-y-0 w-1/3 rounded-[var(--radius-control)] opacity-70"
          style={{ background: fill, animation: 'glim-indeterminate 1.4s ease-in-out infinite' }}
        />
      ) : (
        <div
          className="kl-bar-fill relative h-full rounded-[var(--radius-control)] transition-[width] duration-500 ease-out"
          style={{ width: `${clamped}%`, background: fill }}
        >
          {/* THE FRONT EDGE, which was dead. A determinate fill is a flat block
              that grows, so a download moving at 40 KiB/s and one that stalled
              at the same percentage drew exactly the same picture - and the
              growth itself is far too slow to read as movement on anything
              larger than a few megabytes.

              TWO SEPARATE QUESTIONS, and running them together is how the fix
              for that became a second version of the same bug. The edge is
              DRAWN whenever there is a fill under it, because where the fill
              ends is a fact about the bar and stays true when nothing is
              happening. It BREATHES only while `moving`, because that is a
              claim about right now.

              Written as `clamped > 0` alone it claimed the second thing on the
              strength of the first. Measured on a live list of 101: 43 edges
              drawn in the window, every one of them `glim-pulse running 2s`,
              among them forty finished rows at 100% and a deliberately paused
              one at 70%. A file that had been done for an hour said it was
              moving, and paused against running still drew the same picture -
              which is the exact defect this edge was added to end.
              The other half of that reading is arithmetic: one infinite
              animation for the page (the live dot on the speed curve) had
              become one per drawn row.

              It breathes on glim-pulse, the app's own ambient loop, which is
              the same keyframe and the same period that live dot uses: "this is
              moving" is then said one way in this app rather than two. index.css
              carries the rule, its "off" stop and its reduced-motion stop.

              Only on a fill with a real width behind it. At 0% the sliver would
              sit on the left edge of an empty track pretending to be progress,
              which is the same lie `indeterminate` above exists to stop the bar
              telling. The guard used to stop there, and one pixel too early:
              measured at 1% on a 110px rail, the fill was 1.09px and the 3px
              edge sat on the whole of it and 1.91px past its left as well, so a
              download that had just started drew a white sliver instead of a
              coloured one. Where the fill is narrower than the edge, there is
              no edge to mark - index.css asks the fill itself whether it has
              room, which is the only place that number exists. */}
          {clamped > 0 && <span className={`kl-bar-edge${moving ? ' kl-bar-edge-live' : ''}`} />}
        </div>
      )}
    </div>
  );
}
