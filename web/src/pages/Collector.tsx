import { useCallback, useEffect, useMemo, useState } from 'react';
import { addLinks, recheckTasks, startTasks } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';
import { PageHeader, Button, TextInput } from '../components/ui';
import { TaskListCard, groupByPackage, type Selection } from '../components/TaskList';
import { PackageActions } from '../components/PackageActions';
import { ContainerDrop } from '../components/ContainerDrop';
import { SkippedLinks } from '../components/SkippedLinks';
import {
  COLLECTOR_FILTERS,
  ListActionBar,
  ListMenu,
  ListToolbar,
  SelectionStrip,
  matchesQuickFilters,
  targetTaskId,
  useRemoval,
  type QuickFilterId,
} from '../components/ListToolbar';
import { EMPTY_SEARCH, matchesSearch, type SearchQuery } from '../components/SearchField';
import { anchorFromEvent, useContextMenu } from '../components/ContextMenu';
import { IconPlus, IconPlay, IconCollector } from '../lib/icons';

export function Collector() {
  const { t } = useT();
  const tasks = useTasks('');
  const { toast } = useToast();
  const [links, setLinks] = useState('');
  const [pkg, setPkg] = useState('');
  const [dragOver, setDragOver] = useState(false);
  const [search, setSearch] = useState<SearchQuery>(EMPTY_SEARCH);
  const [filters, setFilters] = useState<Set<QuickFilterId>>(() => new Set());
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const menu = useContextMenu();

  // Everything this instance holds, not only what is staged: a removal weighs
  // the bytes already on disk, and those belong to rows this page never shows.
  const all = useMemo(() => Object.values(tasks), [tasks]);
  const collected = useMemo(
    () =>
      all
        .filter((x) => x.status === 'collected')
        .sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1)),
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
    const skipped = Math.max(0, submitted - created.length);
    toast(
      skipped
        ? t('collector.toastSkipped', { n: created.length, skipped })
        : t('collector.toastStaged', { n: created.length }),
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

  // The same rule as the download list: the row under the pointer becomes the
  // selection when it was not one already, and with nothing to act on the
  // browser's own menu is left alone rather than replaced by an empty one.
  function onContextMenu(e: React.MouseEvent): void {
    // The column header opens its own menu on right-click; when it has, this one
    // stays out of the way instead of stacking a second menu on top. The native
    // event is the live one — the synthetic event's flag was captured before any
    // handler ran.
    if (e.nativeEvent.defaultPrevented) return;
    const id = targetTaskId(e);
    if (!id && selected.size === 0) return;
    if (id && !selected.has(id)) setSelected(new Set([id]));
    e.preventDefault();
    menu.openAt(anchorFromEvent(e));
  }

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
        onMore={menu.openAt}
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
        all={collected}
        selected={selected}
        base="/api"
        removal={removal}
      />
      {removal.dialog}
    </div>
  );
}
