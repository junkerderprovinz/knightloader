import { useCallback, useEffect, useMemo, useState } from 'react';
import { addLinks, recheckTasks, startTasks } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';
import { PageHeader, Button, TextInput } from '../components/ui';
import {
  TaskListCard,
  groupByPackage,
  useCollapsedPackages,
  type Selection,
} from '../components/TaskList';
import { PackageActions } from '../components/PackageActions';
import { ContainerDrop } from '../components/ContainerDrop';
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
import { IconPlus, IconPlay, IconCollector } from '../lib/icons';

export function Collector() {
  const { t } = useT();
  const fx = useFx();
  const tasks = useTasks('');
  const { toast } = useToast();
  const [links, setLinks] = useState('');
  const [pkg, setPkg] = useState('');
  const [dragOver, setDragOver] = useState(false);
  const [search, setSearch] = useState<SearchQuery>(EMPTY_SEARCH);
  const [filters, setFilters] = useState<Set<QuickFilterId>>(() => new Set());
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
    () => collected.filter((x) => matchesQuickFilters(x, filters) && matchesSearch(x, search)),
    [collected, filters, search],
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

  async function onAdd() {
    if (!links.trim()) return;
    const submitted = new Set(
      links
        .split(/[\r\n]+/)
        .map((l) => l.trim())
        .filter((l) => /^https?:\/\//i.test(l)),
    ).size;
    const created = await addLinks(links, pkg);
    setLinks('');
    if (!created.length) {
      toast(t('collector.toastNone'), 'fail');
      return;
    }
    // A link the filter held is a task, so it comes back in `created` — but it
    // was not staged, and counting it as staged is the sentence that sends
    // somebody looking for it in a list it is deliberately not in.
    const heldNow = created.filter((x) => x.skipped).length;
    const staged = created.length - heldNow;
    const skipped = Math.max(0, submitted - created.length);
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

  function onDrop(e: React.DragEvent) {
    e.preventDefault();
    setDragOver(false);
    const text = e.dataTransfer.getData('text');
    if (text) setLinks((l) => (l ? `${l}\n${text}` : text));
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

      {/* The hero: one drop zone that is also the paste field. */}
      <div className="glim-card p-0 overflow-hidden">
        <div
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={onDrop}
          className={`relative m-3 rounded-[var(--radius-control)] transition-colors ${
            dragOver ? 'bg-accentSoft shadow-[0_0_0_2px_var(--focus-ring)]' : 'bg-carbon-surface2'
          }`}
        >
          <textarea
            dir="ltr"
            placeholder={t('collector.placeholder')}
            rows={4}
            value={links}
            onChange={(e) => setLinks(e.target.value)}
            onKeyDown={(e) => {
              if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') onAdd();
            }}
            className="w-full resize-y rounded-[var(--radius-control)] bg-transparent px-4 py-3 text-sm text-carbon-text placeholder:text-carbon-textMuted outline-none"
          />
          {dragOver && (
            <div className="pointer-events-none absolute inset-0 grid place-items-center rounded-[var(--radius-control)]">
              <span className="flex items-center gap-2 text-sm font-medium text-accent">
                <IconCollector width={18} height={18} />
                {t('collector.add')}
              </span>
            </div>
          )}
        </div>
        <div className="flex items-center gap-3 px-4 pb-4">
          <TextInput
            placeholder={t('collector.package')}
            value={pkg}
            onChange={(e) => setPkg(e.target.value)}
            className="max-w-xs"
          />
          <span className="flex-1" />
          <Button icon={<IconPlus />} onClick={onAdd} disabled={!links.trim()}>
            {t('collector.add')}
          </Button>
        </div>
      </div>

      {/* Intake that is not a paste, and the trace of what the paste dropped.
          Both sit under the hero and above the list: the paste box is why people
          open this page, and nothing may push it off the top. The skipped strip
          in particular has to render when the list is empty — a paste of nothing
          but duplicates stages nothing, and that is the moment it explains most. */}
      <div className="flex flex-col gap-3">
        <ContainerDrop pkg={pkg} />
        <FilteredLinks held={held} />
        <SkippedLinks />
      </div>

      {collected.length > 0 && (
        <ListToolbar
          search={search}
          onSearch={setSearch}
          filters={COLLECTOR_FILTERS}
          active={filters}
          onActive={setFilters}
          tasks={collected}
          shown={filtered.length}
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

      <ListMenu
        anchor={menu.anchor}
        onClose={menu.close}
        all={all}
        selected={selected}
        base="/api"
        removal={removal}
        target={target}
        list={listContext}
      />
      {removal.dialog}
    </div>
  );
}
