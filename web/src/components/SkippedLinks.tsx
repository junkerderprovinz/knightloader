import { useEffect, useState } from 'react';
import { clearSkipped, connectWS, fetchSkipped, type SkippedLink } from '../lib/api';
import { fmtDate } from '../lib/format';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { skippedReason } from '../lib/skippedReason';
import { Button } from './ui';
import { Notice, NoticeRow } from './Notice';
import { IconEmpty, IconEye, IconEyeOff } from '../lib/icons';

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
    <div className="fixed bottom-[calc(5rem+var(--phone-bar-space)+var(--page-actions-space))] end-5 z-40 w-[min(92vw,34rem)]">
      <Notice
        id="skipped"
        className="glim-toast"
        title={t('skipped.summary', { n: items.length })}
        info={t('skipped.info')}
        onClose={() => setDismissed(true)}
        actions={
          <>
            {items.length > 1 && (
              <Button kind="secondary" icon={showAll ? <IconEyeOff /> : <IconEye />} onClick={() => setShowAll((v) => !v)}>
                {showAll ? t('common.hide') : t('common.show')}
              </Button>
            )}
            <Button kind="secondary" icon={<IconEmpty />} onClick={onClear}>
              {t('skipped.clear')}
            </Button>
          </>
        }
      >
        {shown.map((s, i) => (
          <NoticeRow key={`${keyOf(s)}|${i}`} reason={skippedReason(t, s)} name={s.url} at={fmtDate(s.at)} />
        ))}
      </Notice>
    </div>
  );
}
