import { useCallback, useEffect, useMemo, useState } from 'react';
import { recheckTasks, startTasks, type Task } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useReportListView } from '../lib/listview';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';
import { PageHeader, Button } from '../components/ui';
import {
  TaskListCard,
  groupByPackage,
  useCollapsedPackages,
  type Selection,
} from '../components/TaskList';
import { PackageActions } from '../components/PackageActions';
import { AddLinksForm } from '../components/AddLinksForm';
import { ContainerDrop } from '../components/ContainerDrop';
import { TorrentUpload } from '../components/TorrentUpload';
import { FilteredLinks, useFx } from '../components/FilteredLinks';
import { SkippedLinks } from '../components/SkippedLinks';
import {
  COLLECTOR_FILTERS,
  ListActionBar,
  ListMenu,
  ListToolbar,
  SelectionStrip,
  matchesQuickFilters,
  targetPackage,
  targetTaskId,
  useRemoval,
  type ListContext,
  type MenuTarget,
  type QuickFilterId,
} from '../components/ListToolbar';
import { EMPTY_SEARCH, matchesSearch, type SearchQuery } from '../components/SearchField';
import { anchorFromEvent, useContextMenu } from '../components/ContextMenu';
import {
  CollectorFacetSidebar,
  CollectorFacetsToggle,
  EMPTY_FACETS,
  facetActiveCount,
  matchesFacets,
  type FacetSelection,
} from '../components/CollectorFacets';
import { CollectorStats } from '../components/CollectorStats';
import { useScriptMenu } from '../components/ScriptActions';
import { FirstTouchHint } from '../components/FirstTouchHint';
import { usePublishCommandPageContext } from '../lib/commands/pageContext';
import { IconPlay } from '../lib/icons';

export function Collector() {
  const { t } = useT();
  const fx = useFx();
  const tasks = useTasks('');
  const { toast } = useToast();
  // pkg stays lifted here rather than moving into AddLinksForm: the
  // container-drop zone below wants the same package name a link pasted at
  // the same time would get, and it is not part of the form's own lane.
  const [pkg, setPkg] = useState('');
  const [search, setSearch] = useState<SearchQuery>(EMPTY_SEARCH);
  const [filters, setFilters] = useState<Set<QuickFilterId>>(() => new Set());
  // The facet groups the collector's own sidebar exposes (host, file type,
  // package) — see components/CollectorFacets.tsx for why availability is not a
  // fourth one: it is already the quick filters above.
  const [facets, setFacets] = useState<FacetSelection>(EMPTY_FACETS);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const menu = useContextMenu();
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

  // The shell's strip cannot see this page's search box or its quick filters, so
  // it is told which rows survived them — see lib/listview.ts.
  useReportListView(filtered, selected);
  // The command surface's own bridge (lib/commands/pageContext.ts): the exact
  // setSelected/removal this page already holds, so
  // lib/commands/collector.ts's selectAll/removeSelected call the identical
  // functions the toolbar's own buttons call.
  usePublishCommandPageContext(useMemo(() => ({ setSelection: setSelected, removal }), [removal]));
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

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('collector.title')} subtitle={t('collector.subtitle')} />

      <FirstTouchHint id="collector" />

      {/* The hero: one drop zone that is also the paste field, plus the
          per-batch destination/priority/unpacking/comment/password options
          and the recently-used-destination history — see
          components/AddLinksForm.tsx. */}
      <AddLinksForm pkg={pkg} onPkgChange={setPkg} onStaged={handleStaged} />

      {/* Intake that is not a paste, and the trace of what the paste dropped.
          Both sit under the hero and above the list: the paste box is why people
          open this page, and nothing may push it off the top. The skipped strip
          in particular has to render when the list is empty — a paste of nothing
          but duplicates stages nothing, and that is the moment it explains most. */}
      <div className="flex flex-col gap-3">
        <ContainerDrop pkg={pkg} />
        <TorrentUpload pkg={pkg} />
        <FilteredLinks held={held} />
        <SkippedLinks />
      </div>

      {/* The strip and the facet sidebar are both keyed off `collected`, not
          `filtered`: a facet that has narrowed the list to nothing must not
          also make the controls that would widen it disappear. */}
      {collected.length > 0 && <CollectorStats all={collected} visible={filtered} selected={selectedTasks} />}

      <div className="flex min-w-0 flex-col items-start gap-6 lg:flex-row">
        {collected.length > 0 && <CollectorFacetSidebar tasks={collected} selection={facets} onChange={setFacets} />}

        <div className="flex min-w-0 flex-1 flex-col gap-4">
          {collected.length > 0 && (
            <ListToolbar
              search={search}
              onSearch={setSearch}
              filters={COLLECTOR_FILTERS}
              active={filters}
              onActive={setFilters}
              tasks={collected}
              shown={filtered.length}
              right={<CollectorFacetsToggle activeCount={facetActiveCount(facets)} />}
            />
          )}

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

          <div onContextMenu={onContextMenu}>
            {collected.length === 0 ? (
              <div className="glim-card p-12 text-center text-sm text-carbon-textMuted">{t('collector.empty')}</div>
            ) : filtered.length === 0 ? (
              <div className="glim-card p-12 text-center text-sm text-carbon-textMuted">{t('downloads.noMatch')}</div>
            ) : (
              <TaskListCard groups={groups} base="/api" selection={selection} profile="collector" />
            )}
          </div>

          <ListActionBar all={all} selected={selected} onSelected={setSelected} visible={filtered} local>
            <Button
              kind="ghost"
              className="px-2.5 text-xs"
              onClick={() => {
                // An empty id list means every staged link on this route —
                // deliberately unlike the bulk routes, where empty is refused
                // outright rather than read as "all".
                recheckTasks([]);
                toast(t('task.recheck'), 'info');
              }}
            >
              {t('collector.checkAll')}
            </Button>
            <Button kind="secondary" className="px-2.5 text-xs" onClick={startAll} disabled={collected.length === 0}>
              {t('collector.startAll')}
            </Button>
          </ListActionBar>
        </div>
      </div>

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
    </div>
  );
}
