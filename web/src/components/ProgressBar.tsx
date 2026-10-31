/**
 * SloshPops are the bubbles that pop past the end of a fill level drawn as a
 * bar, inside the element that carries glim-slosh-edge. index.css times them
 * by their place, one rule for each of the two.
 */
export function SloshPops() {
  return (
    <>
      <span className="glim-slosh-pop-bar" />
      <span className="glim-slosh-pop-bar" />
    </>
  );
}

// A progress track at --radius-pill, so it follows the shape setting like
// badges and buttons. At h-5 it stays below an IconBadge, 24px in a list row,
// so the badges and not the bar set the row's height.
export function ProgressBar({
  percent,
  active,
  indeterminate,
  moving = false,
  tone = 'accent',
  striped = false,
  level = false,
  label,
}: {
  percent: number;
  /**
   * Centred in the bar, such as the percentage. It is drawn twice, on the
   * track and in the fill's ink clipped to the fill, so it reads on both as
   * the fill passes under it.
   */
  label?: string;
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
  /**
   * A fill level, such as a quota, rather than a transfer: the fill's end is a
   * liquid's surface that sloshes, and it has no front edge to pulse.
   */
  level?: boolean;
}) {
  if (!active) return null;
  const isIndet = indeterminate === true;
  const clamped = Math.max(0, Math.min(100, percent));
  const fill =
    tone === 'ok' ? 'var(--status-ok-solid)' : tone === 'fail' ? 'var(--status-fail-solid)' : 'var(--accent)';
  const stripes = striped ? ' kl-bar-stripes' : '';
  const ink = tone === 'accent' ? 'text-accentContrast' : 'text-carbon-background';
  // The fill grows from the start edge, the right one in a right-to-left page.
  const rest = `${100 - clamped}%`;
  const clip = document.documentElement.dir === 'rtl' ? `inset(0 0 0 ${rest})` : `inset(0 ${rest} 0 0)`;
  const text = 'glim-num pointer-events-none absolute inset-0 flex items-center justify-center text-meta font-medium';
  return (
    <div
      className="relative h-5 w-full overflow-hidden rounded-[var(--radius-pill)] bg-carbon-surface3/70 glim-meter"
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
          className={`kl-bar-fill relative h-full transition-[width] duration-500 ease-out${stripes} ${
            level ? (clamped > 0 ? 'glim-slosh-edge' : '') : 'rounded-[var(--radius-pill)]'
          }`}
          style={{ width: `${clamped}%`, backgroundColor: fill }}
        >
          {/* The front edge tells a moving download from a stalled one at the
              same percentage. It is drawn wherever there is a fill and pulses
              on glim-pulse only while `moving`. index.css hides it when the
              fill is narrower than the edge. */}
          {clamped > 0 && !level && <span className={`kl-bar-edge${moving ? ' kl-bar-edge-live' : ''}`} />}
          {clamped > 0 && level && <SloshPops />}
        </div>
      )}
      {label !== undefined && !isIndet && (
        <>
          <span aria-hidden className={`${text} text-carbon-text`}>
            {label}
          </span>
          <span
            aria-hidden
            className={`${text} ${ink} transition-[clip-path] duration-500 ease-out`}
            style={{ clipPath: clip }}
          >
            {label}
          </span>
        </>
      )}
    </div>
  );
}
