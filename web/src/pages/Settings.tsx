import { useEffect, useState } from 'react';
import {
  type Settings,
  type Instance,
  fetchSettings,
  saveSettings,
  fetchAccounts,
  saveAccount,
  fetchInstances,
  addInstance,
  removeInstance,
} from '../lib/api';
import { Button, Field, TextInput, NumberInput, Toggle, Card } from '../components/ui';
import { IconTrash } from '../lib/icons';

function Section({ title, desc, children }: { title: string; desc?: string; children: React.ReactNode }) {
  return (
    <Card className="flex flex-col gap-4">
      <div>
        <h2 className="text-base font-semibold text-carbon-text">{title}</h2>
        {desc && <p className="text-carbon-textMuted text-xs mt-0.5">{desc}</p>}
      </div>
      {children}
    </Card>
  );
}

export function SettingsPage() {
  const [cfg, setCfg] = useState<Settings | null>(null);
  const [saved, setSaved] = useState(false);
  const [torboxKey, setTorboxKey] = useState('');
  const [hasTorbox, setHasTorbox] = useState(false);
  const [torboxSaved, setTorboxSaved] = useState(false);

  const [peers, setPeers] = useState<Instance[]>([]);
  const [peerName, setPeerName] = useState('');
  const [peerURL, setPeerURL] = useState('');
  const [peerErr, setPeerErr] = useState('');

  useEffect(() => {
    fetchSettings().then(setCfg);
    fetchAccounts().then((s) => setHasTorbox(s.includes('torbox')));
    fetchInstances().then(setPeers);
  }, []);

  async function onSaveSettings() {
    if (!cfg) return;
    const applied = await saveSettings(cfg);
    setCfg(applied);
    setSaved(true);
    setTimeout(() => setSaved(false), 1800);
  }

  async function onSaveKey() {
    if (!torboxKey.trim()) return;
    await saveAccount('torbox', torboxKey.trim());
    setTorboxKey('');
    setHasTorbox(true);
    setTorboxSaved(true);
    setTimeout(() => setTorboxSaved(false), 1800);
  }

  async function onAddPeer() {
    setPeerErr('');
    try {
      const r = await addInstance(peerName.trim(), peerURL.trim());
      if (!r.online) setPeerErr('Added, but the instance did not answer (offline?).');
      setPeerName('');
      setPeerURL('');
      setPeers(await fetchInstances());
    } catch (e: any) {
      setPeerErr(String(e?.message ?? e));
    }
  }

  async function onRemovePeer(name: string) {
    await removeInstance(name);
    setPeers(await fetchInstances());
  }

  return (
    <div className="flex flex-col gap-5">
      <h1 className="text-2xl font-bold text-carbon-text">Settings</h1>

      {cfg && (
        <Section title="Downloads" desc="How many downloads run at once, and the global speed cap.">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <Field label="Max simultaneous">
              <NumberInput value={cfg.maxConcurrent} min={1} max={64} onValue={(v) => setCfg({ ...cfg, maxConcurrent: v })} />
            </Field>
            <Field label="Max per host">
              <NumberInput value={cfg.maxPerHost} min={1} max={64} onValue={(v) => setCfg({ ...cfg, maxPerHost: v })} />
            </Field>
            <Field label="Speed limit (KiB/s, 0 = ∞)" hint="Applies to yt-dlp and JDownloader.">
              <NumberInput
                value={Math.round(cfg.speedLimit / 1024)}
                min={0}
                step={256}
                onValue={(v) => setCfg({ ...cfg, speedLimit: Math.max(0, v) * 1024 })}
              />
            </Field>
          </div>
          <div className="flex flex-col gap-3">
            <Toggle checked={cfg.extract} onChange={(v) => setCfg({ ...cfg, extract: v })} label="Extract archives after download" />
            <Toggle checked={cfg.deleteArchive} onChange={(v) => setCfg({ ...cfg, deleteArchive: v })} label="Delete archive after extraction" />
          </div>
          <div className="flex items-center gap-3">
            <Button onClick={onSaveSettings}>Save</Button>
            {saved && <span className="text-statusOk text-sm">Saved.</span>}
          </div>
        </Section>
      )}

      <Section title="TorBox debrid" desc="Stored encrypted on this instance; applied on restart.">
        <Field
          label="TorBox API key"
          hint={hasTorbox ? 'A key is stored. Enter a new one to replace it.' : 'Premium hosters are unlocked through TorBox.'}
        >
          <TextInput
            type="password"
            placeholder={hasTorbox ? '••••••••' : 'Paste your key…'}
            value={torboxKey}
            onChange={(e) => setTorboxKey(e.target.value)}
          />
        </Field>
        <div className="flex items-center gap-3">
          <Button kind="secondary" onClick={onSaveKey} disabled={!torboxKey.trim()}>
            Save key
          </Button>
          {torboxSaved && <span className="text-statusOk text-sm">Saved.</span>}
        </div>
      </Section>

      <Section title="Instances" desc="Register other KnightLoader instances to view and control them from here.">
        {peers.length === 0 && <div className="text-carbon-textMuted text-sm">No other instances yet.</div>}
        {peers.map((p) => (
          <div key={p.name} className="flex items-center gap-3">
            <span className="w-32 font-medium text-carbon-text">{p.name}</span>
            <span className="flex-1 text-carbon-textMuted text-sm truncate">{p.url}</span>
            <Button kind="danger" icon={<IconTrash />} title={`Remove ${p.name}`} onClick={() => onRemovePeer(p.name)} />
          </div>
        ))}
        <div className="grid grid-cols-1 sm:grid-cols-[1fr_2fr_auto] gap-3 items-end">
          <Field label="Name">
            <TextInput placeholder="e.g. cellar" value={peerName} onChange={(e) => setPeerName(e.target.value)} />
          </Field>
          <Field label="URL">
            <TextInput placeholder="http://host:8749" value={peerURL} onChange={(e) => setPeerURL(e.target.value)} />
          </Field>
          <Button kind="secondary" onClick={onAddPeer} disabled={!peerName.trim() || !peerURL.trim()}>
            Add instance
          </Button>
        </div>
        {peerErr && <div className="text-statusFail text-sm">{peerErr}</div>}
      </Section>
    </div>
  );
}
