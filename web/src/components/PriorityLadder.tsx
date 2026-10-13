import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { useT } from '../lib/i18n';
import type { GlyphProps } from '../lib/glyphs';
import { IconWarning } from '../lib/icons';
import { LIFT, SETTLE, useReorder } from './dragLift';
import { PriorityFlow, entryBand, nameBand } from './pictures/PriorityFlow';
import { RowGrip, moveTo } from './RowGrip';
import { Card, InfoBubble, SectionTitle, useTooltip } from './ui';

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
  entry,
  labelFor,
  tipFor,
  troubleFor,
  glyph,
  busy = false,
  onReorder,
}: {
  ids: string[];
  /** The card's title, which names the picture and the list. */
  title: string;
  /** The name of what comes in at the top, beside its glyph. */
  entry: string;
  labelFor: (id: string) => string;
  /** What a row takes, for a row whose name does not say it. */
  tipFor?: (id: string) => string | undefined;
  /** Why a row's account does not work, for a row that stays in the order all the same. */
  troubleFor?: (id: string) => string | undefined;
  glyph?: (p: GlyphProps) => ReactNode;
  busy?: boolean;
  onReorder: (next: string[]) => Promise<void> | void;
}) {
  const { t } = useT();
  const [pending, setPending] = useState<string[] | null>(null);
  const list = useRef<HTMLOListElement>(null);
  // The row last moved by the keyboard. Moving a row moves its element, and
  // the browser drops the focus of an element that moves.
  const stepped = useRef<string | null>(null);

  async function store(next: string[]) {
    setPending(next);
    try {
      await onReorder(next);
    } finally {
      setPending(null);
    }
  }

  useLayoutEffect(() => {
    const id = stepped.current;
    if (id === null) return;
    if (document.activeElement === document.body) {
      list.current?.querySelector<HTMLElement>(`[data-ladder-id="${id}"] button`)?.focus();
    }
    if (pending === null) stepped.current = null;
  });

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
      <p data-new="priority-start-names" dir="ltr" style={entryBand(resting.length)} className="flex items-center ps-1.5">
        <span dir="auto" className="line-clamp-2 break-words text-[13px] leading-[15px] text-carbon-text">
          {entry}
        </span>
      </p>
      {/* The list is the rows' offsetParent, the layout a drag measures in.
          While a row is held the others wiggle (glim-drag-armed). It does not
          mirror, as the picture does not, so a name stays beside its station. */}
      <ol
        ref={list}
        data-new="priority-names"
        dir="ltr"
        aria-label={title}
        style={nameBand(resting.length)}
        className={`flex flex-col ${drag.held !== null ? 'glim-drag-armed' : ''}`}
      >
        {drag.order.map((id) => {
          const look = drag.look(id);
          const carried = look === LIFT || look === SETTLE;
          const tip = tipFor?.(id);
          const trouble = troubleFor?.(id);
          return (
            <li
              key={id}
              data-ladder-id={id}
              // A carried row floats over the others, so it takes a ground of
              // its own. select-none keeps a drag from selecting the names.
              className={`flex min-h-0 flex-1 cursor-grab select-none items-center gap-1 rounded-[var(--radius-control)]
                ps-1.5 pe-1 text-carbon-textSub active:cursor-grabbing ${carried ? 'bg-carbon-surface2' : ''} ${look}`}
              onPointerDown={(e) => drag.press(e, id)}
            >
              {/* The name comes first, right beside its station, in the type
                  the other pictures name a station in. It takes a second line
                  rather than lose its end on a narrow card. */}
              <span className="flex min-w-0 flex-1 items-center">
                <span
                  dir="auto"
                  className={`line-clamp-2 break-words text-[13px] leading-[15px] text-carbon-text ${trouble ? 'opacity-55' : ''}`}
                >
                  {labelFor(id)}
                </span>
                {trouble && <Trouble reason={trouble} />}
                {tip && <InfoBubble tip={tip} />}
              </span>
              <RowGrip
                label={t('accounts.routing.dragHandle', { name: labelFor(id) })}
                busy={busy || pending !== null}
                onMove={(by) => {
                  const next = moveTo(drag.order, id, by);
                  if (!next) return;
                  stepped.current = id;
                  void store(next);
                }}
              />
            </li>
          );
        })}
      </ol>
    </PriorityFlow>
  );
}

/** Trouble is the warning beside a row whose account does not work, with the reason in its bubble. */
function Trouble({ reason }: { reason: string }) {
  const tip = useTooltip<HTMLSpanElement>(reason);
  return (
    <>
      <span {...tip.triggerProps} aria-label={reason} className="ms-1.5 inline-flex shrink-0 text-statusWarn">
        <IconWarning width={14} height={14} />
      </span>
      {tip.node}
    </>
  );
}

/**
 * PriorityCard is the frame both priority cards share: the title, a line on
 * what the picture is and how to use it, the ladder, and under it whatever
 * belongs to that order. `right` ends the title's line.
 */
export function PriorityCard({
  hue,
  title,
  hint,
  intro,
  right,
  'data-new': mark,
  children,
}: {
  hue: number;
  title: string;
  hint: string;
  intro: string;
  right?: ReactNode;
  'data-new'?: string;
  children: ReactNode;
}) {
  return (
    <Card hue={hue} data-new={mark} className="flex flex-col gap-3">
      <SectionTitle hint={hint} right={right}>
        {title}
      </SectionTitle>
      <p className="text-sm text-carbon-textSub">{intro}</p>
      {children}
    </Card>
  );
}
