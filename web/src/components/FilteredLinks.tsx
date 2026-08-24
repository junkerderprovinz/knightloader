import { useCallback, useState } from 'react';
import type { Task } from '../lib/api';
import { fmtDate } from '../lib/format';
import { useT, type TranslationKey } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { useToast } from '../lib/toast';
import { Button, InfoBubble } from './ui';
import { IconRetry, IconTrash } from '../lib/icons';

/**
 * The holding area: the links the link filter refused.
 *
 * It exists because the first version of the filter did the right thing badly.
 * A refused link was kept and explained — which is the whole point, JDownloader
 * eats them in silence and gets reported as a broken paste box — but it was kept
 * *in the collector*, next to the links that were about to download. So a filter
 * that was working perfectly looked like a collector full of junk, and the only
 * way to get a clean list was to switch the filter off.
 *
 * Same record, different list. Nothing is lost and nothing is hidden: the count
 * is on the strip, the rule that caught each link is next to it, and Restore puts
 * one back with that rule waived — because the commonest reason to open this list
 * at all is that the rule turned out to be too broad.
 *
 * No accent anywhere on it. The accent means activity, and a held link is the
 * opposite of activity: it is the one thing on this page that is not going to
 * happen.
 */
export function FilteredLinks({ held }: { held: Task[] }) {
  const { t } = useT();
  const fx = useFx();
  const { toast } = useToast();
  const [showAll, setShowAll] = useState(false);
  const [busy, setBusy] = useState(false);

  // No state of its own beyond that, and no socket. The links are tasks, so the
  // page above already has them from the one stream this app opens; a second
  // subscription here would be a second copy to keep in step and a second thing
  // to reconnect.

  const act = useCallback(
    async (run: () => Promise<Response>, failKey: FilteredKey) => {
      setBusy(true);
      try {
        const resp = await run().catch(() => null);
        if (!resp?.ok) toast(fx(failKey), 'fail');
      } finally {
        setBusy(false);
      }
      // Nothing is written to local state on success. The server broadcasts the
      // tasks it changed, and taking them off this list optimistically would put
      // them back on the next message if the write had in fact failed.
    },
    [fx, toast],
  );

  const restore = (ids: string[]) => act(() => restoreFiltered(ids), 'collector.filtered.restoreFailed');
  const clear = (ids: string[]) => act(() => clearFiltered(ids), 'collector.filtered.clearFailed');

  if (!held.length) return null;

  // Newest first: the links the paste the user is still looking at produced are
  // the ones this strip has to explain.
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
            {/* The rule first, because it is the thing the user goes and edits.
                It is data on the task, not a name parsed back out of the
                sentence next to it — that sentence is going to be translated. */}
            <span className="max-w-[22%] shrink-0 truncate text-carbon-text" title={ruleOf(h)}>
              {ruleOf(h) || fx('collector.filtered.noRule')}
            </span>
            <span className="max-w-[30%] shrink-0 truncate text-carbon-textSub" title={h.skipReason}>
              {h.skipReason}
            </span>
            {/* dir=ltr: a URL is not prose and must not be reordered when the
                interface language is right-to-left. */}
            <span dir="ltr" className="min-w-0 flex-1 truncate text-carbon-textMuted" title={h.url}>
              {h.url}
            </span>
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

/**
 * ruleOf is which rule caught the link. The engine writes exactly one, because a
 * filter set stops at the rule that refuses; a later wave that lets several act
 * on one link would show them all, so this reads the list rather than [0].
 */
function ruleOf(h: Task): string {
  return (h.matchedRules ?? []).join(', ');
}

// ---------------------------------------------------------------------------
// The two writes. They live here rather than in lib/api.ts for the same reason
// the rule editor's do: this is the only caller, and lib/api.ts is a shared file
// with one writer per wave.

const restoreFiltered = (ids: string[]) =>
  fetch('/api/collector/filtered/restore', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids }),
  });

// The ids go in the query, not in a body: a DELETE with a body is not something
// every proxy between a browser and this server forwards, and an empty id list
// on this route means "all of them".
const clearFiltered = (ids: string[]) =>
  fetch(`/api/collector/filtered?ids=${encodeURIComponent(ids.join(','))}`, { method: 'DELETE' });

// ---------------------------------------------------------------------------
// Strings. A local table with the real catalogue asked first, so that a wave
// which writes strings and a wave which translates them never queue behind each
// other on one file.
//
// All sixteen keys are now in en.ts and in all 41 other locales, so `t` answers
// first for every one of them and nothing below is ever read. It is kept only
// because deleting it means retyping FilteredKey and useFx, which is a separate
// edit from this one; the table is dead weight, not a second source of truth.

export const FILTERED_STRINGS = {
  'collector.filtered.summary': '{n} link(s) held by the link filter',
  'collector.filtered.info':
    'Links a filter rule refused. They are kept here rather than in the list above, so a filter that is working does not look like a collector full of junk — nothing was lost. Restore puts a link back and lets it past the rule that caught it, which is what you want when the rule turned out to be too broad. Clear deletes it; no file has been downloaded either way.',
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

/**
 * An origin with no label of its own falls back to the raw value rather than to
 * a blank cell. The set is the server's, and an entrance added in a later wave
 * has to show up as its own name instead of as a gap that reads as a bug.
 *
 * The membership test is against the catalogue, not against the local table
 * below: the table is a fallback the catalogue now answers ahead of, so an
 * entrance the translators have named but nobody has copied back down here would
 * otherwise render as a raw id with its translation one lookup away.
 */
function originLabel(fx: ReturnType<typeof useFx>, origin?: string): string {
  if (!origin) return '';
  const key = `collector.filtered.origin.${origin}` as FilteredKey;
  return key in en ? fx(key) : origin;
}
