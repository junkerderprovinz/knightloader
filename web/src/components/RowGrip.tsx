import type { KeyboardEvent, PointerEvent } from 'react';
import { IconGrip } from '../lib/icons';

/**
 * RowGrip is the grip of a row that can be dragged into another order, and a
 * real button, so the arrow keys move the row while it has the focus. It
 * carries an aria-label but no tooltip, which would follow the pointer down
 * every row. While the order is being stored it stays focusable and ignores
 * the keys, so the keyboard can carry on once the store has finished.
 */
export function RowGrip({
  label,
  busy,
  onMove,
  onPointerDown,
}: {
  label: string;
  busy: boolean;
  /** A step up (-1) or down (1), from the arrow keys. */
  onMove: (by: -1 | 1) => void;
  /** Where only the grip starts a drag, the drag's press. */
  onPointerDown?: (e: PointerEvent<HTMLButtonElement>) => void;
}) {
  function onKeyDown(e: KeyboardEvent<HTMLButtonElement>) {
    if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
    e.preventDefault();
    if (busy) return;
    onMove(e.key === 'ArrowUp' ? -1 : 1);
  }
  return (
    <button
      type="button"
      aria-disabled={busy || undefined}
      aria-label={label}
      className="shrink-0 cursor-grab touch-none rounded-[var(--radius-pill)] px-1 py-0.5 text-carbon-textMuted
        outline-none transition-colors hover:text-carbon-text focus-visible:shadow-[0_0_0_2px_var(--focus-ring)]
        active:cursor-grabbing aria-disabled:cursor-default"
      onKeyDown={onKeyDown}
      onPointerDown={onPointerDown}
    >
      <IconGrip width={14} height={16} />
    </button>
  );
}

/** moveTo returns `ids` with `id` moved `by` places, or null where it cannot go. */
export function moveTo(ids: readonly string[], id: string, by: number): string[] | null {
  const from = ids.indexOf(id);
  const to = from + by;
  if (from < 0 || to < 0 || to >= ids.length) return null;
  const next = [...ids];
  next.splice(from, 1);
  next.splice(to, 0, id);
  return next;
}
