import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { recheckTasks, startTasks, type Task } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useReportListView } from '../lib/listview';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';
import { PageHeader, Button, IconBadge, hueStyle } from '../components/ui';
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
  ListToolbar,
  SelectionStrip,
  cleanupItems,
  matchesQuickFilters,
  targetPackage,
  targetTaskId,
  useCleanup,
  useRemoval,
  type ListContext,
  type MenuTarget,
  type QuickFilterId,
} from '../components/ListToolbar';
import { EMPTY_SEARCH, matchesSearch, type SearchQuery } from '../components/SearchField';
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
import { IconCheck, IconPlay, IconSearch, IconTrash } from '../lib/icons';

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
  const collected = useMemo(
    () =>
      all
        .filter((x) => x.status === 'collected' && !x.skipped)
        .sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1)),
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

  const startSelected = () => {
    if (!selected.size) return;
    startTasks([...selected]);
    toast(t('collector.toastStarted', { n: selected.size }), 'info');
  };
  const startAll = () => {
    startTasks([]);
    toast(t('collector.toastStarted', { n: collected.length }), 'info');
  };

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
          />
        </div>
        {collected.length > 0 && <CollectorStats all={collected} visible={filtered} selected={selectedTasks} />}
        {collected.length > 0 && <CollectorFacetSidebar tasks={collected} selection={facets} onChange={setFacets} />}
      </div>

      {/* Intake that is not a paste, and the trace of what the paste dropped.
          Both sit under the hero and above the list: the paste box is why people
          open this page, and nothing may push it off the top. The skipped strip
          in particular has to render when the list is empty — a paste of nothing
          but duplicates stages nothing, and that is the moment it explains most. */}
      <div className="shrink-0 flex flex-col gap-3">
        <FileDrop ref={fileDrop} pkg={pkg} />
        <FilteredLinks held={held} />
        <SkippedLinks />
      </div>

      {collected.length > 0 && (
        <div className="shrink-0">
          <ListToolbar
            search={search}
            onSearch={setSearch}
            filters={COLLECTOR_FILTERS}
            active={filters}
            onActive={setFilters}
            tasks={collected}
            shown={filtered.length}
          />
        </div>
      )}

      {/* Only rendered while something is actually selected - SelectionStrip
          itself already returns null then, but this wrapper div is a direct
          child of the page's own gap-6 flex column, so leaving it mounted
          empty still cost one full gap unit of dead space between the
          search bar and the badges row below (jdp, 2026-08-24: "das
          Linkhauptfenster ist zu weit unten, das soll unter der Suchleiste
          direkt anfangen") - same conditional-wrapper pattern the search
          bar's own ListToolbar block above already uses for the same
          reason. */}
      {selected.size > 0 && (
        <div className="shrink-0">
          <SelectionStrip
            all={collected}
            selected={selected}
            onSelected={setSelected}
            removal={removal}
            onMore={(at) => {
              setTarget({ kind: 'selection' });
              menu.openAt(at);
            }}
          >
            <PackageActions
              tasks={collected}
              selected={selected}
              base="/api"
              onDone={() => toast(t('task.applied'), 'ok')}
            />
            {/* Secondary, not primary: the page's one accent button is "Add to
                collector" in the hero, and a second would make neither read as the
                thing to do next. */}
            <Button
              kind="secondary"
              className="px-2.5 text-xs"
              icon={<IconPlay width={15} height={15} />}
              onClick={startSelected}
            >
              {t('collector.startSelected')}
            </Button>
          </SelectionStrip>
        </div>
      )}

      {/* jdp, 2026-08-24: "Alle buttons sollen oberhalb des fensters
          platziert sein: Alle auswählen, Aufräumen, alle prüfen, alle
          starten -> alle in einer zeile ganz rechts als badges (inkl.
          farbmodi), der Filter button kann weg" - CollectorFacetsToggle
          (the former "Filter" entry point) is gone entirely now that the
          facets card above is always shown, not toggled. Reuses the exact
          same allChosen/cleanup/checkAll/startAll logic ListActionBar used
          to run for this page - only the trigger's shape changed, not what
          it does - and stays local to this file since Downloads.tsx keeps
          ListActionBar's own text-button look unchanged. */}
      <div className="flex shrink-0 items-center justify-end gap-2" role="group" aria-label={t('list.actions')}>
        <IconBadge
          icon={<IconCheck width={16} height={16} />}
          className="glim-hue glim-hue-icon"
          style={hueStyle(0)}
          aria-label={allChosen ? t('select.none') : t('select.all')}
          disabled={filtered.length === 0}
          onClick={() => setSelected(allChosen ? new Set() : new Set(filtered.map((x) => x.id)))}
        />
        <IconBadge
          icon={<IconTrash width={16} height={16} />}
          className="glim-hue glim-hue-icon"
          style={hueStyle(1)}
          aria-label={t('cleanup.menu')}
          onClick={(e) => void openCleanup(e.currentTarget)}
        />
        <IconBadge
          icon={<IconSearch width={16} height={16} />}
          className="glim-hue glim-hue-icon"
          style={hueStyle(2)}
          aria-label={t('collector.checkAll')}
          disabled={collected.length === 0}
          onClick={() => {
            // An empty id list means every staged link on this route —
            // deliberately unlike the bulk routes, where empty is refused
            // outright rather than read as "all".
            recheckTasks([]);
            toast(t('task.recheck'), 'info');
          }}
        />
        <IconBadge
          icon={<IconPlay width={16} height={16} />}
          className="glim-hue glim-hue-icon"
          style={hueStyle(3)}
          aria-label={t('collector.startAll')}
          disabled={collected.length === 0}
          onClick={startAll}
        />
      </div>

      {/* jdp, 2026-08-24: "Das hauptlinkfenster soll immer die ganze
          fensterbreite einnehmen und immer bis ganz nach unten im fenster
          gehen. egal wie viele links drinn sind." - min-h-0 + flex-1 here,
          inside the root's own h-full/min-h-0 above (itself inside
          app/Layout.tsx's h-screen/overflow-y-auto <main>), makes the list
          the one scrolling region: everything above it keeps its natural
          height (shrink-0), this section absorbs whatever space is left and
          never less than that, and the row list inside scrolls on its own
          rather than growing the whole page. */}
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
          <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
            <TaskListCard groups={groups} base="/api" selection={selection} profile="collector" />
          </div>
        )}
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
