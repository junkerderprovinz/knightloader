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
  Modal,
  NumberInput,
  Toggle,
} from '@carbon/react';
import { Settings as SettingsIcon } from '@carbon/icons-react';
import {
  Task,
  Settings,
  fetchTasks,
  addLinks,
  pause,
  resume,
  remove,
  connectWS,
  fetchSettings,
  saveSettings,
  fetchAccounts,
  saveAccount,
} from './api';

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
  extracting: { type: 'teal', label: 'Extracting' },
  done: { type: 'green', label: 'Done' },
  error: { type: 'red', label: 'Error' },
};

function SettingsModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [cfg, setCfg] = useState<Settings | null>(null);
  const [torboxKey, setTorboxKey] = useState('');
  const [hasTorbox, setHasTorbox] = useState(false);

  useEffect(() => {
    if (!open) return;
    fetchSettings().then(setCfg);
    fetchAccounts().then((s) => setHasTorbox(s.includes('torbox')));
    setTorboxKey('');
  }, [open]);

  async function onSave() {
    if (cfg) await saveSettings(cfg);
    if (torboxKey.trim()) await saveAccount('torbox', torboxKey.trim());
    onClose();
  }

  return (
    <Modal
      open={open}
      modalHeading="Settings"
      primaryButtonText="Save"
      secondaryButtonText="Cancel"
      onRequestClose={onClose}
      onRequestSubmit={onSave}
      size="sm"
    >
      {cfg && (
        <div className="kl-settings">
          <NumberInput
            id="maxConcurrent"
            label="Max simultaneous downloads"
            min={1}
            max={64}
            value={cfg.maxConcurrent}
            onChange={(_, { value }) =>
              setCfg({ ...cfg, maxConcurrent: Number(value) || 1 })
            }
          />
          <NumberInput
            id="maxPerHost"
            label="Max downloads per host"
            min={1}
            max={64}
            value={cfg.maxPerHost}
            onChange={(_, { value }) =>
              setCfg({ ...cfg, maxPerHost: Number(value) || 1 })
            }
          />
          <NumberInput
            id="speedLimit"
            label="Speed limit (KiB/s, 0 = unlimited)"
            helperText="Applies to media (yt-dlp) and JDownloader downloads."
            min={0}
            step={256}
            value={Math.round(cfg.speedLimit / 1024)}
            onChange={(_, { value }) =>
              setCfg({ ...cfg, speedLimit: Math.max(0, Number(value) || 0) * 1024 })
            }
          />
          <Toggle
            id="extract"
            labelText="Extract archives after download"
            labelA="Off"
            labelB="On"
            toggled={cfg.extract}
            onToggle={(v) => setCfg({ ...cfg, extract: v })}
          />
          <Toggle
            id="deleteArchive"
            labelText="Delete archive after extraction"
            labelA="Keep"
            labelB="Delete"
            toggled={cfg.deleteArchive}
            onToggle={(v) => setCfg({ ...cfg, deleteArchive: v })}
          />
          <TextInput
            id="torboxKey"
            type="password"
            labelText="TorBox API key"
            helperText={
              hasTorbox
                ? 'A key is stored (encrypted). Enter a new one to replace it; applied on restart.'
                : 'Stored encrypted on this instance; applied on restart.'
            }
            placeholder={hasTorbox ? '••••••••' : 'Paste your key…'}
            value={torboxKey}
            onChange={(e) => setTorboxKey(e.target.value)}
          />
        </div>
      )}
    </Modal>
  );
}

export default function App() {
  const [tasks, setTasks] = useState<Record<string, Task>>({});
  const [links, setLinks] = useState('');
  const [pkg, setPkg] = useState('');
  const [showSettings, setShowSettings] = useState(false);
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
          <span className="kl-spacer" />
          <Button
            kind="ghost"
            size="sm"
            hasIconOnly
            iconDescription="Settings"
            renderIcon={SettingsIcon}
            onClick={() => setShowSettings(true)}
          />
        </header>
        <SettingsModal open={showSettings} onClose={() => setShowSettings(false)} />

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
