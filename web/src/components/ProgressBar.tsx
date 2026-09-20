// A progress track at --radius-control, so it follows the shape setting like
// badges and buttons. At h-5 it stays below an IconBadge's 32px, which would
// otherwise set every row's height.
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
   * A looping bar with no number. The caller sets it only while something is
   * running, since a stopped queue has many rows at zero.
   */
  indeterminate?: boolean;
  /**
   * Whether bytes are moving now, which makes the fill's edge pulse. Off by
   * default because most bars, such as quotas and disk space, are not transfers.
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
          {/* The front edge tells a moving download from a stalled one at the
              same percentage. It is drawn wherever there is a fill and pulses
              on glim-pulse only while `moving`. index.css hides it when the
              fill is narrower than the edge. */}
          {clamped > 0 && <span className={`kl-bar-edge${moving ? ' kl-bar-edge-live' : ''}`} />}
        </div>
      )}
    </div>
  );
}
