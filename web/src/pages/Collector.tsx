import { useCallback, useEffect, useMemo, useRef, useState, type SVGProps } from 'react';
import { recheckTasks, startTasks, type Task } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useReportListView } from '../lib/listview';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';
import { PageHeader, IconBadge, Button } from '../components/ui';
import { Tabs } from '../components/Tabs';
import {
  TaskListCard,
  groupByPackage,
  useCollapsedPackages,
  type Selection,
} from '../components/TaskList';
import { PackageActions } from '../components/PackageActions';
import { AddLinksForm } from '../components/AddLinksForm';
import { FileDrop, type FileDropHandle } from '../components/FileDrop';
import { FilteredLinks, useFx } from '../components/FilteredLinks';
import { SkippedLinks } from '../components/SkippedLinks';
import {
  COLLECTOR_FILTERS,
  ListMenu,
  cleanupItems,
  matchesQuickFilters,
  offeredQuickFilters,
  targetPackage,
  targetTaskId,
  useCleanup,
  useRemoval,
  type ListContext,
  type MenuTarget,
  type QuickFilterId,
} from '../components/ListToolbar';
import { EMPTY_SEARCH, matchesSearch, SearchField, type SearchQuery } from '../components/SearchField';
import { anchorBelow, anchorFromEvent, useContextMenu, ContextMenu } from '../components/ContextMenu';
import {
  CollectorFacetSidebar,
  EMPTY_FACETS,
  matchesFacets,
  type FacetSelection,
} from '../components/CollectorFacets';
import { CollectorStats } from '../components/CollectorStats';
import { useScriptMenu } from '../components/ScriptActions';
import { FirstTouchHint } from '../components/FirstTouchHint';
import { usePublishCommandPageContext } from '../lib/commands/pageContext';
import { IconCheck, IconClock, IconClose, IconPlay, IconRetry, IconSearch, IconTrash, IconWarning } from '../lib/icons';

// One glyph lib/icons.tsx has no equivalent for yet, needed only by the
// selection-mode half of the action row below. Follows that file's own
// house style (solid fill, never a stroked outline) rather than
// ListToolbar.tsx's local stroke-based twin of the same idea (its own
// unexported IconTrashFiles), which this file cannot import without
// editing ListToolbar.tsx - not this file's lane this round.

/** "Remove and delete files": IconTrash's own body with two slits carved
 *  through it (fillRule="evenodd", the same technique IconWarning's "!" uses
 *  in lib/icons.tsx) - a trash can whose contents are visibly gone, distinct
 *  from the plain IconTrash beside it so the two danger badges are told
 *  apart without hovering for a tooltip. */
const IconTrashFiles = (p: SVGProps<SVGSVGElement>) => (
  <svg width={22} height={22} viewBox="0 0 20 20" fill="currentColor" className="shrink-0" aria-hidden {...p}>
    <rect x="8" y="2" width="4" height="2" rx="1" />
    <rect x="3.5" y="4.5" width="13" height="2.2" rx="1.1" />
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M5.3 7.5h9.4l-.9 9.1a1.5 1.5 0 0 1-1.5 1.4H7.7a1.5 1.5 0 0 1-1.5-1.4L5.3 7.5Z
         M7 9.6h6v1.3H7Z
         M7 12.4h6v1.3H7Z"
    />
  </svg>
);

/** COLLECTOR_FILTERS minus the two now rendered as their own square badges
 *  in the action row instead (see the "Nicht prüfbar" / "Ungeprüft"
 *  IconBadges below) — the strip this feeds keeps Online/Offline/Deaktiviert/
 *  Gehalten, the four that stayed text chips. */
const COLLECTOR_BADGE_FILTERS: QuickFilterId[] = COLLECTOR_FILTERS.filter(
  (id) => id !== 'uncheckable' && id !== 'unchecked',
);

export function Collector() {
  const { t } = useT();
  const fx = useFx();
  const tasks = useTasks('');
  const { toast } = useToast();
  // pkg stays lifted here rather than moving into AddLinksForm: the
  // container-drop zone below wants the same package name a link pasted at
  // the same time would get, and it is not part of the form's own lane.
  const [pkg, setPkg] = useState('');
  // Reaches into FileDrop from AddLinksForm's own button row (jdp: "Dropzone
  // mit Dateiwählen button neben dem Zum-Sammler-Button") - see
  // FileDropHandle's own doc comment for why the button moved rather than
  // the whole drop target, and now also for the drop-target's own file
  // handling (jdp, 2026-08-24: "können wir diesen text und card nicht
  // entfernen" - the paste box's own drop target hands files here too).
  const fileDrop = useRef<FileDropHandle>(null);
  const [search, setSearch] = useState<SearchQuery>(EMPTY_SEARCH);
  // The search field's own open/closed state (jdp, 2026-08-24: "das
  // suchfeld soll auch als quadratischer badge neben die andren vier
  // badges. bei klick soll das suchfeld ausklappen") - the field itself
  // stays mounted only while this is true, as a small popover anchored
  // under searchRef below rather than growing inline (jdp, 2026-08-25:
  // "die suche soll einfach nach unten aufklappen und über allem
  // hoovern").
  const [searchOpen, setSearchOpen] = useState(false);
  const searchRef = useRef<HTMLDivElement>(null);
  const [filters, setFilters] = useState<Set<QuickFilterId>>(() => new Set());
  // The facet groups the collector's own sidebar exposes (host, file type,
  // package) — see components/CollectorFacets.tsx for why availability is not a
  // fourth one: it is already the quick filters above.
  const [facets, setFacets] = useState<FacetSelection>(EMPTY_FACETS);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const menu = useContextMenu();
  // The cleanup menu's own anchor, separate from `menu` above: this is the
  // badge row's own dropdown (jdp, 2026-08-24: "Aufräumen ... als badge"),
  // not the row/package/list context menu ListMenu below already owns.
  const cleanupMenu = useContextMenu();
  const [target, setTarget] = useState<MenuTarget>({ kind: 'selection' });
  const folds = useCollapsedPackages('collector');

  // Everything this instance holds, not only what is staged: a removal weighs
  // the bytes already on disk, and those belong to rows this page never shows.
  const all = useMemo(() => Object.values(tasks), [tasks]);
  // position, not createdAt: position is the field drag-to-reorder (and the
  // menu's own top/up/down/bottom) actually writes (App.ReorderBand,
  // renumberBand), and applySort below is a no-op in the default queue-order
  // view (sort === null, exactly when dnd is enabled) - createdAt here meant
  // every reorder kept broadcasting a real, saved position change that never
  // once became visible, because nothing downstream ever read it back.
  const collected = useMemo(
    () =>
      all
        .filter((x) => x.status === 'collected' && !x.skipped)
        .sort((a, b) => a.position - b.position),
    [all],
  );
  // The holding area, kept out of the list above on purpose: a link the filter
  // refused is still recorded and still restorable, but a filter that is working
  // must not make this page look like a collector full of junk.
  const held = useMemo(
    () => all.filter((x) => x.skipped).sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1)),
    [all],
  );
  const filtered = useMemo(
    () => collected.filter((x) => matchesQuickFilters(x, filters) && matchesSearch(x, search) && matchesFacets(x, facets)),
    [collected, filters, search, facets],
  );
  const groups = useMemo(() => groupByPackage(filtered), [filtered]);

  // "Nicht prüfbar" / "Ungeprüft" moved out of ListToolbar's own text-chip
  // filter strip and into this row of square badges (jdp, 2026-08-25: "die
  // 'nicht prüfbar' und 'ungeprüft' buttons sollen auch in die zeile der
  // ganzen quadratischen badges"), toggling the SAME `filters` set the strip
  // itself reads — COLLECTOR_BADGE_FILTERS below is what stays in the strip.
  const uncheckableCount = useMemo(() => collected.filter((x) => x.online === 'uncheckable').length, [collected]);
  const uncheckedCount = useMemo(() => collected.filter((x) => !x.online).length, [collected]);
  // The rest of the strip (Online/Offline/Deaktiviert/Gehalten), merged
  // into the badge row below (jdp, 2026-08-25: "können wir die nicht in
  // der zeile der quadratischen icons platzieren"). offeredQuickFilters is
  // ListToolbar's own logic, reused rather than copied so the two inline
  // renderings of "which chip, which count" can never drift apart.
  const offeredFilters = useMemo(
    () => offeredQuickFilters(COLLECTOR_BADGE_FILTERS, collected, filters),
    [collected, filters],
  );
  // filtered.length !== collected.length rather than ListToolbar's own
  // "active.size > 0 || search text" reading: this page also narrows by
  // the sidebar's own facets (host/type/package), which that simpler
  // reading knows nothing about - a facet-only narrowing would otherwise
  // show every row without ever explaining why fewer are visible.
  const narrowed = filtered.length !== collected.length;

  function toggleFilter(id: QuickFilterId): void {
    const next = new Set(filters);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setFilters(next);
  }

  // Closes the search popover on an outside click or Escape - the same
  // pattern LanguagePicker.tsx's own dropdown already uses.
  useEffect(() => {
    if (!searchOpen) return;
    const onClick = (e: MouseEvent) => {
      if (searchRef.current && !searchRef.current.contains(e.target as Node)) setSearchOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setSearchOpen(false);
    document.addEventListener('mousedown', onClick);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onClick);
      document.removeEventListener('keydown', onKey);
    };
  }, [searchOpen]);

  // Drop selections that have left the collector.
  useEffect(() => {
    setSelected((prev) => {
      const live = new Set(collected.map((x) => x.id));
      const next = new Set([...prev].filter((id) => live.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [collected]);

  const clearSelection = useCallback(() => setSelected(new Set()), []);
  const removal = useRemoval({ all, selected, base: '/api', onDone: clearSelection });
  // This page's own useCleanup() instance, now driven by the badge row below
  // instead of ListActionBar's text-button trigger (which Downloads.tsx keeps
  // unchanged - this page stopped using that shared component so its own
  // restyle never touches Downloads' look). Loaded proactively, the same
  // reason ListMenu loads its own copy on mount rather than waiting for a
  // click: a command visible in a palette that has to wait on a request
  // before it can say whether "clear finished" applies is a command that
  // answers late.
  const cleanup = useCleanup(all);
  useEffect(() => {
    void cleanup.load().catch(() => {
      /* the badge's own menu already reports this when opened; a command does not nag twice */
    });
  }, [cleanup.load]);

  // The shell's strip cannot see this page's search box or its quick filters, so
  // it is told which rows survived them — see lib/listview.ts.
  useReportListView(filtered, selected);
  // The command surface's own bridge (lib/commands/pageContext.ts): the exact
  // setSelected/removal/cleanup this page already holds, so
  // lib/commands/collector.ts's selectAll/removeSelected/chooseFile call
  // the identical functions the toolbar's own buttons call.
  usePublishCommandPageContext(
    useMemo(
      () => ({
        setSelection: setSelected,
        removal,
        cleanup,
        openFilePicker: () => fileDrop.current?.openPicker(),
      }),
      [removal, cleanup],
    ),
  );
  // Resolved once here rather than inside CollectorStats: `collected`, not
  // `all`, because the stats strip is about what is staged, the same scope
  // every other figure on this page already uses.
  const selectedTasks = useMemo(() => collected.filter((x) => selected.has(x.id)), [collected, selected]);
  // Wave 11B: the LinkGrabber half of the census's "both table context
  // menus" - a staged link is not downloaded yet, but a script that only
  // ever ran once a file already existed would miss exactly the "inspect and
  // adjust before it starts" use case Packagizer-style automation is for.
  const scriptGroups = useScriptMenu({ chosen: selectedTasks, base: '/api' });

  const selection: Selection = {
    ids: selected,
    toggle: (id) =>
      setSelected((s) => {
        const n = new Set(s);
        if (n.has(id)) n.delete(id);
        else n.add(id);
        return n;
      }),
    set: setSelected,
  };

  // Handed to AddLinksForm as onStaged: the form owns the request and its own
  // fields, this page keeps owning what the result is worth telling the user.
  // submittedCount is how many URL-shaped lines the box held, which the form
  // is in the only position to count since the text itself lives there now.
  function handleStaged(created: Task[], submittedCount: number) {
    if (!created.length) {
      toast(t('collector.toastNone'), 'fail');
      return;
    }
    // A link the filter held is a task, so it comes back in `created` — but it
    // was not staged, and counting it as staged is the sentence that sends
    // somebody looking for it in a list it is deliberately not in.
    const heldNow = created.filter((x) => x.skipped).length;
    const staged = created.length - heldNow;
    const skipped = Math.max(0, submittedCount - created.length);
    if (heldNow) {
      toast(
        staged
          ? fx('collector.filtered.toastHeld', { n: staged, held: heldNow })
          : fx('collector.filtered.toastAllHeld', { held: heldNow }),
        staged ? 'ok' : 'info',
      );
      return;
    }
    toast(
      skipped
        ? t('collector.toastSkipped', { n: staged, skipped })
        : t('collector.toastStaged', { n: staged }),
      'ok',
    );
  }

  /**
   * One toast, telling the truth about what the start did.
   *
   * It used to announce "n started" from the number of rows selected, before
   * the request had answered - so a start that moved nothing still said it had
   * moved everything. The route now reports what it did, and the three ways it
   * can come to nothing each get their own sentence: a schedule holding the
   * queue, a link filter holding the links, or nothing matching at all.
   */
  const runStart = async (ids: string[]) => {
    try {
      const r = await startTasks(ids);
      if (r.blocked) return toast(t('collector.toastStartBlocked'), 'fail');
      if (r.started === 0 && r.skipped > 0) return toast(t('collector.toastStartSkipped', { n: r.skipped }), 'fail');
      if (r.started === 0) return;
      toast(t('collector.toastStarted', { n: r.started }), 'info');
    } catch {
      toast(t('list.optionsFailed'), 'fail');
    }
  };
  const startSelected = () => {
    if (!selected.size) return;
    void runStart([...selected]);
  };
  const startAll = () => void runStart([]);

  async function openCleanup(el: HTMLButtonElement | null): Promise<void> {
    try {
      await cleanup.load();
      cleanupMenu.openAt(anchorBelow(el));
    } catch {
      toast(t('list.optionsFailed'), 'fail');
    }
  }

  // The same three readings as the download list: a link, a package header, or
  // the empty space around them.
  function onContextMenu(e: React.MouseEvent): void {
    // The column header opens its own menu on right-click; when it has, this one
    // stays out of the way instead of stacking a second menu on top. The native
    // event is the live one — the synthetic event's flag was captured before any
    // handler ran.
    if (e.nativeEvent.defaultPrevented) return;
    const id = targetTaskId(e);
    const pkg = id === null ? targetPackage(e) : null;
    if (id) {
      if (!selected.has(id)) setSelected(new Set([id]));
      setTarget({ kind: 'selection' });
    } else if (pkg !== null) {
      const ids = filtered.filter((x) => (x.package || '') === pkg).map((x) => x.id);
      if (!(ids.length > 0 && ids.every((x) => selected.has(x)))) setSelected(new Set(ids));
      setTarget({ kind: 'package', name: pkg });
    } else {
      setTarget({ kind: 'list' });
    }
    e.preventDefault();
    menu.openAt(anchorFromEvent(e));
  }

  const listContext: ListContext = {
    packages: groups.map(([name]) => name),
    collapsed: folds.collapsed,
    onCollapse: folds.collapse,
    onExpand: folds.expand,
    onSelectAll: () => setSelected(new Set(filtered.map((x) => x.id))),
    onSelectNone: clearSelection,
    // The collector is always this instance's own.
    local: true,
  };

  const allChosen = filtered.length > 0 && filtered.every((x) => selected.has(x.id));
  // The selection-mode half of the action row below needs these twice each
  // (the remove badge and, when it applies, the remove-with-files badge) -
  // the same ids/onDisk SelectionStrip used to derive for itself.
  const selectedIds = selectedTasks.map((x) => x.id);
  const selectedOnDisk = selectedTasks.some((x) => x.loaded > 0);

  return (
    // flex-1, not h-full: app/Layout.tsx's own page wrapper is a flex
    // column now specifically so a page can grow into it this way - see
    // that file's own doc comment on why flex-grow, not a percentage
    // height, is what reliably fills it.
    <div className="flex min-h-0 flex-1 flex-col gap-6">
      <div className="shrink-0">
        <PageHeader title={t('collector.title')} />
      </div>

      <div className="shrink-0">
        <FirstTouchHint id="collector" />
      </div>

      {/* Three equal-height columns (jdp, 2026-08-24: "erst soll die
          linksammler-card kommen, rechts daneben die statistik-card, und
          rechts davon die filter card. alle drei card sollen immer gleich
          hoch sein. wenn eine wächst sollen die anderen mitwachsen") - a
          plain flex row leaves align-items at its default `stretch`, which is
          exactly "all three grow together": no explicit height math, no
          `items-start` override fighting it. AddLinksForm is the one hero
          (flex-1), the other two size to their own content but still match
          whichever of the three is tallest. */}
      <div className="flex min-w-0 shrink-0 flex-col gap-4 lg:flex-row">
        <div className="min-w-0 flex-1">
          <AddLinksForm
            pkg={pkg}
            onPkgChange={setPkg}
            onStaged={handleStaged}
            onChooseFile={() => fileDrop.current?.openPicker()}
            onFilesDropped={(files) => fileDrop.current?.handleFiles(files)}
            // FileDrop's own visible output now lives inside this same card
            // (jdp, 2026-08-26: "der Fortschrittsbalken soll im
            // Linksammlerfenster angezeigt werden") - it still needs to stay
            // mounted unconditionally for its own ref API regardless of
            // where its OUTPUT renders (AddLinksForm's folder-icon badge
            // opens the picker through it), which is why the component
            // itself is instantiated once, here, rather than twice.
            footer={<FileDrop ref={fileDrop} pkg={pkg} />}
          />
        </div>
        {collected.length > 0 && <CollectorStats all={collected} visible={filtered} selected={selectedTasks} />}
        {collected.length > 0 && <CollectorFacetSidebar tasks={collected} selection={facets} onChange={setFacets} />}
      </div>

      {/* The quick-filter toolbar, the one action-badge row (search, the
          selection actions and the four page-level actions all merged into
          it now - see that row's own doc comment) and the list itself, all
          wrapped in ONE inner flex-col with a tighter gap-3 (jdp,
          2026-08-24, second round: "zwischen suchfeld und hauptfenster ist
          immer noch ein großer anbstand" - hiding the empty selection
          strip's own wrapper already removed one PHANTOM gap that round, but
          the outer page's gap-6 still put a full 24px seam before AND after
          the badge row on top of the row's own height, reading as one big
          gap even with nothing phantom left in it). This "list-management
          cluster" reads as one connected unit, gap-3 between its own parts;
          the hero row above keeps the page's normal gap-6 - it is a
          genuinely separate section, this one is not.

          The rest of the intake trace (a held link, the skipped-links
          notice) still sits here rather than in the hero card - FileDrop's
          own visible output moved into the Linksammler card itself
          (jdp, 2026-08-26, see AddLinksForm's own `footer` prop), but a
          held/skipped link is not something FileDrop produced, so it stays
          exactly where it was. Both are normally rendered as literally
          nothing (no held link, no skip in this session), which is why
          they cost gap-3 rather than gap-6 here - see this cluster's own
          reasoning above. SkippedLinks floats as its own fixed card
          regardless of where it is mounted (see its own doc comment) -
          it does not depend on living in any particular position in the
          tree, only on living somewhere. */}
      <div className="flex min-h-0 flex-1 flex-col gap-3">
        {/* No wrapper div of their own any more (jdp, 2026-08-26: "Alle
            quadratischen badges weiter runter, näher an die card" - the
            common case, neither a held nor a skipped link in this
            session, had both components returning null while their OWN
            now-empty wrapper still counted as a real flex child, costing
            this cluster's own gap-3 on both sides of nothing. Rendered as
            direct children of this flex-col instead - a null child
            contributes no element at all, so the gap simply does not
            apply when there is nothing to show, and collapses to the
            identical gap-3 between them when there is.) */}
        <FilteredLinks held={held} />
        <SkippedLinks />

        {/* Search, the quick-filter strip, the selection actions and the
            four page-level actions all share ONE row now (bug #35
            continued, jdp 2026-08-24: "suchfeld badge soll das erste badge
            von links sein und wenn man drauf klickt nach links aufklappen,
            nicht die suchleiste in eine neue zeile packen"; same round, a
            second item: "wenn ich ein Linkpaket auswähle kommen oben
            buttons wie zb Auswahlaufheben ... die sind nicht als badges
            erkennbar und die sollen in der gleichen zeile wie die
            quadratischen badges erscheinen, nicht in einer neuen Zeile").
            Reversed 2026-08-25 (jdp: "die suche bitte auch wieder rechts zu
            den anderen dazumachen. es soll aber der linkeste badge sein und
            das suchfeld nach links aufklappen") - search is now the FIRST
            badge of the right-hugging cluster instead of its own
            left-aligned element. Reversed again the SAME day (jdp: "die
            suche soll einfach nach unten aufklappen und über allem
            hoovern"): the field no longer trades places with the leading
            spacer to grow INLINE (which pushed every badge after it
            sideways and widened the whole row) - it is now a small
            absolutely-positioned popover anchored under the search badge
            itself (position: relative on that one badge's own wrapper,
            the field position: absolute below it, same click-outside +
            Escape close LanguagePicker.tsx's own dropdown already uses),
            so opening it never moves anything else in this row at all.
            Selection replaces the fixed set of badges after search with
            its own, on the same "one connected row" logic. Reuses the
            exact same allChosen/cleanup/checkAll/startAll logic
            ListActionBar used to run for this page, and the exact same
            removeNow/askWithFiles/onMore wiring SelectionStrip used to run -
            only the trigger's shape changed, not what it does - and stays
            local to this file since Downloads.tsx keeps ListActionBar's and
            SelectionStrip's own text-button look unchanged.

            The quick-filter strip (Online/Offline/Deaktiviert/Gehalten
            chips, the "Alles anzeigen" clear button, the "N von M
            angezeigt" readout) moved into this same row too (jdp,
            2026-08-25: "können wir die nicht in der zeile der quadratischen
            icons platzieren damit wie den abstand zwischen der 'neue Links'
            und der 'Sammlung' card verringern können?") - it used to be
            ListToolbar's own separate `shrink-0` row above this one,
            costing a full extra flex gap even when nothing in it had
            anything to show. offeredQuickFilters is ListToolbar's own
            counting/visibility logic, exported so this inline copy of its
            markup never drifts from what ListToolbar itself still renders
            for Downloads.tsx. flex-wrap here (this row alone, not the
            layout above it) is new: a variable number of filter chips can
            now share the row with the action badges, and wrapping is safer
            than a horizontal scrollbar for a row this narrow a window can
            make. */}
        <div className="flex flex-wrap shrink-0 items-center gap-2" role="group" aria-label={t('list.actions')}>
          <span className="flex-1" />

          {/* Filters, not actions — visible regardless of selection, the
              same reasoning the search badge beside them already follows,
              rather than living only in one of the two clusters below that
              swap out with a selection. Left of the search badge (jdp,
              2026-08-26: "die sollen links vom suchbadge sein nicht mitten
              drin") - the search badge is the row's own fixed anchor point
              (its popover opens from it every time), so a variable number
              of filter chips sits on the side that does not push it around
              as chips appear and disappear. */}
          <IconBadge
            hue={0}
            active={filters.has('uncheckable')}
            icon={<IconWarning width={16} height={16} />}
            title={t('filter.uncheckable')}
            aria-label={t('filter.uncheckable')}
            disabled={uncheckableCount === 0 && !filters.has('uncheckable')}
            onClick={() => toggleFilter('uncheckable')}
          />
          <IconBadge
            hue={1}
            active={filters.has('unchecked')}
            icon={<IconClock width={16} height={16} />}
            title={t('filter.unchecked')}
            aria-label={t('filter.unchecked')}
            disabled={uncheckedCount === 0 && !filters.has('unchecked')}
            onClick={() => toggleFilter('unchecked')}
          />

          {offeredFilters.length > 0 && (
            <Tabs
              select="many"
              size="sm"
              label={t('filter.label')}
              active={filters}
              onSelect={(id) => toggleFilter(id as QuickFilterId)}
              items={offeredFilters.map(({ f, n }) => ({ id: f.id, label: t(f.label), badge: n }))}
              after={
                filters.size > 0 && (
                  <Button kind="ghost" className="px-2 py-1 text-xs" onClick={() => setFilters(new Set())}>
                    {t('filter.clear')}
                  </Button>
                )
              }
            />
          )}
          {narrowed && (
            <span className="glim-num text-xs text-carbon-textMuted">
              {t('search.shown', { n: filtered.length, total: collected.length })}
            </span>
          )}

          {/* The selection count, next to the filter chips rather than on
              the far side of every action badge (jdp, 2026-08-26,
              screenshot: "die badges 'ausgewählt', 'online',
              'ausgeschaltet' etc bitte links von allen quadratischen
              badges anzeigen") - every informational readout now sits
              together before the first actual action trigger, search
              included. The clear-selection badge stays paired with the
              count it clears rather than moving into the action cluster
              below - it acts on this text, not on the selection's own
              contents the way start/remove do. */}
          {selected.size > 0 && (
            <>
              <span className="glim-num text-sm text-carbon-textSub">
                {selected.size} {t('select.count')}
              </span>
              <IconBadge
                hue={1}
                icon={<IconClose width={16} height={16} />}
                title={t('select.none')}
                aria-label={t('select.none')}
                onClick={clearSelection}
              />
            </>
          )}

          <div ref={searchRef} className="relative">
            <IconBadge
              hue={0}
              icon={<IconSearch width={16} height={16} />}
              title={t('collector.searchToggle')}
              aria-label={t('collector.searchToggle')}
              onClick={() => setSearchOpen((v) => !v)}
            />
            {searchOpen && (
              <div
                // w-96 (jdp, 2026-08-26: "suchfeld soll breiter sein"), up
                // from w-72 - end-0 keeps it hugging the badge's own trailing
                // edge regardless of width, so widening it only ever grows
                // the field towards the row's own leading side.
                className="absolute end-0 top-full z-20 mt-2 w-96 rounded-[var(--radius-control)]
                  bg-carbon-surface p-2 shadow-[var(--elevation)]"
              >
                <SearchField value={search} onChange={setSearch} className="w-full" />
              </div>
            )}
          </div>

          {selected.size > 0 ? (
            <>
              <PackageActions
                tasks={collected}
                selected={selected}
                base="/api"
                onDone={() => toast(t('task.applied'), 'ok')}
              />
              {/* Secondary, not primary: the page's one accent button is
                  "Add to collector" in the hero, and a second would make
                  neither read as the thing to do next - unchanged from
                  before, just a square badge instead of a labelled button
                  now. */}
              <IconBadge
                hue={2}
                icon={<IconPlay width={16} height={16} />}
                title={t('collector.startSelected')}
                aria-label={t('collector.startSelected')}
                onClick={startSelected}
              />
              {/* The "Mehr" trigger is gone (jdp, 2026-08-26: "der badge
                  'mehr' entfernen") - it opened the identical menu a
                  right-click on the selection already does, so removing it
                  loses no capability, only a redundant second way in. */}
              <IconBadge
                hue={4}
                icon={<IconTrash width={16} height={16} />}
                title={t('task.remove')}
                aria-label={t('task.remove')}
                onClick={() => void removal.removeNow(selectedIds)}
              />
              {/* Only when there is something on disk to erase. */}
              {selectedOnDisk && (
                <IconBadge
                  hue={5}
                  icon={<IconTrashFiles width={16} height={16} />}
                  title={t('task.removeWithFiles')}
                  aria-label={t('task.removeWithFiles')}
                  onClick={() => removal.askWithFiles(selectedIds)}
                />
              )}
              {/* The (i) keyboard-shortcut bubble is gone too (jdp,
                  2026-08-26: "i infobubble entfernen") - the badges it was
                  explaining are still self-explanatory via their own
                  hover tooltips. */}
            </>
          ) : (
            <>
              <IconBadge
                hue={1}
                icon={<IconCheck width={16} height={16} />}
                title={allChosen ? t('select.none') : t('select.all')}
                aria-label={allChosen ? t('select.none') : t('select.all')}
                disabled={filtered.length === 0}
                onClick={() => setSelected(allChosen ? new Set() : new Set(filtered.map((x) => x.id)))}
              />
              <IconBadge
                hue={2}
                icon={<IconTrash width={16} height={16} />}
                title={t('cleanup.menu')}
                aria-label={t('cleanup.menu')}
                onClick={(e) => void openCleanup(e.currentTarget)}
              />
              <IconBadge
                hue={3}
                icon={<IconRetry width={16} height={16} />}
                title={t('collector.checkAll')}
                aria-label={t('collector.checkAll')}
                disabled={collected.length === 0}
                onClick={() => {
                  // An empty id list means every staged link on this route —
                  // deliberately unlike the bulk routes, where empty is
                  // refused outright rather than read as "all".
                  recheckTasks([]);
                  toast(t('task.recheck'), 'info');
                }}
              />
              <IconBadge
                hue={4}
                icon={<IconPlay width={16} height={16} />}
                title={t('collector.startAll')}
                aria-label={t('collector.startAll')}
                disabled={collected.length === 0}
                onClick={startAll}
              />
            </>
          )}
        </div>

        {/* jdp, 2026-08-24: "Das hauptlinkfenster soll immer die ganze
            fensterbreite einnehmen und immer bis ganz nach unten im fenster
            gehen. egal wie viele links drinn sind." - min-h-0 + flex-1 here
            makes the list the one scrolling region: everything above it in
            THIS inner cluster keeps its natural height (shrink-0), this
            section absorbs whatever space is left and never less than
            that, and the row list inside scrolls on its own rather than
            growing the whole page. */}
        <div className="flex min-h-0 flex-1 flex-col" onContextMenu={onContextMenu}>
        {collected.length === 0 ? (
          <div className="glim-card flex flex-1 items-center justify-center p-12 text-center text-sm text-carbon-textMuted">
            {t('collector.empty')}
          </div>
        ) : filtered.length === 0 ? (
          <div className="glim-card flex flex-1 items-center justify-center p-12 text-center text-sm text-carbon-textMuted">
            {t('downloads.noMatch')}
          </div>
        ) : (
          // flex flex-col here, not just min-h-0/flex-1: a percentage height
          // on TaskListCard's own root (h-full) does not reliably resolve
          // through a plain block box whose OWN height came from flex-grow
          // plus overflow-auto - a genuine browser quirk (confirmed live:
          // the wrapper measured a real 738px, the h-full child inside it
          // still fell back to its own content height). Making this wrapper
          // a flex container too, and TaskListCard's own root a flex-grow
          // child (flex-1, not h-full) below, sidesteps it entirely - flex
          // distributes space in one pass, with none of percentage-height's
          // resolve-through-an-overflow-box ambiguity.
          // pt-3, down from pt-4 (jdp, 2026-08-26, [81]: "immer noch zu weit
          // weg" - the gap was still too big after the first pass at this
          // same complaint). A first attempt at THIS pass removed this
          // padding entirely, on the theory that TaskListCard's own internal
          // `px-4 pt-4` (right before its SectionTitle) already covered the
          // badge's own `-top-[11px]` offset on its own - live-measured
          // straight after and proven wrong: the badge sat at 494 against
          // this wrapper's own clip boundary at 505, an 11px overshoot
          // identical to the ORIGINAL bug report, meaning the card's own
          // padding does not protect against THIS wrapper's own
          // overflow-y-auto at all. pt-3 is not a fresh guess - it is the
          // exact value the original fix already measured a 1px margin at,
          // tighter than pt-4's 5px and still positive. Verified live rather
          // than assumed, again, after this correction: badge fully clear
          // of the boundary at pt-3, gap visibly tighter than pt-4 was.
          <div className="flex min-h-0 flex-1 flex-col overflow-y-auto pt-3">
            <TaskListCard
              groups={groups}
              base="/api"
              selection={selection}
              profile="collector"
              title={t('collector.listTitle')}
              hue={3}
            />
          </div>
        )}
        </div>
      </div>

      {cleanupMenu.anchor && cleanup.classes && (
        <ContextMenu
          anchor={cleanupMenu.anchor}
          label={t('cleanup.menuLabel')}
          onClose={cleanupMenu.close}
          groups={[{ id: 'cleanup', items: cleanupItems(cleanup.classes, t, (cls) => void cleanup.preview(cls)) }]}
        />
      )}

      <ListMenu
        anchor={menu.anchor}
        onClose={menu.close}
        all={all}
        selected={selected}
        base="/api"
        removal={removal}
        target={target}
        list={listContext}
        extraGroups={scriptGroups}
      />
      {removal.dialog}
      {cleanup.dialog}
    </div>
  );
}
