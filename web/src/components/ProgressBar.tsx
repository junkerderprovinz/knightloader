// A progress track at --radius-pill, so it follows the shape setting like
// badges and buttons. At h-5 it stays below an IconBadge's 32px, which would
// otherwise set every row's height.
export function ProgressBar({
  percent,
  active,
  indeterminate,
  moving = false,
  tone = 'accent',
  striped = false,
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
  tone?: 'accent' | 'ok' | 'fail';
  /**
   * Diagonal stripes over the fill, for an archive being unpacked. The tone
   * stays the row's, so the texture is what tells it from a download.
   */
  striped?: boolean;
}) {
  if (!active) return null;
  const isIndet = indeterminate === true;
  const clamped = Math.max(0, Math.min(100, percent));
  const fill =
    tone === 'ok' ? 'var(--status-ok-solid)' : tone === 'fail' ? 'var(--status-fail-solid)' : 'var(--accent)';
  const stripes = striped ? ' kl-bar-stripes' : '';
  return (
    <div
      className="relative h-5 w-full overflow-hidden rounded-[var(--radius-pill)] bg-carbon-surface3/70"
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={isIndet ? undefined : Math.round(clamped)}
    >
      {isIndet ? (
        <div
          className={`absolute inset-y-0 w-1/3 rounded-[var(--radius-pill)] opacity-70${stripes}`}
          style={{ backgroundColor: fill, animation: 'glim-indeterminate 1.4s ease-in-out infinite' }}
        />
      ) : (
        <div
          className={`kl-bar-fill relative h-full rounded-[var(--radius-pill)] transition-[width] duration-500 ease-out${stripes}`}
          style={{ width: `${clamped}%`, backgroundColor: fill }}
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
