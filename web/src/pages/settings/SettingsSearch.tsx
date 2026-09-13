// One box over all of Settings.
//
// It searches the section names, the card titles, the caption beside every
// control and the text behind every (i) - and then it JUMPS. It never filters
// the page: hiding non-matching cards would scramble the hue sequence every page
// hands its cards by position (DownloadsSettings passes hue 0..10, and its own
// comment says a jumbled badge sequence reads as a bug), and it would contradict
// the rail, which still says the page is whole.
//
// It also does not search settings VALUES. That box already exists, on the
// Advanced page, over the raw dotted paths and the JSON under them - a different
// question with a different answer, and worth knowing about: a query that finds
// nothing here quite often finds something there.
//
// Three tiers of result: a section, a card, a row. A row shows its caption
// first and the card it is on underneath, plus a small marker when the query
// matched only the explanation - without it, a hit on a caption with none of the
// typed letters in it reads as a bug rather than as a match on the sentence
// behind the (i).
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useT, type TranslationKey } from '../../lib/i18n';
import { en } from '../../lib/locales/en';
import { IconClose, IconSearch } from '../../lib/icons';
import { fold, scoreFolded, scoreProse } from '../../lib/rank';
import { useToast } from '../../lib/toast';
import { useUIState } from '../../lib/uistate';
import { InfoBubble } from '../../components/ui';
// A cycle on purpose, and a safe one: Settings.tsx mounts this component and
// this component reads its page ordering. `orderPages` is a function
// DECLARATION, so its binding is hoisted and initialised before either module
// body runs, and it is only ever called at render time - long after both are
// evaluated. The alternative was a second copy of it here, which is the one
// thing the ordering must not have: it answers "what happens to a page the
// stored drag order has never heard of", and two answers to that drift.
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
import { useStickyReveal } from './stickyReveal';
import { label as pageLabel } from './tx';

/** How long to wait after the last keystroke before matching. The same 150ms,
 *  for the same reason, that Advanced.tsx's own filter already uses: without it
 *  the box stutters on exactly the keys somebody is trying to search with. */
const DEBOUNCE_MS = 150;

/** How many results are drawn. The true total is still announced, so a query
 *  that matches ninety things says so rather than looking like it matched forty. */
const RENDER_CAP = 40;

/**
 * Result tiers. Only a TIEBREAK, and the smallest term in the rank below - it
 * separates a section from a card from a row when the two matched equally well,
 * and nothing more.
 *
 * It was the first term for a while, and that was wrong in a way only the real
 * catalogue shows: the top five results for "speed" were five cards whose
 * EXPLANATIONS happened to contain the word, ranked above the row actually
 * called "Speed limit". Whether the query matched a name or a sentence matters
 * far more than which of the three kinds of thing carries that name.
 */
const TIER = { page: 0, card: 1, row: 2 } as const;

/**
 * Added to a match found in explanation text, so that EVERY name match sorts
 * above EVERY prose match. Comfortably above the largest thing the rest of the
 * rank can add (a subsequence hit scores 1000, multiplied by 100 below).
 */
const PROSE_PENALTY = 1_000_000;

interface Item {
  /** Stable within one build of the index; used as the React key and the
   *  aria-activedescendant target. */
  id: string;
  tier: keyof typeof TIER;
  page: string;
  /** The card's SectionTitle key, for everything but a section result. */
  title?: TranslationKey;
  /** The row's caption key. Absent for a card result AND for a card's `also`
   *  text, which has no caption of its own to land on. */
  label?: TranslationKey;
  /** Line one of the result. */
  name: string;
  /** The card's own name, for line two of a row result. */
  cardName?: string;
  /** The displayed name, folded. Matched by name AND by abbreviation. */
  foldedName: string;
  /**
   * Everything explanatory this entry carries, folded and run together: the (i)
   * text, and for a card its `body` prose as well. Matched by SUBSTRING only -
   * see scoreProse, and see what happens without that rule.
   */
  foldedProse: string;
}

/**
 * Resolve a key to text, defensively.
 *
 * lib/i18n.tsx's `t` is `dict[key] ?? en[key]` with no final fallback, so a key
 * that never landed in en.ts returns undefined despite its `string` type - and
 * the first .toLowerCase() in the matcher below would throw and blank the whole
 * settings page. The check script refuses to let such a key into the index, and
 * this is the belt to that pair of braces: an unknown key contributes nothing to
 * the search instead of taking the page down with it.
 */
function text(t: (k: TranslationKey) => string, key: TranslationKey | undefined): string {
  if (!key) return '';
  return key in en ? t(key) : '';
}

/**
 * Every searchable string in the index, resolved and folded once.
 *
 * DEPENDS ON `t`, and that is the single most important line in this file. `t`
 * is a useCallback over [dict], and `dict` starts as English and is REPLACED
 * when the chosen language's chunk finishes loading. Built with [] instead, this
 * index would be English on all 41 non-English languages, permanently, and it
 * would be invisible in English testing - which would destroy the one thing this
 * feature gets for free.
 */
function buildItems(t: (k: TranslationKey) => string, pages: FeaturePage[]): Item[] {
  const items: Item[] = [];
  for (const p of pages) {
    // Only what the server sent AND what this build actually draws. A result for
    // a page with no component lands on the registered-but-empty placeholder,
    // which is the exact failure registry.tsx records the connection manager
    // suffering once.
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
      // A card's own hint and its `body` prose are one haystack: both are
      // explanation, both land on the same card, and two entries for them would
      // be the same result twice.
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

      // `also` is a NAME the card carries that is not a row - a status badge, a
      // caption drawn without a Caption, a select named only by its aria-label.
      // Offered as a row-shaped result with no `label`, so picking it lands on
      // the card: there is nothing with a caption to scroll to, and saying
      // otherwise would be the search lying about the reader's own page.
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

  // Both live HERE and are never lifted into SettingsPage. Up there, every
  // keystroke would re-render the rail's 22-item Tabs and the whole mounted
  // sub-page - and the mounted sub-page is Access.tsx (60 KB) or Look.tsx
  // (62 KB).
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);

  const [settled, setSettled] = useState('');
  useEffect(() => {
    const id = window.setTimeout(() => setSettled(query), DEBOUNCE_MS);
    return () => window.clearTimeout(id);
  }, [query]);

  // The rail's own drag order, so the groups below read top to bottom in the
  // order the tiles beside them do. Expect the first paint to use the server's
  // order: useUIState hands out its fallback until the stored document arrives,
  // the same timing Settings.tsx already defends against for the remembered page.
  const [order] = useUIState<string[]>('settingsTabOrder', []);
  const ordered = useMemo(() => orderPages(pages, order), [pages, order]);

  const items = useMemo(() => buildItems(t, ordered), [t, ordered]);

  const hits = useMemo<Hit[]>(() => {
    const q = fold(settled.trim());
    if (!q) return [];
    const out: Hit[] = [];
    for (const item of items) {
      // The name first, with abbreviation matching, because it is short and
      // because "cmdp" finding "Command palette" is worth having. Then the
      // explanation, by substring ONLY: every letter of any query appears
      // somewhere in a 400-character paragraph, in order, so subsequence
      // matching over prose returns the whole catalogue - measured, not feared.
      let s = scoreFolded(item.foldedName, q);
      let inHint = false;
      if (s < 0 && item.foldedProse) {
        s = scoreProse(item.foldedProse, q);
        inHint = s >= 0;
      }
      if (s < 0) continue;
      // Name before prose, then how good the match is, then what kind of thing
      // it is. In that order: a row literally called "Speed limit" has to come
      // before eight cards whose explanations mention speed.
      out.push({ item, rank: (inHint ? PROSE_PENALTY : 0) + s * 100 + TIER[item.tier], inHint });
    }
    out.sort((a, b) => a.rank - b.rank);
    return out;
  }, [items, settled]);

  /**
   * Grouped by page and headed by the page name, the same shape the command
   * palette already groups its own results in.
   *
   * Group order is the group's BEST hit first, and the rail's own drag order
   * only as the tiebreak. Rail order alone would have been simpler and is wrong
   * in one specific way that matters: the first row of the first group is what
   * Enter picks, so a search whose best answer lives on a page somebody dragged
   * to the bottom of their rail would open the wrong thing on the very keystroke
   * people use most. Between two pages that matched equally well, the rail's
   * order is exactly the right answer, which is what it is used for.
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

  /**
   * The rendered rows, flat and in RENDER order.
   *
   * Not `hits.slice()`, which is in rank order: the arrow keys and Enter index
   * into this, and an index into a differently-ordered list would move the
   * highlight down the screen in a sequence that has nothing to do with where
   * the rows are.
   */
  const shown = useMemo(() => groups.flatMap(([, list]) => list), [groups]);

  useEffect(() => {
    setActive((i) => Math.min(i, Math.max(0, shown.length - 1)));
  }, [shown.length]);

  const listId = 'settings-search-list';
  const activeId = shown[active] ? `settings-search-opt-${shown[active].item.id}` : undefined;

  // Keep the highlighted row on screen. Forty results in a 60vh popover means the
  // arrow keys walk it off the bottom within a few presses, and a selection you
  // cannot see is a selection Enter will surprise you with. `block: 'nearest'`
  // moves the list by the least it can, exactly as the command palette does.
  const itemRefs = useRef(new Map<string, HTMLButtonElement>());
  useEffect(() => {
    const hit = shown[active];
    if (hit) itemRefs.current.get(hit.item.id)?.scrollIntoView({ block: 'nearest' });
  }, [active, shown]);

  // The "Search all settings" command, arriving from the palette. It has no way
  // to reach this input, so it leaves a timestamp in jump.ts and this takes it.
  // Runs on MOUNT as well as on change, because the command navigates here and
  // asks in the same breath: by the time this field exists, the asking is
  // already in the past.
  const focusAskedAt = useSearchFocusAskedAt();
  useEffect(() => {
    if (focusAskedAt === 0) return;
    if (Date.now() - focusAskedAt > FOCUS_REQUEST_TTL_MS) {
      clearSearchFocus();
      return;
    }
    clearSearchFocus();
    const raf = requestAnimationFrame(() => inputRef.current?.focus());
    return () => cancelAnimationFrame(raf);
  }, [focusAskedAt]);

  // The other half of the jump: the result was picked and the page was told to
  // navigate; now find the thing and mark it. Keyed on the nonce, so asking for
  // the same row twice really does look twice.
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
        // Landed on the card but the row was not drawn. Say only that, and never
        // why: the card may simply not have finished loading, and a guess at the
        // cause ("the switch above it is off") is a guess the jumper cannot make.
        if (outcome === 'card' && jump.label) {
          toast(t('settings.search.rowHidden', { card: title }), 'info');
        }
        clearJump();
      },
    });
    return cancel;
    // Deliberately keyed on the nonce rather than on `jump`: a fresh object with
    // the same contents is the same request, and a new nonce is a new one.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jumpNonce]);

  const pick = useCallback(
    (hit: Hit) => {
      const { item } = hit;
      setOpen(false);
      if (item.tier !== 'page' && item.title) {
        requestJump({ page: item.page, title: item.title, label: item.label });
      } else {
        // A section result asks for no jump - and has to retire whatever jump is
        // still hunting, or the previous request's retry loop goes on looking and
        // marks something on the page this one just navigated to.
        clearJump();
      }
      navigate(`/settings/${item.page}`);
    },
    [navigate],
  );

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') {
      // Two jobs, one key, in this order. And stopPropagation, because the
      // command palette closes on the same key and a settings search whose
      // Escape leaks upward is one keypress away from closing something nobody
      // meant to close - the same guard SearchField.tsx already puts on its own.
      e.stopPropagation();
      if (open) {
        e.preventDefault();
        setOpen(false);
      } else if (query) {
        e.preventDefault();
        setQuery('');
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
      // Tabbing out of the box means "I am done with it" - the same rule the
      // command palette and the context menu already follow for this key.
      setOpen(false);
    }
  }

  const showList = open && settled.trim() !== '';
  let rowIndex = -1;

  // Off screen while the column is being read downward, back on the way up. The
  // three things that pin it are the three that mean somebody is using it: the
  // list is open, there is a query in the box, or the box has the focus. The
  // last is checked against the live document rather than kept as state, because
  // `open` is already set by onFocus and a second piece of state saying almost
  // the same thing is a second piece of state to get out of step.
  const barRef = useRef<HTMLDivElement>(null);
  const pinned =
    showList || query !== '' || (typeof document !== 'undefined' && document.activeElement === inputRef.current);
  const hidden = useStickyReveal(barRef, pinned);

  return (
    // Sticky, and with its own opaque ground plus negative margins: the column
    // it sits in is `p-6 md:p-8 overflow-y-auto`, so without them cards would
    // slide visibly behind a transparent strip and past the box's own edges. The
    // matching paddings put the input back exactly where it was, which is what
    // keeps it level with the first tile in the rail beside it (Settings.tsx's
    // own note on why the rail and the column share a top padding).
    //
    // NOT in PageHeader. That renders above the rail-plus-column flex, so a field
    // there would push the rail down and stop it running the full window height -
    // the one thing this page's whole layout exists to do.
    //
    // It also gets out of the way. Opaque and sticky together mean every card
    // passes UNDER it, and the card directly under it is the first one on the
    // page - so reading downward hid the one title that says where you are.
    // `glim-autohide` carries the slide and lives with the other motion
    // utilities, which is what makes it vanish under reduced motion without this
    // file knowing anything about that. See stickyReveal.ts for when, and for
    // the three states that pin it in place.
    <div
      ref={barRef}
      className={`glim-autohide sticky top-0 z-20 -mx-6 -mt-6 bg-carbon-background px-6 pb-4 pt-6
        md:-mx-8 md:-mt-8 md:px-8 md:pt-8 ${hidden ? '-translate-y-full' : 'translate-y-0'}`}
      onBlur={(e) => {
        // Closes when focus genuinely leaves the box and its list, not when it
        // moves between the two.
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false);
      }}
    >
      <div
        className="flex items-center gap-1 rounded-[var(--radius-control)] bg-carbon-surface2 pe-1 ps-2.5
          transition-shadow focus-within:shadow-[0_0_0_2px_var(--focus-ring)]"
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
          className="min-w-0 flex-1 bg-transparent py-2 text-sm text-carbon-text
            placeholder:text-carbon-textMuted outline-none [&::-webkit-search-cancel-button]:hidden"
        />
        {query && (
          <button
            type="button"
            aria-label={t('settings.search.clear')}
            title={t('settings.search.clear')}
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
        {/* The explanation lives behind the (i), never as a line of grey prose
            under the box - the same rule every settings row on every page below
            already follows. */}
        <InfoBubble tip={t('settings.search.hint')} className="me-1" />
      </div>

      {/* A child of the sticky bar and sized to it, never a portal. InfoBubble
          portals because it is anchored to something that scrolls; this bar does
          not move, so nothing here has to be re-measured on scroll and the
          column's own overflow cannot clip it. */}
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
              // A real group, not a bare div: these options are not direct
              // children of the listbox, and without the role a screen reader
              // reads forty options with no idea that the eyebrow above them says
              // which section each belongs to.
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
                      // onMouseDown, not onClick: the blur handler above closes
                      // the list, and a click fires after blur.
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
                      {/* The page is already the group's own heading above, so
                          line two says only what that heading does not: which
                          card, and whether the match was in the explanation
                          rather than in the name. Two spans with a gap, never one
                          string with a separator glyph in it - a hand-written
                          "›" is a character nobody translated, and it points the
                          wrong way once the page mirrors under [dir="rtl"]. */}
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

      {/* The true total, for a reader who cannot see the list. Two count keys
          rather than one plural: the catalogue does {var} replacement and nothing
          else, and en.ts already solves the same problem the same way for
          task.file / task.files. */}
      <span aria-live="polite" className="sr-only">
        {showList ? (hits.length === 1 ? t('settings.search.countOne') : t('settings.search.count', { n: hits.length })) : ''}
      </span>
    </div>
  );
}
