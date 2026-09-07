// Searching a download list by category rather than by one blind substring.
//
// A single match over "name plus url" is wrong in both directions at once: the
// hoster's own domain appears in every URL, so a search for it returns the whole
// list, and a query that happens to be a fragment of an unrelated link buries
// the row that was actually being looked for. Naming the field turns the search
// back into a question with an answer.
//
// This file is the control. What a query MEANS lives in lib/searchQuery.ts,
// which imports nothing that survives compilation so that the parser can be
// exercised on its own (web/check-search-query.mjs) rather than only through a
// rendered component.
import { useRef } from 'react';
import { useT, type TranslationKey } from '../lib/i18n';
import { InfoBubble } from './ui';
import { IconSearch, IconClose } from '../lib/icons';
import { type SearchCategory, type SearchQuery } from '../lib/searchQuery';

// Re-exported from here because this is where every page already imports the
// search from, and moving the meaning into its own module is not a reason to
// make four pages edit their import lines.
export { EMPTY_SEARCH, matchesSearch, type SearchCategory, type SearchQuery } from '../lib/searchQuery';

const CATEGORIES: { id: SearchCategory; label: TranslationKey }[] = [
  { id: 'any', label: 'search.any' },
  { id: 'name', label: 'search.name' },
  { id: 'host', label: 'search.host' },
  { id: 'package', label: 'search.package' },
  { id: 'comment', label: 'search.comment' },
  { id: 'url', label: 'search.url' },
];

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
        className="glim-select appearance-none pe-6 shrink-0 rounded-[var(--radius-control)] bg-carbon-surface3/70 px-2 py-1 text-xs
          text-carbon-textSub outline-none"
      >
        {CATEGORIES.map((c) => (
          <option key={c.id} value={c.id}>
            {t(c.label)}
          </option>
        ))}
      </select>
      {/* One bubble, two sentences: what the picker does, then what the box
          itself understands. Two bubbles side by side would be two identical
          "(i)" glyphs a hand's width apart with no way to tell which is which
          before hovering both. */}
      <InfoBubble tip={`${t('search.hint')} ${t('search.syntax')}`} className="me-1" />
    </div>
  );
}
