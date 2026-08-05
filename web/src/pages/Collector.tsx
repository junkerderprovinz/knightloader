import { useEffect, useMemo, useState } from 'react';
import { addLinks, remove, startTasks } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';
import { PageHeader, Button, TextInput, Card } from '../components/ui';
import { PackageGroup, groupByPackage, type Selection } from '../components/TaskList';
import { IconPlus, IconPlay, IconTrash } from '../lib/icons';

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
        .filter((t) => t.status === 'collected')
        .sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1)),
    [tasks],
  );
  const groups = useMemo(() => groupByPackage(collected), [collected]);

  // Drop selections that have left the collector.
  useEffect(() => {
    setSelected((prev) => {
      const live = new Set(collected.map((t) => t.id));
      const next = new Set([...prev].filter((id) => live.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [collected]);

  const selection: Selection = {
    ids: selected,
    toggle: (id) =>
      setSelected((s) => {
        const n = new Set(s);
        n.has(id) ? n.delete(id) : n.add(id);
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

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('collector.title')} subtitle={t('collector.subtitle')} />

      <Card className="flex flex-col gap-3">
        <div
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={onDrop}
          className={`rounded-lg transition-colors ${dragOver ? 'ring-2 ring-accent' : ''}`}
        >
          <textarea
            placeholder={t('collector.placeholder')}
            rows={4}
            value={links}
            onChange={(e) => setLinks(e.target.value)}
            onKeyDown={(e) => {
              if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') onAdd();
            }}
            className="w-full rounded-lg bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text placeholder:text-carbon-textMuted outline-none focus:ring-2 focus:ring-[var(--status-info-solid)] resize-y"
          />
        </div>
        <div className="flex items-center gap-3">
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
      </Card>

      {collected.length === 0 ? (
        <div className="kl-card p-10 text-center text-carbon-textMuted">{t('collector.empty')}</div>
      ) : (
        <>
          <div className="flex items-center gap-2 rounded-card kl-card px-5 py-3 text-sm">
            <span className="text-carbon-textSub">
              {selected.size > 0
                ? `${selected.size} ${t('collector.selected')}`
                : `${collected.length} ${t('collector.staged')}`}
            </span>
            <span className="flex-1" />
            <Button kind="primary" icon={<IconPlay />} onClick={startSelected} disabled={selected.size === 0}>
              {t('collector.startSelected')}
            </Button>
            <Button kind="secondary" onClick={startAll}>
              {t('collector.startAll')}
            </Button>
            <Button kind="danger" icon={<IconTrash />} onClick={removeSelected} disabled={selected.size === 0}>
              {t('collector.remove')}
            </Button>
          </div>
          {groups.map(([name, items]) => (
            <PackageGroup key={name || '__none'} name={name} items={items} base="/api" selection={selection} />
          ))}
        </>
      )}
    </div>
  );
}
