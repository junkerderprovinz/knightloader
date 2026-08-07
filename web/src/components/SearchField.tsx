// Searching a download list by category rather than by one blind substring.
//
// A single match over "name plus url" is wrong in both directions at once: the
// hoster's own domain appears in every URL, so a search for it returns the whole
// list, and a query that happens to be a fragment of an unrelated link buries
// the row that was actually being looked for. Naming the field turns the search
// back into a question with an answer.
import { useRef } from 'react';
import { type Task } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { InfoBubble } from './ui';
import { IconSearch, IconClose } from '../lib/icons';

export type SearchCategory = 'any' | 'name' | 'host' | 'package' | 'comment' | 'url';

export interface SearchQuery {
  text: string;
  category: SearchCategory;
}

export const EMPTY_SEARCH: SearchQuery = { text: '', category: 'any' };

const CATEGORIES: { id: SearchCategory; label: TranslationKey }[] = [
  { id: 'any', label: 'search.any' },
  { id: 'name', label: 'search.name' },
  { id: 'host', label: 'search.host' },
  { id: 'package', label: 'search.package' },
  { id: 'comment', label: 'search.comment' },
  { id: 'url', label: 'search.url' },
];

/**
 * hostOf is the file host a row would be sorted by.
 *
 * Task.host is written by the wave that builds the host lookup and is empty
 * until then, so the URL's own hostname stands in. Through a debrid service the
 * two genuinely differ — the stored host is where the file lives, the URL is
 * where the bytes come from — and when the field is filled it must win.
 */
function hostOf(t: Task): string {
  if (t.host) return t.host;
  try {
    return new URL(t.url).hostname.replace(/^www\./, '');
  } catch {
    return '';
  }
}

/**
 * fieldOf is what one category reads off a task.
 *
 * `name` falls back to the URL because that is what an unresolved link renders
 * as its name: a search that skipped it would claim no row matches while the
 * matching text is on screen.
 */
function fieldOf(t: Task, c: Exclude<SearchCategory, 'any'>): string {
  switch (c) {
    case 'name':
      return t.name || t.url;
    case 'host':
      return hostOf(t);
    case 'package':
      return t.package;
    case 'comment':
      return t.comment ?? '';
    case 'url':
      return t.url;
  }
}

/** matchesSearch is the filter itself, so the pages cannot disagree about it. */
export function matchesSearch(t: Task, q: SearchQuery): boolean {
  const needle = q.text.trim().toLowerCase();
  if (!needle) return true;
  if (q.category !== 'any') return fieldOf(t, q.category).toLowerCase().includes(needle);
  return CATEGORIES.some((c) => c.id !== 'any' && fieldOf(t, c.id).toLowerCase().includes(needle));
}

/**
 * SearchField is the input and its category picker as one control.
 *
 * The picker is a native <select>: it is one of a fixed handful of values, it
 * has to be reachable by keyboard and by screen reader, and six segments of a
 * segmented control would take more width than the field they narrow.
 */
export function SearchField({
  value,
  onChange,
  className = '',
}: {
  value: SearchQuery;
  onChange: (next: SearchQuery) => void;
  className?: string;
}) {
  const { t } = useT();
  const input = useRef<HTMLInputElement>(null);

  return (
    <div
      className={`flex min-w-[16rem] items-center gap-1 rounded-[var(--radius-control)] bg-carbon-surface2
        pe-1 ps-2.5 transition-shadow focus-within:shadow-[0_0_0_2px_var(--focus-ring)] ${className}`}
    >
      <IconSearch className="shrink-0 text-carbon-textMuted" width={15} height={15} />
      <input
        ref={input}
        type="search"
        value={value.text}
        onChange={(e) => onChange({ ...value, text: e.target.value })}
        onKeyDown={(e) => {
          // Escape clears rather than blurs: an empty list with a forgotten
          // query in a field nobody is looking at reads as lost downloads.
          if (e.key === 'Escape' && value.text) {
            e.stopPropagation();
            onChange({ ...value, text: '' });
          }
        }}
        placeholder={t('search.placeholder')}
        aria-label={t('search.placeholder')}
        className="min-w-0 flex-1 bg-transparent py-2 text-sm text-carbon-text
          placeholder:text-carbon-textMuted outline-none [&::-webkit-search-cancel-button]:hidden"
      />
      {value.text && (
        <button
          type="button"
          aria-label={t('search.clear')}
          title={t('search.clear')}
          onClick={() => {
            onChange({ ...value, text: '' });
            input.current?.focus();
          }}
          className="grid h-6 w-6 shrink-0 place-items-center rounded-[var(--radius-control)]
            text-carbon-textMuted transition-colors hover:bg-carbon-surface3 hover:text-carbon-text"
        >
          <IconClose width={13} height={13} />
        </button>
      )}
      <select
        value={value.category}
        onChange={(e) => onChange({ ...value, category: e.target.value as SearchCategory })}
        aria-label={t('search.in')}
        className="shrink-0 rounded-[var(--radius-control)] bg-carbon-surface3/70 px-2 py-1 text-xs
          text-carbon-textSub outline-none"
      >
        {CATEGORIES.map((c) => (
          <option key={c.id} value={c.id}>
            {t(c.label)}
          </option>
        ))}
      </select>
      <InfoBubble tip={t('search.hint')} className="me-1" />
    </div>
  );
}
