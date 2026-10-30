import { useCallback, useState } from 'react';
import type { Task } from '../lib/api';
import { fmtDate } from '../lib/format';
import { rejectionReason } from '../lib/rejectionReason';
import { useT, type TranslationKey } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { useToast } from '../lib/toast';
import { Button } from './ui';
import { Notice, NoticeRow } from './Notice';
import { IconEmpty, IconEye, IconEyeOff, IconRestore } from '../lib/icons';

/**
 * FilteredLinks is the holding area for links a filter rule refused, torrents
 * that announce a banned tracker and links the download history already has,
 * kept out of the collector list so a working filter does not look like junk.
 * Restore puts a link back past what held it. There is no accent, since a held
 * link is not activity.
 *
 * `held` comes from the page's task stream; the component opens no socket.
 */
export function FilteredLinks({ held }: { held: Task[] }) {
  const { t } = useT();
  const { toast } = useToast();
  const [showAll, setShowAll] = useState(false);
  const [busy, setBusy] = useState(false);

  // Not optimistic: the server broadcasts the changed tasks.
  const act = useCallback(
    async (run: () => Promise<Response>, failKey: TranslationKey) => {
      setBusy(true);
      try {
        const resp = await run().catch(() => null);
        if (!resp?.ok) toast(t(failKey), 'fail');
      } finally {
        setBusy(false);
      }
    },
    [t, toast],
  );

  const restore = (ids: string[]) => act(() => restoreFiltered(ids), 'collector.filtered.restoreFailed');
  const clear = (ids: string[]) => act(() => clearFiltered(ids), 'collector.filtered.clearFailed');

  if (!held.length) return null;

  const newest = [...held].reverse();
  const shown = showAll ? newest : newest.slice(0, 1);

  return (
    <Notice
      id="filtered"
      title={t('collector.filtered.heldSummary', { n: held.length })}
      info={t('collector.filtered.heldInfo')}
      actions={
        <>
          {held.length > 1 && (
            <Button kind="secondary" icon={showAll ? <IconEyeOff /> : <IconEye />} onClick={() => setShowAll((v) => !v)}>
              {showAll ? t('common.hide') : t('common.show')}
            </Button>
          )}
          <Button kind="secondary" icon={<IconEmpty />} disabled={busy} onClick={() => clear(held.map((h) => h.id))}>
            {t('collector.filtered.clear')}
          </Button>
          <Button kind="secondary" icon={<IconRestore />} disabled={busy} onClick={() => restore(held.map((h) => h.id))}>
            {t('collector.filtered.restoreAll')}
          </Button>
        </>
      }
    >
      {shown.map((h) => (
        <NoticeRow
          key={h.id}
          reason={`${heldBy(t, h)}: ${rejectionReason(t, h.skipCode, h.skipParams, h.skipReason)}`}
          name={h.name && h.name !== h.url ? h.name : h.url}
          source={originLabel(t, h.origin)}
          at={fmtDate(h.createdAt)}
          action={
            <Button
              kind="secondary"
              icon={<IconRestore />}
              className="px-2.5 text-xs"
              disabled={busy}
              onClick={() => restore([h.id])}
            >
              {t('collector.filtered.restore')}
            </Button>
          }
        />
      ))}
    </Notice>
  );
}

// heldBy names what caught the link: the download history, or the filter rule.
// The engine records one rule today, but the field is a list. Every filter
// verdict names its rule, an unnamed one by its position, so a hold that names
// none comes from the banned trackers on the Torrents page.
function heldBy(t: ReturnType<typeof useT>['t'], h: Task): string {
  if (h.skipCode === 'downloaded') return t('collector.filtered.byHistory');
  return (h.matchedRules ?? []).join(', ') || t('settings.torrents.bannedTrackers');
}

const restoreFiltered = (ids: string[]) =>
  fetch('/api/collector/filtered/restore', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids }),
  });

// The ids go in the query because not every proxy forwards a DELETE body. An
// empty list means all of them.
const clearFiltered = (ids: string[]) =>
  fetch(`/api/collector/filtered?ids=${encodeURIComponent(ids.join(','))}`, { method: 'DELETE' });

// originLabel falls back to the raw origin for one the catalogue does not
// know, since the server's set grows.
function originLabel(t: ReturnType<typeof useT>['t'], origin?: string): string {
  if (!origin) return '';
  const key = `collector.filtered.origin.${origin}` as TranslationKey;
  return key in en ? t(key) : origin;
}
