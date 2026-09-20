// The search box with a category picker, since a hoster's domain appears in
// every URL and a plain substring match over name and URL finds everything.
// The query's meaning lives in lib/searchQuery.ts.
import { useRef } from 'react';
import { useT, type TranslationKey } from '../lib/i18n';
import { InfoBubble } from './ui';
import { IconSearch, IconClose } from '../lib/icons';
import { type SearchCategory, type SearchQuery } from '../lib/searchQuery';

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
 * wheelSteps lets the wheel step a closed <select> one option per notch,
 * clamped at both ends (design rule 14). It is a native listener with
 * `passive: false`, because React registers onWheel passive and preventDefault
 * would not stop the page scrolling. QueueBar.tsx and RuleEditor.tsx carry the
 * same listener.
 */
function wheelSteps(el: HTMLSelectElement | null) {
  if (!el) return;
  const onWheel = (e: WheelEvent) => {
    // Only the sign of deltaY counts; trackpads report fractions.
    if (el.disabled || el.options.length < 2 || e.deltaY === 0) return;
    e.preventDefault();
    const next = Math.min(el.options.length - 1, Math.max(0, el.selectedIndex + (e.deltaY > 0 ? 1 : -1)));
    if (next === el.selectedIndex) return;
    el.selectedIndex = next;
    // A real change event, so the element's onChange handles it like a click.
    el.dispatchEvent(new Event('change', { bubbles: true }));
  };
  el.addEventListener('wheel', onWheel, { passive: false });
  return () => el.removeEventListener('wheel', onWheel);
}

/**
 * SearchField is the input and its category picker as one control. The picker
 * stays a native <select> against design rule 18 until the app has one shared
 * listbox to replace every picker with.
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
          // Escape clears rather than blurs: a forgotten query reads as lost downloads.
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
          {/* The glyph fills half the box. The hover is surface3 because the
              button sits on surface2, where --carbon-hover would darken it
              (rule 21). */}
          <IconClose width={12} height={12} />
        </button>
      )}
      <select
        ref={wheelSteps}
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
      {/* One bubble for the picker and the syntax, not two identical (i)s. */}
      <InfoBubble tip={`${t('search.hint')} ${t('search.syntax')}`} className="me-1" />
    </div>
  );
}
