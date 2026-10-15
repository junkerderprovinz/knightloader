import { useId, useState, type ReactNode } from 'react';
import { HintBubble, LabelBadge } from '../../../components/ui';
import { IconExpand } from '../../../lib/icons';
import type { TranslationKey } from '../../../lib/i18n';

export type FoldVerdict = 'ok' | 'warn' | 'fail';

/** The word a fold's badge says for each verdict, as the start report's rows say it. */
export const VERDICT_WORDS: Record<FoldVerdict, TranslationKey> = {
  ok: 'settings.diagnostics.verdict.ok',
  warn: 'settings.diagnostics.verdict.warn',
  fail: 'settings.diagnostics.verdict.fail',
};

/**
 * Fold is a row of the Health card that opens on what lies behind it, with
 * its verdict at the row's end so a closed row still says whether to look. It
 * opens by itself on trouble until somebody opens or closes it.
 */
export function Fold({
  label,
  hint,
  verdict,
  verdictWord,
  open: wanted,
  children,
}: {
  label: string;
  hint?: string;
  verdict?: FoldVerdict;
  verdictWord?: string;
  /** Whether the row stands open before anybody touches it. */
  open: boolean;
  children: ReactNode;
}) {
  const [chosen, setChosen] = useState<boolean | null>(null);
  const open = chosen ?? wanted;
  const body = useId();

  return (
    <div className="flex flex-col border-t border-carbon-border pt-3">
      {/* The button's ::after spans the row, so the whole row opens it, and
          the (i) sits above that layer, so asking what it is opens nothing. */}
      <div
        className="relative -mx-2 flex items-center gap-x-4 rounded-[var(--radius-control)] px-2 py-1.5
          transition-colors hover:bg-carbon-hover"
      >
        <span className="flex min-w-0 items-center">
          <button
            type="button"
            aria-expanded={open}
            aria-controls={body}
            onClick={() => setChosen(!open)}
            className="flex min-w-0 items-center gap-2 text-start after:absolute after:inset-0
              after:rounded-[var(--radius-control)] focus-visible:outline-none
              focus-visible:after:ring-2 focus-visible:after:ring-[var(--focus-ring)]"
          >
            <IconExpand
              width={16}
              height={16}
              className={`shrink-0 text-carbon-textMuted transition-transform ${open ? '' : '-rotate-90 rtl:rotate-90'}`}
            />
            <span data-glim-label={label} className="min-w-0 text-sm text-carbon-text">
              {label}
            </span>
          </button>
          <span className="relative z-10 flex">
            <HintBubble hint={hint} />
          </span>
        </span>
        {verdict && verdictWord && (
          <span className="ms-auto">
            <LabelBadge tone={verdict} label={verdictWord} />
          </span>
        )}
      </div>
      {open && (
        <div id={body} className="flex flex-col gap-4 pt-3 ps-6">
          {children}
        </div>
      )}
    </div>
  );
}
