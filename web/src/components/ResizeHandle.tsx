import type { KeyboardEvent, PointerEvent } from 'react';
import { useTooltip } from './ui';

/**
 * ResizeHandle is the 8px grab strip on an edge of a header cell, which needs
 * `position: relative`. Its own component only so it can hold a hook: it
 * carries the house bubble rather than a native `title` (see columns.tsx's
 * Tip), and a hook cannot live inside a header's `map()`. The handle is
 * invisible furniture with no text of its own, which is the case GlimStone
 * says needs a tooltip unconditionally. The cell's row carries `group/header`,
 * which shows the line while the pointer is over the header.
 */
export function ResizeHandle({
  hint,
  onResize,
  onReset,
  at = 'end',
  step,
}: {
  hint: string;
  onResize: (phase: 'start' | 'move' | 'end', e: PointerEvent<HTMLElement>) => void;
  onReset: () => void;
  /** The edge of its cell the handle sits on. */
  at?: 'start' | 'end';
  /** Makes the handle a stop for the keyboard, where the arrow keys move it. */
  step?: { percent: number; onStep: (wider: boolean) => void };
}) {
  const tip = useTooltip<HTMLSpanElement>(hint);
  // role/tabIndex dropped: this element already states role="separator", and
  // the "note" role the hook hands out would replace it.
  const { role: _role, tabIndex: _tabIndex, ...hover } = tip.triggerProps;

  function onKeyDown(e: KeyboardEvent<HTMLElement>) {
    if (!step || (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight')) return;
    e.preventDefault();
    const towardEnd = (e.key === 'ArrowRight') !== (document.documentElement.dir === 'rtl');
    step.onStep(towardEnd === (at === 'end'));
  }

  return (
    <>
      <span
        role="separator"
        aria-orientation="vertical"
        aria-label={hint}
        {...hover}
        {...(step && {
          tabIndex: 0,
          'aria-valuenow': Math.round(step.percent),
          'aria-valuemin': 0,
          'aria-valuemax': 100,
          onKeyDown,
        })}
        onPointerDown={(e) => onResize('start', e)}
        onPointerMove={(e) => onResize('move', e)}
        onPointerUp={(e) => onResize('end', e)}
        // A drag the browser takes away (a context menu, a window switch)
        // must still settle the width, or the table keeps a hand-painted
        // track list that the next render silently undoes.
        onPointerCancel={(e) => onResize('end', e)}
        onDoubleClick={onReset}
        className={`absolute inset-y-0 z-10 w-2 cursor-col-resize touch-none ${at === 'end' ? 'end-0' : 'start-0'}`}
      >
        {/* The visible line, drawn inside the 8px grab area rather than as a
            border on the cell: a border would sit at the edge of the column,
            and the thing to aim at is the handle. Transparent until the header
            is hovered, and always ignoring the pointer so it never eats the
            drag it advertises. */}
        <span
          aria-hidden
          className={`pointer-events-none absolute inset-y-1 w-px bg-transparent transition-colors
            group-hover/header:bg-carbon-border ${at === 'end' ? 'end-[3px]' : 'start-[3px]'}`}
        />
      </span>
      {tip.node}
    </>
  );
}
