// The list search with a category picker, since a hoster's domain appears in
// every URL and a plain substring match over name and URL finds everything.
// The query's meaning lives in lib/searchQuery.ts.
//
// It has no button of its own: it floats inside the list's area once somebody
// types there or presses Ctrl+F, and Escape closes it. A field that holds text
// stays, so a forgotten search cannot hide.
import { useCallback, useEffect, useImperativeHandle, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type Ref, type RefObject } from 'react';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconClose } from '../lib/icons';
import { Dropdown } from './Dropdown';
import { SearchBox } from './SearchBox';
import type { SearchHandle } from './SearchToggle';
import { Button } from './ui';
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

/** Whether a key press is a character somebody types, and nothing else wants it. */
function startsTyping(e: KeyboardEvent): boolean {
  if (e.ctrlKey || e.metaKey || e.altKey || e.key.length !== 1 || e.key === ' ') return false;
  const at = e.target;
  if (!(at instanceof HTMLElement)) return true;
  return !at.closest('input, textarea, select, [contenteditable="true"], [role="menu"], [role="dialog"]');
}

/**
 * ListSearch floats over the top of the list's area, below the column heads,
 * inside an element that is positioned and does not scroll.
 * `area` is the element whose key presses open it: a character typed there
 * starts the search with that character.
 */
export function ListSearch({
  value,
  onChange,
  handle,
  area,
}: {
  value: SearchQuery;
  onChange: (next: SearchQuery) => void;
  handle?: Ref<SearchHandle>;
  area: RefObject<HTMLElement | null>;
}) {
  const { t } = useT();
  const [opened, setOpened] = useState(false);
  // Bumped on every request for the caret, which the field takes once drawn.
  const [asked, setAsked] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const shown = opened || value.text !== '';
  const latest = useRef(value);
  latest.current = value;

  const open = useCallback(() => {
    setOpened(true);
    setAsked((n) => n + 1);
  }, []);
  useImperativeHandle(handle, () => ({ open }), [open]);

  useEffect(() => {
    if (asked > 0) input.current?.focus();
  }, [asked]);

  useEffect(() => {
    const el = area.current;
    if (!el) return;
    const onKey = (e: KeyboardEvent) => {
      if (!startsTyping(e)) return;
      e.preventDefault();
      onChange({ ...latest.current, text: latest.current.text + e.key });
      open();
    };
    el.addEventListener('keydown', onKey);
    return () => el.removeEventListener('keydown', onKey);
  }, [area, onChange, open]);

  const close = () => {
    onChange({ ...value, text: '' });
    setOpened(false);
    area.current?.focus();
  };

  function keyDown(e: ReactKeyboardEvent<HTMLInputElement>) {
    if (e.key !== 'Escape') return;
    // Escape closes the search before any window around it.
    e.stopPropagation();
    e.preventDefault();
    close();
  }

  if (!shown) return null;
  return (
    // Over the rows rather than above them, so the list keeps its place, in
    // the area around the list that does not scroll.
    <div role="search" className="glim-card glim-fade absolute end-4 top-14 z-20 flex items-center gap-2 p-2">
      <SearchBox
        value={value.text}
        onChange={(text) => onChange({ ...value, text })}
        inputRef={input}
        onKeyDown={keyDown}
        label={t('search.placeholder')}
        // One bubble for the picker and the syntax, not two identical (i)s.
        hint={`${t('search.hint')} ${t('search.syntax')}`}
        className="w-[22rem] max-w-[calc(100vw-4rem)]"
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
      <Button kind="secondary" icon={<IconClose />} title={t('search.close')} aria-label={t('search.close')} onClick={close} />
    </div>
  );
}
