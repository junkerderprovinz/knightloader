import { useRef, useState, type ReactNode } from 'react';
import { useT } from '../lib/i18n';
import type { GlyphProps } from '../lib/glyphs';
import { LIFT, SETTLE, useReorder } from './dragLift';
import { PriorityFlow, nameBand } from './pictures/PriorityFlow';
import { RowGrip, moveTo } from './RowGrip';
import { Card, InfoBubble, SectionTitle } from './ui';

/**
 * PriorityLadder is a priority picture whose names are rows that set the
 * order: a row moves by drag, or with its grip focused by the arrow keys. The
 * download backends and the captcha accounts are ordered with it, so the two
 * cards look and work alike. `onReorder` stores the new order, and the rows
 * keep it drawn until that has finished, so a dropped row does not jump back
 * to its old place and then forward again.
 */
export function PriorityLadder({
  ids,
  title,
  labelFor,
  tipFor,
  glyph,
  busy = false,
  onReorder,
}: {
  ids: string[];
  /** The card's title, which names the picture and the list. */
  title: string;
  labelFor: (id: string) => string;
  /** What a row takes, for a row whose name does not say it. */
  tipFor?: (id: string) => string | undefined;
  glyph?: (p: GlyphProps) => ReactNode;
  busy?: boolean;
  onReorder: (next: string[]) => Promise<void> | void;
}) {
  const { t } = useT();
  const [pending, setPending] = useState<string[] | null>(null);
  const list = useRef<HTMLOListElement>(null);

  async function store(next: string[]) {
    setPending(next);
    try {
      await onReorder(next);
    } finally {
      setPending(null);
    }
  }

  const resting = pending ?? ids;
  // The whole row is pressed to move it, so a mouse arms the drag by moving
  // and a click on a row's (i) still opens its bubble.
  const drag = useReorder({
    ids: resting,
    container: list,
    attr: 'data-ladder-id',
    axis: 'y',
    arm: 'move',
    enabled: !busy,
    onReorder: (next) => void store(next),
  });

  return (
    // The stations follow the resting order, so a drag in flight moves the
    // names past them without starting the picture over at every step.
    <PriorityFlow backends={resting.map((id) => ({ id, name: labelFor(id) }))} label={title} glyph={glyph}>
      {/* The list is the rows' offsetParent, the layout a drag measures in.
          While a row is held the others wiggle (glim-drag-armed). */}
      <ol
        ref={list}
        aria-label={title}
        style={nameBand(resting.length)}
        className={`flex flex-col ${drag.held !== null ? 'glim-drag-armed' : ''}`}
      >
        {drag.order.map((id) => {
          const look = drag.look(id);
          const carried = look === LIFT || look === SETTLE;
          const tip = tipFor?.(id);
          return (
            <li
              key={id}
              data-ladder-id={id}
              // A carried row floats over the others, so it takes a ground of
              // its own. select-none keeps a drag from selecting the names.
              className={`flex min-h-0 flex-1 cursor-grab select-none items-center gap-1 rounded-[var(--radius-control)]
                pe-1 text-sm text-carbon-textSub active:cursor-grabbing ${carried ? 'bg-carbon-surface2' : ''} ${look}`}
              onPointerDown={(e) => drag.press(e, id)}
            >
              <RowGrip
                label={t('accounts.routing.dragHandle', { name: labelFor(id) })}
                disabled={busy}
                onMove={(by) => {
                  const next = moveTo(drag.order, id, by);
                  if (next) void store(next);
                }}
              />
              <span className="flex min-w-0 items-center">
                <span className="truncate text-carbon-text">{labelFor(id)}</span>
                {tip && <InfoBubble tip={tip} />}
              </span>
            </li>
          );
        })}
      </ol>
    </PriorityFlow>
  );
}

/**
 * PriorityCard is the frame both priority cards share: the title, a line on
 * what the picture is and how to use it, the ladder, and under it whatever
 * belongs to that order.
 */
export function PriorityCard({
  hue,
  title,
  hint,
  intro,
  'data-new': mark,
  children,
}: {
  hue: number;
  title: string;
  hint: string;
  intro: string;
  'data-new'?: string;
  children: ReactNode;
}) {
  return (
    <Card hue={hue} data-new={mark} className="flex flex-col gap-3">
      <SectionTitle hint={hint}>{title}</SectionTitle>
      <p className="text-sm text-carbon-textSub">{intro}</p>
      {children}
    </Card>
  );
}
