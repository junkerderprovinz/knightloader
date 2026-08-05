// A thin accent progress bar. Determinate tracks percent; indeterminate loops a
// small segment (before a size is known). Modelled on BombVault's ProgressBar.
export function ProgressBar({
  percent,
  active,
  indeterminate,
}: {
  percent: number;
  active: boolean;
  indeterminate?: boolean;
}) {
  if (!active) return null;
  const isIndet = indeterminate ?? percent <= 0;
  const clamped = Math.max(0, Math.min(100, percent));
  return (
    <div
      className="relative h-1 w-full overflow-hidden rounded-full"
      style={{ background: 'var(--carbon-border)' }}
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={isIndet ? undefined : Math.round(clamped)}
    >
      {isIndet ? (
        <div
          className="absolute inset-y-0 w-1/3 rounded-full"
          style={{ background: 'var(--accent)', animation: 'kl-indeterminate 1.2s ease-in-out infinite' }}
        />
      ) : (
        <div
          className="h-full transition-[width] duration-300 ease-out"
          style={{ width: `${clamped}%`, background: 'var(--accent)' }}
        />
      )}
    </div>
  );
}
