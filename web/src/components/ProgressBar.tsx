// A slim progress track. Determinate fills with the accent; indeterminate loops
// a short segment (used while a task is queued and has no size yet).
export function ProgressBar({
  percent,
  active,
  indeterminate,
  tone = 'accent',
}: {
  percent: number;
  active: boolean;
  indeterminate?: boolean;
  tone?: 'accent' | 'ok';
}) {
  if (!active) return null;
  const isIndet = indeterminate ?? percent <= 0;
  const clamped = Math.max(0, Math.min(100, percent));
  const fill = tone === 'ok' ? 'var(--status-ok-solid)' : 'var(--accent)';
  return (
    <div
      className="relative h-1.5 w-full overflow-hidden rounded-full bg-carbon-surface3/70"
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={isIndet ? undefined : Math.round(clamped)}
    >
      {isIndet ? (
        <div
          className="absolute inset-y-0 w-1/3 rounded-full opacity-70"
          style={{ background: fill, animation: 'glim-indeterminate 1.4s ease-in-out infinite' }}
        />
      ) : (
        <div
          className="h-full rounded-full transition-[width] duration-500 ease-out"
          style={{ width: `${clamped}%`, background: fill }}
        />
      )}
    </div>
  );
}
