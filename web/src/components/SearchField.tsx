// The search box with a category picker, since a hoster's domain appears in
// every URL and a plain substring match over name and URL finds everything.
// The query's meaning lives in lib/searchQuery.ts.
import type { Ref } from 'react';
import { useT, type TranslationKey } from '../lib/i18n';
import { Dropdown } from './Dropdown';
import { SearchBox } from './SearchBox';
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

/** SearchField is the list search: the shared field with its category picker inside it. */
export function SearchField({
  value,
  onChange,
  inputRef,
  className = '',
}: {
  value: SearchQuery;
  onChange: (next: SearchQuery) => void;
  inputRef?: Ref<HTMLInputElement>;
  className?: string;
}) {
  const { t } = useT();
  return (
    <SearchBox
      value={value.text}
      onChange={(text) => onChange({ ...value, text })}
      inputRef={inputRef}
      label={t('search.placeholder')}
      // One bubble for the picker and the syntax, not two identical (i)s.
      hint={`${t('search.hint')} ${t('search.syntax')}`}
      className={`min-w-[16rem] ${className}`}
      end={
        <Dropdown
          look="inset"
          label={t('search.in')}
          value={value.category}
          onChange={(category) => onChange({ ...value, category })}
          options={CATEGORIES.map((c) => ({ value: c.id, label: t(c.label) }))}
        />
      }
    />
  );
}
