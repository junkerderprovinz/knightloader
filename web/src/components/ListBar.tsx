// Filter, Sort and Search: the three buttons that end the row above a list's
// card (GlimStone, "Lists"). Filter opens the quick filters, Sort offers what a
// click on a column header does, and Search turns into its field. The page
// hands in what stands before them and folds it when the row runs short
// (lib/rowFit.ts).
import { useEffect, useRef, useState, type ReactNode, type RefObject } from 'react';
import { useT } from '../lib/i18n';
import { IconClose, IconFilter, IconSearch, IconSort } from '../lib/icons';
import { ContextMenu, anchorBelow, useContextMenu, type MenuGroup } from './ContextMenu';
import type { QuickFilter, QuickFilterId } from './ListToolbar';
import { SearchField, type SearchQuery } from './SearchField';
import { Tabs } from './Tabs';
import type { ListSort } from './TaskList';
import { IconBadge } from './ui';

/** Whether a press landed in one of the boxes, or in a menu one of them opened. */
function pressedIn(at: EventTarget | null, ...boxes: (HTMLElement | null)[]): boolean {
  if (!(at instanceof Node)) return false;
  if (boxes.some((box) => box?.contains(at))) return true;
  return at instanceof Element && at.closest('[role="menu"]') !== null;
}

/** useDismiss closes something open on a press outside its boxes or on Escape. */
function useDismiss(open: boolean, close: () => void, ...boxes: RefObject<HTMLElement | null>[]): void {
  useEffect(() => {
    if (!open) return;
    const onPress = (e: MouseEvent) => {
      if (!pressedIn(e.target, ...boxes.map((b) => b.current))) close();
    };
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && close();
    document.addEventListener('mousedown', onPress);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onPress);
      document.removeEventListener('keydown', onKey);
    };
    // The refs are stable; their boxes are read when the press arrives.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, close]);
}

/**
 * Marked wraps one of the three buttons and puts a dot on its corner while the
 * button holds something: a filter that is on, a sort, a search text. The
 * buttons themselves keep one weight.
 */
function Marked({
  on,
  boxRef,
  children,
}: {
  on: boolean;
  boxRef?: RefObject<HTMLDivElement | null>;
  children: ReactNode;
}) {
  return (
    <div ref={boxRef} className="relative shrink-0">
      {children}
      {on && (
        <span
          aria-hidden
          data-marked
          className="pointer-events-none absolute -end-1 -top-1 h-2 w-2 rounded-[var(--radius-pill)] bg-accent"
        />
      )}
    </div>
  );
}

export function ListBar({
  rowRef,
  glyphs,
  scrolls,
  children,
  filters,
  active,
  onToggleFilter,
  onClearFilters,
  sorts,
  search,
  onSearch,
  searchOpen,
  onSearchOpen,
}: {
  /** The row the page measures with useRowFit. */
  rowRef: RefObject<HTMLDivElement | null>;
  /** The three buttons as glyphs, for a row short of room for their words. */
  glyphs: boolean;
  /** The row scrolls sideways, on a phone. What opens then opens under it. */
  scrolls: boolean;
  /** What stands before the three buttons. */
  children: ReactNode;
  /** The quick filters on offer, each with how many rows it matches. */
  filters: { f: QuickFilter; n: number }[];
  active: ReadonlySet<QuickFilterId>;
  onToggleFilter: (id: QuickFilterId) => void;
  onClearFilters: () => void;
  /** One entry per card the page draws, the main list first. */
  sorts: ListSort[];
  search: SearchQuery;
  onSearch: (next: SearchQuery) => void;
  /** Whether the search was opened by hand. A search with text shows its field anyway. */
  searchOpen: boolean;
  onSearchOpen: (open: boolean) => void;
}) {
  const { t } = useT();
  const [filterOpen, setFilterOpen] = useState(false);
  const filterBox = useRef<HTMLDivElement>(null);
  const filterPanel = useRef<HTMLDivElement>(null);
  const searchBox = useRef<HTMLDivElement>(null);
  const searchButton = useRef<HTMLDivElement>(null);
  const sortMenu = useContextMenu();

  useDismiss(filterOpen, () => setFilterOpen(false), filterBox, filterPanel);
  // A field holding text stays, so a forgotten query cannot hide behind a button.
  useDismiss(searchOpen && !search.text, () => onSearchOpen(false), searchBox, searchButton);

  const searching = searchOpen || search.text !== '';
  useEffect(() => {
    if (searchOpen) searchBox.current?.querySelector('input')?.focus();
  }, [searchOpen]);

  // One card sorts from the menu itself. Several get an entry each, named by
  // the card, with its own menu behind it.
  const sortGroups: MenuGroup[] =
    sorts.length === 1
      ? sorts[0].groups
      : [{ id: 'cards', items: sorts.map((s) => ({ id: s.title, label: s.title, detail: s.current, submenu: s.groups })) }];

  return (
    <div className="relative shrink-0">
      <div
        ref={rowRef}
        className={`flex items-center gap-2 ${scrolls ? '-my-1 overflow-x-auto py-1' : ''}`}
        role="group"
        aria-label={t('list.actions')}
      >
        {children}

        <Marked on={active.size > 0} boxRef={filterBox}>
          <IconBadge
            labelled={!glyphs}
            hue={0}
            icon={<IconFilter width={16} height={16} />}
            title={t('list.filter')}
            aria-label={t('list.filter')}
            aria-haspopup="dialog"
            aria-expanded={filterOpen}
            disabled={filters.length === 0}
            onClick={() => setFilterOpen((v) => !v)}
          />
        </Marked>

        <Marked on={sorts.some((s) => s.current !== undefined)}>
          <IconBadge
            labelled={!glyphs}
            hue={1}
            icon={<IconSort width={16} height={16} />}
            title={t('list.sort')}
            aria-label={t('list.sort')}
            aria-haspopup="menu"
            aria-expanded={!!sortMenu.anchor}
            onClick={(e) => sortMenu.openAt(anchorBelow(e.currentTarget))}
          />
        </Marked>

        {searching && !scrolls ? (
          <div ref={searchBox} className="shrink-0">
            <SearchField value={search} onChange={onSearch} className="w-80" />
          </div>
        ) : (
          <Marked on={search.text !== ''} boxRef={searchButton}>
            <IconBadge
              labelled={!glyphs}
              hue={2}
              icon={<IconSearch width={16} height={16} />}
              // The badge's own name rather than the field's placeholder;
              // web/check-placeholder-as-label.mjs keeps it so.
              title={t('search.toggle')}
              aria-label={t('search.toggle')}
              aria-expanded={searching}
              onClick={() => onSearchOpen(!searchOpen)}
            />
          </Marked>
        )}
      </div>

      {/* Under the row rather than inside it: a row that scrolls would clip it. */}
      {searching && scrolls && (
        <div ref={searchBox} className="mt-2">
          <SearchField value={search} onChange={onSearch} className="w-full" />
        </div>
      )}

      {filterOpen && (
        <div
          ref={filterPanel}
          role="dialog"
          aria-label={t('filter.label')}
          className="glim-card glim-fade absolute end-0 top-full z-20 mt-2 flex w-[25rem] max-w-full flex-col gap-3 p-4"
        >
          <div className="text-subline text-carbon-textMuted">{t('filter.label')}</div>
          <Tabs
            inline
            select="many"
            size="sm"
            label={t('filter.label')}
            active={active}
            onSelect={(id) => onToggleFilter(id as QuickFilterId)}
            items={filters.map(({ f, n }) => ({ id: f.id, label: t(f.label), badge: n }))}
          />
          <div className="flex justify-end">
            <IconBadge
              labelled
              icon={<IconClose width={16} height={16} />}
              title={t('filter.clear')}
              aria-label={t('filter.clear')}
              disabled={active.size === 0}
              onClick={onClearFilters}
            />
          </div>
        </div>
      )}

      {sortMenu.anchor && (
        <ContextMenu anchor={sortMenu.anchor} label={t('list.sort')} onClose={sortMenu.close} groups={sortGroups} />
      )}
    </div>
  );
}
