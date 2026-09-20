import { useCallback, useState } from 'react';
import type { Task } from '../lib/api';
import { fmtDate } from '../lib/format';
import { useT, type TranslationKey } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { useToast } from '../lib/toast';
import { Button, InfoBubble } from './ui';
import { Tip } from './columns';
import { IconRetry, IconTrash } from '../lib/icons';

/**
 * FilteredLinks is the holding area for links a filter rule refused, kept out
 * of the collector list so a working filter does not look like junk. Restore
 * puts a link back with its rule waived. There is no accent, since a held link
 * is not activity.
 *
 * `held` comes from the page's task stream; the component opens no socket.
 */
export function FilteredLinks({ held }: { held: Task[] }) {
  const { t } = useT();
  const fx = useFx();
  const { toast } = useToast();
  const [showAll, setShowAll] = useState(false);
  const [busy, setBusy] = useState(false);

  // Not optimistic: the server broadcasts the changed tasks.
  const act = useCallback(
    async (run: () => Promise<Response>, failKey: FilteredKey) => {
      setBusy(true);
      try {
        const resp = await run().catch(() => null);
        if (!resp?.ok) toast(fx(failKey), 'fail');
      } finally {
        setBusy(false);
      }
    },
    [fx, toast],
  );

  const restore = (ids: string[]) => act(() => restoreFiltered(ids), 'collector.filtered.restoreFailed');
  const clear = (ids: string[]) => act(() => clearFiltered(ids), 'collector.filtered.clearFailed');

  if (!held.length) return null;

  const newest = [...held].reverse();
  const shown = showAll ? newest : newest.slice(0, 1);

  return (
    <div className="glim-well overflow-hidden">
      <div className="flex flex-wrap items-center gap-2 px-4 py-2">
        <span className="glim-num flex items-center text-xs text-carbon-textSub">
          {fx('collector.filtered.summary', { n: held.length })}
          <InfoBubble tip={fx('collector.filtered.info')} />
        </span>
        <span className="flex-1" />
        {held.length > 1 && (
          <Button kind="ghost" className="px-2.5 text-xs" onClick={() => setShowAll((v) => !v)}>
            {showAll ? t('common.hide') : t('common.show')}
          </Button>
        )}
        <Button
          kind="ghost"
          className="px-2.5 text-xs"
          icon={<IconRetry width={14} height={14} />}
          disabled={busy}
          onClick={() => restore(held.map((h) => h.id))}
        >
          {fx('collector.filtered.restoreAll')}
        </Button>
        <Button
          kind="ghost"
          className="px-2.5 text-xs"
          icon={<IconTrash width={14} height={14} />}
          disabled={busy}
          onClick={() => clear(held.map((h) => h.id))}
        >
          {fx('collector.filtered.clear')}
        </Button>
      </div>

      <div className="max-h-56 overflow-y-auto pb-1.5">
        {shown.map((h) => (
          <div key={h.id} className="flex items-baseline gap-3 px-4 py-1 text-xs">
            {/* The rule first, since it is what gets edited. */}
            <Tip tip={ruleOf(h)} className="max-w-[22%] shrink-0 truncate text-carbon-text">
              {ruleOf(h) || fx('collector.filtered.noRule')}
            </Tip>
            <Tip tip={h.skipReason} className="max-w-[30%] shrink-0 truncate text-carbon-textSub">
              {h.skipReason}
            </Tip>
            <Tip dir="ltr" tip={h.url} className="min-w-0 flex-1 truncate text-carbon-textMuted">
              {h.url}
            </Tip>
            <span className="flex shrink-0 items-center text-carbon-textMuted">
              {originLabel(fx, h.origin)}
              <InfoBubble tip={fx('collector.filtered.originTitle')} />
            </span>
            <span className="glim-num shrink-0 text-carbon-textMuted">{fmtDate(h.createdAt)}</span>
            <Button
              kind="ghost"
              className="shrink-0 px-2 text-xs"
              disabled={busy}
              onClick={() => restore([h.id])}
            >
              {fx('collector.filtered.restore')}
            </Button>
          </div>
        ))}
      </div>
    </div>
  );
}

// ruleOf names the rule that caught the link. The engine records one today,
// but the field is a list.
function ruleOf(h: Task): string {
  return (h.matchedRules ?? []).join(', ');
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

// English fallbacks behind the catalogue, which already has every key.
export const FILTERED_STRINGS = {
  'collector.filtered.summary': '{n} link(s) held by the link filter',
  'collector.filtered.info':
    'Links a filter rule refused. They are kept here rather than in the list above, so a filter that is working does not look like a collector full of junk - nothing was lost. Restore puts a link back and lets it past the rule that caught it, which is what you want when the rule turned out to be too broad. Clear deletes it; no file has been downloaded either way.',
  'collector.filtered.restore': 'Restore',
  'collector.filtered.restoreAll': 'Restore all',
  'collector.filtered.clear': 'Clear',
  'collector.filtered.noRule': 'the link filter',
  'collector.filtered.originTitle': 'Where this link came from',
  'collector.filtered.origin.paste': 'pasted',
  'collector.filtered.origin.crawl': 'crawled',
  'collector.filtered.origin.cnl': "Click'n'Load",
  'collector.filtered.origin.watch': 'watch folder',
  'collector.filtered.origin.container': 'container',
  'collector.filtered.restoreFailed': 'Could not restore those links. Is the server reachable?',
  'collector.filtered.clearFailed': 'Could not clear those links. Is the server reachable?',
  'collector.filtered.toastHeld': 'Staged {n} link(s); {held} held by the link filter',
  'collector.filtered.toastAllHeld': 'Nothing was staged: the link filter is holding {held} link(s)',
} as const;

export type FilteredKey = keyof typeof FILTERED_STRINGS;

/** `t` for the keys the catalogue does not have yet. */
export function useFx() {
  const { t } = useT();
  return useCallback(
    (key: FilteredKey, vars?: Record<string, string | number>) => {
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? FILTERED_STRINGS[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

// originLabel falls back to the raw origin for one the catalogue does not
// know, since the server's set grows.
function originLabel(fx: ReturnType<typeof useFx>, origin?: string): string {
  if (!origin) return '';
  const key = `collector.filtered.origin.${origin}` as FilteredKey;
  return key in en ? fx(key) : origin;
}
