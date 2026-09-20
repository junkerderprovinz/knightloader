// The header's right-click menu for choosing columns. The keyboard's menu key
// raises the same contextmenu event.

import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useT } from '../lib/i18n';
import { IconCheck } from '../lib/icons';
import { InfoBubble } from './ui';
import type { ColumnDef, ColumnId } from './columns';

const MARGIN = 8;

// Decorative only: the menu row is the button.
function Mark({ on }: { on: boolean }) {
  return (
    <span
      aria-hidden
      className={`grid shrink-0 place-items-center rounded-[var(--radius-control)] transition-colors ${
        on ? 'bg-accent text-accentContrast' : 'bg-carbon-surface3/60 text-transparent'
      }`}
      style={{ height: '1.125rem', width: '1.125rem' }}
    >
      <IconCheck width={12} height={12} />
    </span>
  );
}

export function ColumnMenu({
  at,
  columns,
  hidden,
  onToggle,
  onReset,
  onClose,
}: {
  /** Where the pointer was, in viewport coordinates. */
  at: { x: number; y: number };
  /** Every column this build has, in display order. */
  columns: ColumnDef[];
  hidden: Set<ColumnId>;
  onToggle: (id: ColumnId) => void;
  onReset: () => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number }>({ top: at.y, left: at.x });
  const visibleCount = columns.length - hidden.size;

  // Clamped after mount so a menu opened near an edge stays on screen.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    setPos({
      top: Math.max(MARGIN, Math.min(at.y, window.innerHeight - r.height - MARGIN)),
      left: Math.max(MARGIN, Math.min(at.x, window.innerWidth - r.width - MARGIN)),
    });
  }, [at.x, at.y]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) onClose();
    };
    // The measured position goes stale on scroll or resize.
    window.addEventListener('keydown', onKey);
    window.addEventListener('mousedown', onDown, true);
    window.addEventListener('scroll', onClose, true);
    window.addEventListener('resize', onClose);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('mousedown', onDown, true);
      window.removeEventListener('scroll', onClose, true);
      window.removeEventListener('resize', onClose);
    };
  }, [onClose]);

  return createPortal(
    <div
      ref={ref}
      role="menu"
      aria-label={t('columns.menuTitle')}
      style={{ top: pos.top, left: pos.left }}
      className="glim-card glim-fade fixed z-50 min-w-[15rem] max-w-[20rem] p-1.5"
    >
      <div className="flex items-center px-2 py-1.5 text-[11px] font-semibold text-carbon-textSub">
        {t('columns.menuTitle')}
        <InfoBubble tip={t('columns.headerHint')} />
      </div>

      {columns.map((c) => {
        // The last visible column stays, or nothing is left to right-click.
        const shown = !hidden.has(c.id);
        const locked = !c.hideable;
        const last = shown && visibleCount <= 1;
        const disabled = locked || last;
        return (
          // The InfoBubble sits beside the button, not nested inside it.
          <div key={c.id} className="flex items-center">
            <button
              role="menuitemcheckbox"
              aria-checked={shown}
              disabled={disabled}
              onClick={() => onToggle(c.id)}
              className={`flex min-w-0 flex-1 items-center gap-2.5 rounded-[var(--radius-control)] px-2 py-1.5 text-start text-[12px]
                transition-colors ${
                  disabled
                    ? 'cursor-not-allowed text-carbon-textMuted'
                    : 'text-carbon-text hover:bg-carbon-hover'
                }`}
            >
              <Mark on={shown} />
              <span className="truncate">{t(c.labelKey)}</span>
            </button>
            {disabled && <InfoBubble tip={locked ? t('columns.alwaysShown') : t('columns.lastVisible')} className="mr-1.5" />}
          </div>
        );
      })}

      <div className="my-1 h-px bg-carbon-border/60" />
      <button
        role="menuitem"
        onClick={onReset}
        className="flex w-full items-center rounded-[var(--radius-control)] px-2 py-1.5 text-start text-[12px]
          text-carbon-textSub transition-colors hover:bg-carbon-hover hover:text-carbon-text"
      >
        {t('columns.reset')}
      </button>
    </div>,
    document.body,
  );
}
