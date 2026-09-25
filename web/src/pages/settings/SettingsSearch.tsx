// One search box over all of Settings. It searches page names, card titles,
// captions and the text behind every (i), and jumps to the result instead of
// filtering the page, which would scramble the card hues and contradict the
// rail. Values are searched on the Advanced page instead. A row result marks
// when only its explanation matched.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useT, type TranslationKey } from '../../lib/i18n';
import { en } from '../../lib/locales/en';
import { IconClose, IconSearch } from '../../lib/icons';
import { fold, scoreFolded, scoreProse } from '../../lib/rank';
import { useToast } from '../../lib/toast';
import { useUIState } from '../../lib/uistate';
import { InfoBubble, useTooltip } from '../../components/ui';
// A safe import cycle: orderPages is a hoisted function declaration called
// only at render time, and a second copy would drift.
import { orderPages } from '../Settings';
import type { FeaturePage } from './features';
import {
  clearJump,
  clearSearchFocus,
  FOCUS_REQUEST_TTL_MS,
  locateAndMark,
  requestJump,
  usePendingJump,
  useSearchFocusAskedAt,
} from './jump';
import { SETTINGS_INDEX } from './searchIndex';
import { hasContent } from './registry';
import { useRevealOnScrollUp } from './revealOnScrollUp';
import { label as pageLabel } from './tx';

/** The debounce Advanced.tsx's filter uses too, so typing does not stutter. */
const DEBOUNCE_MS = 150;

/** How many results are drawn; the full count is still announced. */
const RENDER_CAP = 40;

/**
 * The result tiers only break ties, so a row called "Speed limit" is not
 * outranked by cards whose explanations mention speed.
 */
const TIER = { page: 0, card: 1, row: 2 } as const;

/**
 * Added to a match in explanation text so every name match sorts above every
 * prose match; larger than anything else the rank can add.
 */
const PROSE_PENALTY = 1_000_000;

interface Item {
  /** Stable within one build of the index; the React key and the aria-activedescendant target. */
  id: string;
  tier: keyof typeof TIER;
  page: string;
  /** The card's SectionTitle key, for everything but a section result. */
  title?: TranslationKey;
  /** The row's caption key; absent for a card result and for `also` text. */
  label?: TranslationKey;
  name: string;
  /** The card's name, for line two of a row result. */
  cardName?: string;
  /** The displayed name, folded; matched by name and by abbreviation. */
  foldedName: string;
  /** The (i) text and a card's `body` prose, folded; matched by substring only. */
  foldedProse: string;
}

/**
 * text resolves a key, returning '' for one missing from the catalogue, where
 * `t` returns undefined and the matcher would throw.
 */
function text(t: (k: TranslationKey) => string, key: TranslationKey | undefined): string {
  if (!key) return '';
  return key in en ? t(key) : '';
}

/**
 * buildItems resolves and folds every searchable string in the index. Callers
 * rebuild it when `t` changes, because `t` switches from English to the chosen
 * language once that chunk loads.
 */
function buildItems(t: (k: TranslationKey) => string, pages: FeaturePage[]): Item[] {
  const items: Item[] = [];
  for (const p of pages) {
    // Only pages the server sent and this build draws.
    if (!hasContent(p.id)) continue;
    const pageName = pageLabel(t, 'settings.nav.', p.id);
    items.push({
      id: `page:${p.id}`,
      tier: 'page',
      page: p.id,
      name: pageName,
      foldedName: fold(pageName),
      foldedProse: '',
    });

    for (const card of SETTINGS_INDEX[p.id] ?? []) {
      const cardName = text(t, card.title);
      if (!cardName) continue;
      // A card's hint and body are one haystack, so they give one result.
      const cardProse = [card.hint, ...(card.body ?? [])].map((k) => text(t, k)).join(' ');
      items.push({
        id: `card:${p.id}:${card.title}`,
        tier: 'card',
        page: p.id,
        title: card.title,
        name: cardName,
        foldedName: fold(cardName),
        foldedProse: fold(cardProse),
      });

      for (const row of card.rows) {
        const rowName = text(t, row.key);
        if (!rowName) continue;
        items.push({
          id: `row:${p.id}:${card.title}:${row.key}`,
          tier: 'row',
          page: p.id,
          title: card.title,
          label: row.key,
          name: rowName,
          cardName,
          foldedName: fold(rowName),
          foldedProse: fold(text(t, row.hint)),
        });
      }

      // `also` names get a row-shaped result without a `label`, which lands on
      // the card.
      for (const key of card.also ?? []) {
        const alsoName = text(t, key);
        if (!alsoName) continue;
        items.push({
          id: `also:${p.id}:${card.title}:${key}`,
          tier: 'row',
          page: p.id,
          title: card.title,
          name: alsoName,
          cardName,
          foldedName: fold(alsoName),
          foldedProse: '',
        });
      }
    }
  }
  return items;
}

interface Hit {
  item: Item;
  rank: number;
  /** True when nothing in the visible name matched and the explanation did. */
  inHint: boolean;
}

export function SettingsSearch({ pages }: { pages: FeaturePage[] }) {
  const { t } = useT();
  const navigate = useNavigate();
  const { toast } = useToast();
  const inputRef = useRef<HTMLInputElement>(null);

  // Kept here rather than in SettingsPage, where every keystroke would re-render
  // the rail and the mounted sub-page.
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  // Passed as disabled while the clear button is absent, so the bubble closes
  // when the button leaves: an unmounted trigger fires no mouseleave.
  const clearTip = useTooltip<HTMLButtonElement>(t('settings.search.clear'), !query);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...clearTipProps } = clearTip.triggerProps;

  // Pinned while in use (open, text in the box, or focus inside), so a single
  // wheel tick cannot take it away from somebody typing.
  const pinned =
    open || query !== '' || (typeof document !== 'undefined' && document.activeElement === inputRef.current);
  // Changing page resets the bar.
  const { pathname } = useLocation();
  // Declared up here because the palette command below needs `show`.
  const { revealed, barRef, hide, show } = useRevealOnScrollUp(pathname, pinned);

  const [settled, setSettled] = useState('');
  useEffect(() => {
    const id = window.setTimeout(() => setSettled(query), DEBOUNCE_MS);
    return () => window.clearTimeout(id);
  }, [query]);

  // The rail's drag order; the first paint may still use the server's order
  // until the stored document arrives.
  const [order] = useUIState<string[]>('settingsTabOrder', []);
  const ordered = useMemo(() => orderPages(pages, order), [pages, order]);

  const items = useMemo(() => buildItems(t, ordered), [t, ordered]);

  const hits = useMemo<Hit[]>(() => {
    const q = fold(settled.trim());
    if (!q) return [];
    const out: Hit[] = [];
    for (const item of items) {
      // Names match by abbreviation as well; prose by substring only, since
      // every query is a subsequence of a long paragraph.
      let s = scoreFolded(item.foldedName, q);
      let inHint = false;
      if (s < 0 && item.foldedProse) {
        s = scoreProse(item.foldedProse, q);
        inHint = s >= 0;
      }
      if (s < 0) continue;
      // Name before prose, then match quality, then tier.
      out.push({ item, rank: (inHint ? PROSE_PENALTY : 0) + s * 100 + TIER[item.tier], inHint });
    }
    out.sort((a, b) => a.rank - b.rank);
    return out;
  }, [items, settled]);

  /**
   * Grouped by page, best hit first, with the rail order breaking ties, so
   * Enter picks the best answer even on a page dragged to the bottom.
   */
  const groups = useMemo(() => {
    const rail = new Map(ordered.map((p, i) => [p.id, i]));
    const byPage = new Map<string, Hit[]>();
    for (const h of hits.slice(0, RENDER_CAP)) {
      const list = byPage.get(h.item.page);
      if (list) list.push(h);
      else byPage.set(h.item.page, [h]);
    }
    return [...byPage.entries()].sort(
      (a, b) => a[1][0].rank - b[1][0].rank || (rail.get(a[0]) ?? 0) - (rail.get(b[0]) ?? 0),
    );
  }, [hits, ordered]);

  // The rendered rows in render order, which the arrow keys and Enter index.
  const shown = useMemo(() => groups.flatMap(([, list]) => list), [groups]);

  useEffect(() => {
    setActive((i) => Math.min(i, Math.max(0, shown.length - 1)));
  }, [shown.length]);

  const listId = 'settings-search-list';
  const activeId = shown[active] ? `settings-search-opt-${shown[active].item.id}` : undefined;

  // Keeps the highlighted row on screen, moving the list as little as possible.
  const itemRefs = useRef(new Map<string, HTMLButtonElement>());
  useEffect(() => {
    const hit = shown[active];
    if (hit) itemRefs.current.get(hit.item.id)?.scrollIntoView({ block: 'nearest' });
  }, [active, shown]);

  // The palette's "Search all settings" leaves a timestamp in jump.ts. Read on
  // mount too, since the command asks before this field exists.
  const focusAskedAt = useSearchFocusAskedAt();
  useEffect(() => {
    if (focusAskedAt === 0) return;
    if (Date.now() - focusAskedAt > FOCUS_REQUEST_TTL_MS) {
      clearSearchFocus();
      return;
    }
    clearSearchFocus();
    // Reveal the bar first, or there is nothing to focus.
    show();
    const raf = requestAnimationFrame(() => inputRef.current?.focus());
    return () => cancelAnimationFrame(raf);
  }, [focusAskedAt, show]);

  // The other half of the jump, keyed on the nonce so asking twice looks twice.
  const jump = usePendingJump();
  const jumpNonce = jump?.nonce ?? 0;
  useEffect(() => {
    if (!jump) return;
    const title = text(t, jump.title);
    if (!title) {
      clearJump();
      return;
    }
    const cancel = locateAndMark({
      title,
      label: text(t, jump.label) || undefined,
      onDone: (outcome) => {
        // Landed on the card without the row; the reason cannot be known here.
        if (outcome === 'card' && jump.label) {
          toast(t('settings.search.rowHidden', { card: title }), 'info');
        }
        clearJump();
      },
    });
    return cancel;
    // Keyed on the nonce: an equal object is the same request.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jumpNonce]);

  const pick = useCallback(
    (hit: Hit) => {
      const { item } = hit;
      setOpen(false);
      if (item.tier !== 'page' && item.title) {
        requestJump({ page: item.page, title: item.title, label: item.label });
      } else {
        // A section result retires any jump still looking, or it would mark
        // something on the new page.
        clearJump();
      }
      navigate(`/settings/${item.page}`);
    },
    [navigate],
  );

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') {
      // stopPropagation, because the command palette also closes on Escape.
      e.stopPropagation();
      if (open) {
        e.preventDefault();
        setOpen(false);
      } else if (query) {
        e.preventDefault();
        setQuery('');
      } else {
        // A third Escape with list and box empty hides the bar.
        e.preventDefault();
        hide();
      }
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      setOpen(true);
      setActive((i) => Math.min(i + 1, shown.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActive((i) => Math.max(i - 1, 0));
    } else if (e.key === 'Enter') {
      const hit = shown[active];
      if (hit) {
        e.preventDefault();
        pick(hit);
      }
    } else if (e.key === 'Tab') {
      // Tabbing out means done, as in the command palette.
      setOpen(false);
    }
  }

  const showList = open && settled.trim() !== '';
  let rowIndex = -1;

  // Not rendered at all until summoned, so it is out of the tab order and
  // hidden from screen readers.
  if (!revealed) return null;

  return (
    // Sits in the flow above the first card (see revealOnScrollUp.ts), not in
    // PageHeader, where it would push the rail down.
    <div
      ref={barRef}
      onBlur={(e) => {
        // Closes only when focus leaves both the box and its list.
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false);
      }}
    >
      <div
        className="flex h-[var(--btn-h)] items-center gap-1 rounded-[var(--radius-control)] bg-carbon-surface2 pe-1 ps-2.5
          transition-shadow has-[input:focus]:shadow-[0_0_0_2px_var(--focus-ring)]"
      >
        <IconSearch className="shrink-0 text-carbon-textMuted" width={15} height={15} />
        <input
          ref={inputRef}
          type="search"
          value={query}
          role="combobox"
          aria-expanded={showList}
          aria-controls={listId}
          aria-activedescendant={showList ? activeId : undefined}
          aria-autocomplete="list"
          placeholder={t('settings.search.placeholder')}
          aria-label={t('settings.search.placeholder')}
          onChange={(e) => {
            setQuery(e.target.value);
            setOpen(true);
            setActive(0);
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={onKeyDown}
          className="glim-focus-drawn h-full min-w-0 flex-1 bg-transparent text-sm text-carbon-text
            placeholder:text-carbon-textMuted outline-none [&::-webkit-search-cancel-button]:hidden"
        />
        {query && (
          <button
            type="button"
            aria-label={t('settings.search.clear')}
            {...clearTipProps}
            onClick={() => {
              setQuery('');
              setOpen(false);
              inputRef.current?.focus();
            }}
            className="grid h-6 w-6 shrink-0 place-items-center rounded-[var(--radius-control)]
              text-carbon-textMuted transition-colors hover:bg-carbon-surface3 hover:text-carbon-text"
          >
            <IconClose width={13} height={13} />
          </button>
        )}
        {clearTip.node}
        <InfoBubble tip={t('settings.search.hint')} className="me-1" />
      </div>

      {/* A child of the bar rather than a portal, so it moves with it. */}
      {showList && (
        <div className="relative">
          <div
            className="glim-card glim-fade absolute inset-x-0 top-2 z-30 max-h-[60vh] overflow-y-auto py-1.5"
            role="listbox"
            id={listId}
            aria-label={t('settings.search.results')}
          >
            {hits.length === 0 && (
              <p className="px-4 py-6 text-center text-xs text-carbon-textMuted">{t('settings.search.noMatch')}</p>
            )}
            {groups.map(([page, list]) => (
              // A real group, so a screen reader hears which section each
              // option belongs to.
              <div key={page} role="group" aria-label={pageLabel(t, 'settings.nav.', page)} className="py-1">
                <div className="glim-eyebrow px-4 pb-1">{pageLabel(t, 'settings.nav.', page)}</div>
                {list.map((hit) => {
                  rowIndex++;
                  const i = rowIndex;
                  const isActive = i === active;
                  const { item } = hit;
                  return (
                    <button
                      key={item.id}
                      id={`settings-search-opt-${item.id}`}
                      role="option"
                      aria-selected={isActive}
                      type="button"
                      ref={(el) => {
                        if (el) itemRefs.current.set(item.id, el);
                        else itemRefs.current.delete(item.id);
                      }}
                      onMouseEnter={() => setActive(i)}
                      // onMouseDown, since the blur handler closes the list before a click.
                      onMouseDown={(e) => {
                        e.preventDefault();
                        pick(hit);
                      }}
                      className={`flex w-full flex-col items-start gap-0.5 px-4 py-2 text-start text-sm
                        transition-colors outline-none ${
                          isActive ? 'bg-carbon-hover text-carbon-text' : 'text-carbon-textSub'
                        }`}
                    >
                      <span className="w-full truncate">{item.name}</span>
                      {/* The card and the prose marker as two spans, with no
                          separator glyph to translate or mirror. */}
                      <span className="flex w-full min-w-0 gap-2 text-[11px] text-carbon-textMuted">
                        {item.tier === 'page' ? (
                          <span className="truncate">{t('settings.search.section')}</span>
                        ) : (
                          item.cardName && <span className="truncate">{item.cardName}</span>
                        )}
                        {hit.inHint && <span className="shrink-0">{t('settings.search.inHint')}</span>}
                      </span>
                    </button>
                  );
                })}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* The full count for screen readers; two keys because the catalogue
          has no plurals. */}
      <span aria-live="polite" className="sr-only">
        {showList ? (hits.length === 1 ? t('settings.search.countOne') : t('settings.search.count', { n: hits.length })) : ''}
      </span>
    </div>
  );
}
