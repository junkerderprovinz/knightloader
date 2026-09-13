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
 * wheelSteps is rule 14's wheel clause on a native <select>: a CLOSED select
 * steps one option per notch and fires a real `change`, without the platform's
 * own list opening at all. The platform only wires the wheel up once that list
 * is already open, which costs a click on a value somebody reaches for
 * constantly - and this is exactly such a value, since narrowing a search to
 * "Host" and back is something people do several times in a row.
 *
 * Clamped at both ends instead of wrapping: one notch too many must not land a
 * value from the other end of the list.
 *
 * A ref callback with its own cleanup (React 19) and `{ passive: false }`,
 * never onWheel: React registers onWheel passive at its root, so preventDefault
 * inside such a handler does nothing but log a warning, and the page would
 * scroll away under the pointer while the value changed.
 *
 * The same eight lines sit in components/QueueBar.tsx and
 * components/RuleEditor.tsx, the app's two other native selects. GlimStone
 * ships one copy as reference/selectScroll.ts and this app's home for it would
 * be lib/selectScroll.ts, which does not exist yet; three copies of a listener
 * is the honest price of not inventing that file from inside one component.
 */
function wheelSteps(el: HTMLSelectElement | null) {
  if (!el) return;
  const onWheel = (e: WheelEvent) => {
    // A horizontal wheel says nothing about this control, and a trackpad
    // reports fractional deltas - so read the sign of deltaY and nothing else.
    if (el.disabled || el.options.length < 2 || e.deltaY === 0) return;
    // This handler IS the scroll while the pointer sits on the control.
    e.preventDefault();
    const next = Math.min(el.options.length - 1, Math.max(0, el.selectedIndex + (e.deltaY > 0 ? 1 : -1)));
    if (next === el.selectedIndex) return;
    el.selectedIndex = next;
    // A real change event rather than a state write, so the onChange already on
    // the element picks this up exactly as it would a click on an <option>.
    el.dispatchEvent(new Event('change', { bubbles: true }));
  };
  el.addEventListener('wheel', onWheel, { passive: false });
  return () => el.removeEventListener('wheel', onWheel);
}

/**
 * SearchField is the input and its category picker as one control.
 *
 * The picker is a native <select>, and rule 18 ("a native control gets
 * replaced, not persuaded") says it should not stay one: `appearance: none`
 * reaches the closed box and never the list the platform opens on top of it.
 * What keeps it here is not that six segments would be wider than the field
 * they narrow, true as that is - it is that the replacement is ONE listbox
 * shared by every picker in the app, and building a private one inside the
 * search field is how a house ends up with two. Debt, written down as debt.
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
          {/* Half the box, like 16 in 32 and 20 in 40: a glyph alone in a
              square has no text beside it to match, so the only proportion
              available is how much of the frame the ink fills. This was 13.
              The hover above is surface3 and not --carbon-hover on purpose -
              the button carries no fill of its own but SITS on surface2, and
              --carbon-hover is a step below that, so it would darken under the
              pointer instead of lifting (rule 21). */}
          <IconClose width={12} height={12} />
        </button>
      )}
      {/* The wheel steps the category (rule 14, see wheelSteps above): the
          pointer is already on this box while somebody is reading the list it
          narrows, and a notch is cheaper than opening the platform's own menu
          to change one word. */}
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
      {/* One bubble, two sentences: what the picker does, then what the box
          itself understands. Two bubbles side by side would be two identical
          "(i)" glyphs a hand's width apart with no way to tell which is which
          before hovering both. */}
      <InfoBubble tip={`${t('search.hint')} ${t('search.syntax')}`} className="me-1" />
    </div>
  );
}
