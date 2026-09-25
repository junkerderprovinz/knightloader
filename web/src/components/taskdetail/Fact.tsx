import { useEffect, useState, type ReactNode } from 'react';
import { InfoBubble } from '../ui';
import { copyToClipboard } from '../../lib/clipboard';
import { useT } from '../../lib/i18n';
import { useToast } from '../../lib/toast';

/**
 * Fact is the labelled-value row every card in the detail panel draws, shaped
 * like columns.tsx's TooltipField. An empty value renders nothing, because most
 * fields are absent on most tasks and rows of dashes read as a failed load.
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
  /** Shown behind the (i), never printed under the value. */
  hint?: string;
  value?: string;
  /** Structured content such as a badge; takes precedence over `value`. */
  children?: ReactNode;
  /** Keeps a URL, host or path left-to-right in an RTL interface language. */
  ltr?: boolean;
  /** Offers a copy badge beside the value; only meaningful with `value`. */
  copy?: boolean;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [copied, setCopied] = useState(false);

  // Cleared through the effect so a row unmounted mid-flash, which is common
  // because the panel closes on the next outside click, leaves no timer behind.
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
            className="shrink-0 rounded-[var(--radius-pill)] bg-carbon-surface2 px-2 py-0.5 text-[11px]
              font-medium text-carbon-textSub transition duration-150 hover:brightness-110
              motion-safe:active:scale-[.98]"
            onClick={() => {
              // navigator.clipboard does not exist on a plain-http LAN address,
              // where only lib/clipboard.ts's execCommand fallback copies.
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
