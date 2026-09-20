import { useEffect, useState } from 'react';
import { clearSkipped, connectWS, fetchSkipped, type SkippedLink } from '../lib/api';
import { fmtDate } from '../lib/format';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Button, InfoBubble } from './ui';
import { IconClose, IconTrash } from '../lib/icons';

// `at` has nanosecond precision, so the pair is unique in practice.
const keyOf = (s: SkippedLink) => `${s.at}|${s.url}`;

/**
 * SkippedLinks lists links that were refused with a reason, such as a pasted
 * duplicate folded into one already staged, so they do not simply vanish. It
 * is a floating card rather than a toast because it has controls and must not
 * time out.
 */
export function SkippedLinks() {
  const { t } = useT();
  const { toast } = useToast();
  const [items, setItems] = useState<SkippedLink[]>([]);
  const [showAll, setShowAll] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    let alive = true;

    // A socket of its own, since a skip that shows only after a reload is too late.
    const close = connectWS(
      (type, data) => {
        if (type !== 'skipped') return;
        setItems((prev) => [...prev, data as SkippedLink]);
        // Dismissing means "read", not "never again".
        setDismissed(false);
      },
      ['skipped'],
    );

    fetchSkipped()
      .then((history) => {
        if (!alive) return;
        setItems((live) => {
          // The socket opens before the snapshot, so an entry can arrive twice.
          const seen = new Set(history.map(keyOf));
          return [...history, ...live.filter((s) => !seen.has(keyOf(s)))];
        });
      })
      .catch(() => {
        // Not worth an error card; live events still fill the list.
      });

    return () => {
      alive = false;
      close();
    };
  }, []);

  async function onClear() {
    // Not optimistic: entries the server kept would reappear on reload.
    const done = await clearSkipped()
      .then((r) => r.ok)
      .catch(() => false);
    if (!done) {
      toast(t('skipped.clearFailed'), 'fail');
      return;
    }
    setItems([]);
    setShowAll(false);
  }

  if (!items.length || dismissed) return null;

  const newest = [...items].reverse();
  const shown = showAll ? newest : newest.slice(0, 1);

  return (
    // Bottom right with the other notices, above the toast stack at bottom-5.
    <div className="fixed bottom-20 right-5 z-40 w-[min(92vw,26rem)]">
      <div
        className="glim-toast overflow-hidden rounded-[var(--radius-control)] bg-carbon-surface2
          shadow-[var(--elevation)] ring-1 ring-carbon-border"
      >
        <div className="flex flex-wrap items-center gap-2 px-4 py-2.5">
          <span className="glim-num flex items-center text-xs text-carbon-textSub">
            {t('skipped.summary', { n: items.length })}
            <InfoBubble tip={t('skipped.info')} />
          </span>
          <span className="flex-1" />
          {/* Filled buttons, since ghost buttons read as text on a filled panel. */}
          {items.length > 1 && (
            <Button kind="secondary" className="px-2.5 text-xs" onClick={() => setShowAll((v) => !v)}>
              {showAll ? t('common.hide') : t('common.show')}
            </Button>
          )}
          <Button
            kind="secondary"
            className="px-2.5 text-xs"
            icon={<IconTrash width={14} height={14} />}
            onClick={onClear}
          >
            {t('skipped.clear')}
          </Button>
          <Button
            kind="secondary"
            icon={<IconClose width={14} height={14} />}
            aria-label={t('common.dismiss')}
            onClick={() => setDismissed(true)}
          />
        </div>

        <div className="max-h-56 overflow-y-auto pb-1.5">
          {shown.map((s, i) => (
            <div key={`${keyOf(s)}|${i}`} className="flex items-baseline gap-3 px-4 py-1 text-xs">
              <span className="max-w-[45%] shrink-0 truncate text-carbon-textSub" title={s.reason}>
                {s.reason}
              </span>
              <span dir="ltr" className="min-w-0 flex-1 truncate text-carbon-textMuted" title={s.url}>
                {s.url}
              </span>
              <span className="glim-num shrink-0 text-carbon-textMuted">{fmtDate(s.at)}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
