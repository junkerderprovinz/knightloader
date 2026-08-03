import { useEffect, useRef, useState } from 'react';
import {
  Theme,
  TextArea,
  TextInput,
  Button,
  Tag,
  ProgressBar,
  Table,
  TableHead,
  TableRow,
  TableHeader,
  TableBody,
  TableCell,
} from '@carbon/react';
import { Task, fetchTasks, addLinks, pause, resume, remove, connectWS } from './api';

function fmtBytes(n: number): string {
  if (!n) return '—';
  const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${u[i]}`;
}
const fmtSpeed = (n: number) => (n > 0 ? `${fmtBytes(n)}/s` : '');

const statusTag: Record<Task['status'], { type: any; label: string }> = {
  queued: { type: 'cool-gray', label: 'Queued' },
  running: { type: 'blue', label: 'Running' },
  paused: { type: 'gray', label: 'Paused' },
  done: { type: 'green', label: 'Done' },
  error: { type: 'red', label: 'Error' },
};

export default function App() {
  const [tasks, setTasks] = useState<Record<string, Task>>({});
  const [links, setLinks] = useState('');
  const [pkg, setPkg] = useState('');
  const closer = useRef<(() => void) | null>(null);

  useEffect(() => {
    fetchTasks().then((list) =>
      setTasks(Object.fromEntries((list ?? []).map((t) => [t.id, t]))),
    );
    closer.current = connectWS((type, data) => {
      if (type === 'snapshot')
        setTasks(Object.fromEntries((data ?? []).map((t: Task) => [t.id, t])));
      else if (type === 'task') setTasks((p) => ({ ...p, [data.id]: data }));
      else if (type === 'removed')
        setTasks((p) => {
          const n = { ...p };
          delete n[data.id];
          return n;
        });
    });
    return () => closer.current?.();
  }, []);

  const list = Object.values(tasks).sort((a, b) =>
    a.createdAt < b.createdAt ? -1 : 1,
  );

  async function onAdd() {
    if (!links.trim()) return;
    await addLinks(links, pkg);
    setLinks('');
  }

  return (
    <Theme theme="g100">
      <div className="kl-app">
        <header className="kl-header">
          <span aria-hidden>⚔️</span>
          <span className="kl-title">KnightLoader</span>
          <span className="kl-muted">working title</span>
        </header>

        <main className="kl-content">
          <div className="kl-add">
            <TextArea
              labelText="Links"
              placeholder="One URL per line…"
              rows={3}
              value={links}
              onChange={(e) => setLinks(e.target.value)}
            />
            <TextInput
              id="pkg"
              labelText="Package (optional)"
              placeholder="e.g. Season 1"
              value={pkg}
              onChange={(e) => setPkg(e.target.value)}
            />
            <div>
              <Button onClick={onAdd} disabled={!links.trim()}>
                Add to queue
              </Button>
            </div>
          </div>

          <Table size="lg" useZebraStyles>
            <TableHead>
              <TableRow>
                <TableHeader>Name</TableHeader>
                <TableHeader>Size</TableHeader>
                <TableHeader>Progress</TableHeader>
                <TableHeader>Status</TableHeader>
                <TableHeader>Actions</TableHeader>
              </TableRow>
            </TableHead>
            <TableBody>
              {list.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} className="kl-muted">
                    No downloads yet — paste a link above.
                  </TableCell>
                </TableRow>
              )}
              {list.map((t) => {
                const pct =
                  t.size > 0
                    ? Math.min(100, Math.round((t.loaded / t.size) * 100))
                    : t.status === 'done'
                      ? 100
                      : 0;
                const st = statusTag[t.status] ?? { type: 'gray', label: t.status };
                return (
                  <TableRow key={t.id}>
                    <TableCell>
                      <div>{t.name || t.url}</div>
                      {t.error && <div className="kl-err">{t.error}</div>}
                    </TableCell>
                    <TableCell>{fmtBytes(t.size)}</TableCell>
                    <TableCell>
                      <div className="kl-progress">
                        <ProgressBar
                          label={`${pct}%`}
                          hideLabel
                          value={pct}
                          max={100}
                          size="small"
                          status={
                            t.status === 'done'
                              ? 'finished'
                              : t.status === 'error'
                                ? 'error'
                                : 'active'
                          }
                        />
                        <div className="kl-muted">
                          {pct}%{fmtSpeed(t.speed) && ` · ${fmtSpeed(t.speed)}`}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Tag type={st.type} size="sm">
                        {st.label}
                      </Tag>
                    </TableCell>
                    <TableCell>
                      <div className="kl-actions">
                        {t.status === 'running' && (
                          <Button kind="ghost" size="sm" onClick={() => pause(t.id)}>
                            Pause
                          </Button>
                        )}
                        {t.status === 'paused' && (
                          <Button kind="ghost" size="sm" onClick={() => resume(t.id)}>
                            Resume
                          </Button>
                        )}
                        <Button kind="ghost" size="sm" onClick={() => remove(t.id)}>
                          Remove
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </main>
      </div>
    </Theme>
  );
}
