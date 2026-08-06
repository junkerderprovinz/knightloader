import { useEffect, useMemo, useState } from 'react';
import { addLinks, remove, startTasks, recheckTasks } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';
import { PageHeader, Button, TextInput } from '../components/ui';
import { TaskListCard, groupByPackage, type Selection } from '../components/TaskList';
import { PackageActions } from '../components/PackageActions';
import { IconPlus, IconPlay, IconTrash, IconCollector } from '../lib/icons';

export function Collector() {
  const { t } = useT();
  const tasks = useTasks('');
  const { toast } = useToast();
  const [links, setLinks] = useState('');
  const [pkg, setPkg] = useState('');
  const [dragOver, setDragOver] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const collected = useMemo(
    () =>
      Object.values(tasks)
        .filter((x) => x.status === 'collected')
        .sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1)),
    [tasks],
  );
  const groups = useMemo(() => groupByPackage(collected), [collected]);

  // Drop selections that have left the collector.
  useEffect(() => {
    setSelected((prev) => {
      const live = new Set(collected.map((x) => x.id));
      const next = new Set([...prev].filter((id) => live.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [collected]);

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
  const removeSelected = () => selected.forEach((id) => remove(id));
  const selectAll = () => setSelected(new Set(collected.map((x) => x.id)));
  const clearSelection = () => setSelected(new Set());
  const offline = useMemo(() => collected.filter((x) => !!x.error), [collected]);
  const removeOffline = () => offline.forEach((x) => remove(x.id));

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

      {collected.length === 0 ? (
        <div className="glim-card p-12 text-center text-sm text-carbon-textMuted">{t('collector.empty')}</div>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2">
            <span className="glim-num text-sm text-carbon-textSub">
              {selected.size > 0
                ? `${selected.size} ${t('collector.selected')}`
                : `${collected.length} ${t('collector.staged')}`}
            </span>
            <Button
              kind="ghost"
              className="px-2.5 text-xs"
              onClick={selected.size ? clearSelection : selectAll}
            >
              {selected.size ? t('collector.selectNone') : t('collector.selectAll')}
            </Button>
            <Button
              kind="ghost"
              className="px-2.5 text-xs"
              onClick={() => {
                recheckTasks(selected.size ? [...selected] : []);
                toast(t('task.recheck'), 'info');
              }}
            >
              {t('task.recheck')}
            </Button>
            {offline.length > 0 && (
              <Button kind="ghost" className="px-2.5 text-xs" onClick={removeOffline}>
                {t('collector.removeOffline')} ({offline.length})
              </Button>
            )}
            <PackageActions
              tasks={collected}
              selected={selected}
              base="/api"
              onDone={() => toast(t('task.applied'), 'ok')}
            />
            <span className="flex-1" />
            <Button kind="ghost" icon={<IconTrash />} onClick={removeSelected} disabled={selected.size === 0}>
              {t('collector.remove')}
            </Button>
            <Button kind="secondary" onClick={startAll}>
              {t('collector.startAll')}
            </Button>
            <Button kind="primary" icon={<IconPlay />} onClick={startSelected} disabled={selected.size === 0}>
              {t('collector.startSelected')}
            </Button>
          </div>
          <TaskListCard groups={groups} base="/api" selection={selection} />
        </>
      )}
    </div>
  );
}
