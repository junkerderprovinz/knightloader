// The search box with a category picker, since a hoster's domain appears in
// every URL and a plain substring match over name and URL finds everything.
// The query's meaning lives in lib/searchQuery.ts.
import { useRef } from 'react';
import { useT, type TranslationKey } from '../lib/i18n';
import { InfoBubble, useTooltip } from './ui';
import { Dropdown } from './Dropdown';
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

/** SearchField is the input and its category picker as one control, at a field's height. */
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
  // Passed as disabled while the button is absent, so the bubble closes when the
  // button leaves: an unmounted trigger fires no mouseleave.
  const clearTip = useTooltip<HTMLButtonElement>(t('search.clear'), !value.text);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...clearTipProps } = clearTip.triggerProps;

  return (
    <div
      className={`flex h-[var(--btn-h)] min-w-[16rem] items-center gap-1 rounded-[var(--radius-control)] bg-carbon-surface2
        pe-1 ps-2.5 transition-shadow has-[input:focus]:shadow-[0_0_0_2px_var(--focus-ring)] ${className}`}
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
        className="glim-focus-drawn h-full min-w-0 flex-1 bg-transparent text-sm text-carbon-text
          placeholder:text-carbon-textMuted outline-none [&::-webkit-search-cancel-button]:hidden"
      />
      {value.text && (
        <button
          type="button"
          aria-label={t('search.clear')}
          {...clearTipProps}
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
      {clearTip.node}
      <Dropdown
        look="inset"
        label={t('search.in')}
        value={value.category}
        onChange={(category) => onChange({ ...value, category })}
        options={CATEGORIES.map((c) => ({ value: c.id, label: t(c.label) }))}
      />
      {/* One bubble for the picker and the syntax, not two identical (i)s. */}
      <InfoBubble tip={`${t('search.hint')} ${t('search.syntax')}`} className="me-1" />
    </div>
  );
}
