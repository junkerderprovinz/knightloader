import { useEffect, useState } from 'react';
import { clearSkipped, connectWS, fetchSkipped, type SkippedLink } from '../lib/api';
import { fmtDate } from '../lib/format';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Button, InfoBubble } from './ui';
import { IconClose, IconTrash } from '../lib/icons';

// keyOf identifies one entry. The pair is unique in practice: `at` is a Go
// timestamp with nanosecond precision, so two entries share it only if the same
// URL was folded twice inside the same nanosecond.
const keyOf = (s: SkippedLink) => `${s.at}|${s.url}`;

/**
 * SkippedLinks is the trace of links that were refused with a reason.
 *
 * It exists because the alternative is the failure this project set out not to
 * repeat: a pasted duplicate is folded into the copy already staged, and from
 * the outside the link simply vanishes. That looks exactly like a broken paste
 * box, and it gets reported as one. The strip says how many, why, and which.
 *
 * Quiet by design — one line plus the newest entry — because the common case is
 * one duplicate in a paste of forty and it must not compete with the list.
 */
export function SkippedLinks() {
  const { t } = useT();
  const { toast } = useToast();
  const [items, setItems] = useState<SkippedLink[]>([]);
  const [showAll, setShowAll] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    let alive = true;

    // A second socket rather than a poll: the server broadcasts the moment it
    // folds a link, and a link that only appears here after a reload is a link
    // the user has already given up looking for. connectWS is the only
    // subscription this app has — a shared multiplexer belongs in lib/, which
    // another agent owns.
    const close = connectWS((type, data) => {
      if (type !== 'skipped') return;
      setItems((prev) => [...prev, data as SkippedLink]);
      // A new refusal un-dismisses the strip. Dismissing means "I have read
      // these", not "never tell me again", and staying hidden would put the app
      // straight back to swallowing links silently.
      setDismissed(false);
    });

    fetchSkipped()
      .then((history) => {
        if (!alive) return;
        setItems((live) => {
          // The socket is open before the snapshot is taken, so an entry can
          // arrive down both paths. Dropping the duplicate is cheaper than
          // ordering the two requests against each other.
          const seen = new Set(history.map(keyOf));
          return [...history, ...live.filter((s) => !seen.has(keyOf(s)))];
        });
      })
      .catch(() => {
        // The trace is not worth an error card: it is a footnote to a list that
        // renders fine without it, and the live events still fill it in.
      });

    return () => {
      alive = false;
      close();
    };
  }, []);

  async function onClear() {
    // The trace lives on the server, so the local copy is emptied only once the
    // server has actually forgotten it. Clearing optimistically would make the
    // entries reappear on the next reload, which reads as the app resurrecting
    // links the user just dismissed.
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

  // Newest first: the entry that explains what just happened to the paste the
  // user is still looking at is the last one the server appended.
  const newest = [...items].reverse();
  const shown = showAll ? newest : newest.slice(0, 1);

  return (
    <div className="glim-well overflow-hidden">
      <div className="flex flex-wrap items-center gap-2 px-4 py-2">
        <span className="glim-num flex items-center text-xs text-carbon-textSub">
          {t('skipped.summary', { n: items.length })}
          <InfoBubble tip={t('skipped.info')} />
        </span>
        <span className="flex-1" />
        {items.length > 1 && (
          <Button kind="ghost" className="px-2.5 text-xs" onClick={() => setShowAll((v) => !v)}>
            {showAll ? t('common.hide') : t('common.show')}
          </Button>
        )}
        <Button
          kind="ghost"
          className="px-2.5 text-xs"
          icon={<IconTrash width={14} height={14} />}
          onClick={onClear}
        >
          {t('skipped.clear')}
        </Button>
        <Button
          kind="ghost"
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
            {/* dir=ltr: a URL is not prose and must not be reordered when the
                interface language is right-to-left. */}
            <span dir="ltr" className="min-w-0 flex-1 truncate text-carbon-textMuted" title={s.url}>
              {s.url}
            </span>
            <span className="glim-num shrink-0 text-carbon-textMuted">{fmtDate(s.at)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
