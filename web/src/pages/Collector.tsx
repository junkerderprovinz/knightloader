import { useEffect, useMemo, useState } from 'react';
import { addLinks, remove, startTasks } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { PageHeader, Button, TextInput, Card } from '../components/ui';
import { PackageGroup, groupByPackage, type Selection } from '../components/TaskList';
import { IconPlus, IconPlay, IconTrash } from '../lib/icons';

export function Collector() {
  const tasks = useTasks('');
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
    await addLinks(links, pkg);
    setLinks('');
  }

  function onDrop(e: React.DragEvent) {
    e.preventDefault();
    setDragOver(false);
    const text = e.dataTransfer.getData('text');
    if (text) setLinks((l) => (l ? `${l}\n${text}` : text));
  }

  const startSelected = () => selected.size && startTasks([...selected]);
  const startAll = () => startTasks([]);
  const removeSelected = () => selected.forEach((id) => remove(id));

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Collector" subtitle="Paste or drop links — they are analysed and staged, then you start them." />

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
            placeholder="Paste links — one URL per line — or drop them here…  (Ctrl+Enter to add)"
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
          <TextInput placeholder="Package (optional)" value={pkg} onChange={(e) => setPkg(e.target.value)} className="max-w-xs" />
          <span className="flex-1" />
          <Button icon={<IconPlus />} onClick={onAdd} disabled={!links.trim()}>
            Add to collector
          </Button>
        </div>
      </Card>

      {collected.length === 0 ? (
        <div className="kl-card p-10 text-center text-carbon-textMuted">
          The collector is empty. Paste some links above to stage them.
        </div>
      ) : (
        <>
          <div className="flex items-center gap-2 rounded-card kl-card px-5 py-3 text-sm">
            <span className="text-carbon-textSub">
              {selected.size > 0 ? `${selected.size} selected` : `${collected.length} staged`}
            </span>
            <span className="flex-1" />
            <Button kind="primary" icon={<IconPlay />} onClick={startSelected} disabled={selected.size === 0}>
              Start selected
            </Button>
            <Button kind="secondary" onClick={startAll}>
              Start all
            </Button>
            <Button kind="danger" icon={<IconTrash />} onClick={removeSelected} disabled={selected.size === 0}>
              Remove
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
