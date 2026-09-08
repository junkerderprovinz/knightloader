import { useEffect, useState, type ReactNode } from 'react';
import { InfoBubble } from '../ui';
import { copyToClipboard } from '../../lib/clipboard';
import { useT } from '../../lib/i18n';
import { useToast } from '../../lib/toast';

/**
 * Fact is the one labelled-value row every card in this panel draws.
 *
 * One definition, because five cards drawing the same shape by hand is five
 * chances for the label to sit beside the value in one card and above it in
 * another. The structure is columns.tsx's own TooltipField, on purpose: the
 * row tooltip and this panel answer the same questions about the same task,
 * and a reader who has hovered a row should recognise the panel instantly.
 * What is added here is the (i) and the copy badge, which a tooltip cannot
 * carry because a tooltip closes the moment the pointer leaves it.
 *
 * AN EMPTY VALUE RENDERS NOTHING AT ALL, and that is the rule that keeps the
 * cards honest. Most of these fields are absent on most tasks: a collected
 * link has no finish time, a direct HTTP download has no source page, a task
 * nothing has routed yet has no connection. Rows of dashes read as a panel
 * that failed to load; an absent field simply costs no line.
 */
export function Fact({
  label,
  hint,
  value,
  children,
  ltr,
  copy,
}: {
  label: string;
  /** The explanation, behind the (i). Never printed under the value: a panel
   *  whose every row carries two lines of grey prose is a panel nobody reads
   *  twice, and the sentence is still one hover away. */
  hint?: string;
  /** The plain text form of the value. Empty, absent, or the empty string
   *  means there is nothing to draw and the whole row disappears. */
  value?: string;
  /** Structured content instead of plain text: a badge, a row of chips, the
   *  backend badge. Takes precedence over `value` when both are given. */
  children?: ReactNode;
  /**
   * Marks the value as left-to-right regardless of the interface language.
   * A URL, a host name and a file path are LTR strings in every language, and
   * in Arabic, Hebrew or Persian an unmarked one is reordered around its
   * punctuation until it is no longer the address anybody typed.
   */
  ltr?: boolean;
  /** Offers a copy badge beside the value. Only meaningful with `value`, since
   *  what a chip row would copy is anybody's guess. */
  copy?: boolean;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [copied, setCopied] = useState(false);

  // The flash is two seconds and then gone. Cleared through the effect rather
  // than from inside the click handler so a row unmounted mid-flash, which is
  // the ordinary case here because the panel closes on the next click
  // somewhere else, does not leave a timer holding a dead setState.
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 2000);
    return () => window.clearTimeout(timer);
  }, [copied]);

  const text = value ?? '';
  if (!children && !text) return null;

  return (
    <div className="min-w-0">
      <div className="glim-eyebrow flex items-center gap-1">
        {label}
        {hint && <InfoBubble tip={hint} label={label} />}
      </div>
      <div className="flex min-w-0 items-start gap-2">
        <div dir={ltr ? 'ltr' : undefined} className="min-w-0 flex-1 break-words text-carbon-text">
          {children ?? text}
        </div>
        {copy && text && (
          <button
            type="button"
            className="shrink-0 rounded-[var(--radius-control)] bg-carbon-surface2 px-2 py-0.5 text-[11px]
              font-medium text-carbon-textSub transition duration-150 hover:brightness-110
              motion-safe:active:scale-[.98]"
            onClick={() => {
              // copyToClipboard, never navigator.clipboard directly: this app's
              // commonest deployment is a plain-http LAN address, where the
              // modern API does not exist at all and only the execCommand path
              // in lib/clipboard.ts actually copies anything.
              void copyToClipboard(text).then((ok) => {
                if (ok) setCopied(true);
                else toast(t('detail.copyFailed'), 'fail');
              });
            }}
          >
            {copied ? t('detail.copied') : t('detail.copy')}
          </button>
        )}
      </div>
    </div>
  );
}
