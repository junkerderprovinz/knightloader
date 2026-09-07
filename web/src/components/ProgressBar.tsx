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
          className="h-full rounded-[var(--radius-control)] transition-[width] duration-500 ease-out"
          style={{ width: `${clamped}%`, background: fill }}
        />
      )}
    </div>
  );
}
