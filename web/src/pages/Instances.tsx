import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { type Instance, fetchInstances, addInstance, removeInstance } from '../lib/api';
import { PageHeader, Card, Button, Field, TextInput } from '../components/ui';
import { InstanceCard } from '../components/InstanceCard';

export function Instances() {
  const [peers, setPeers] = useState<Instance[]>([]);
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [err, setErr] = useState('');
  const navigate = useNavigate();

  const load = () => fetchInstances().then(setPeers);
  useEffect(() => {
    load();
  }, []);

  async function onAdd() {
    setErr('');
    try {
      const r = await addInstance(name.trim(), url.trim());
      if (!r.online) setErr('Added, but the instance did not answer (offline?).');
      setName('');
      setUrl('');
      await load();
    } catch (e: any) {
      setErr(String(e?.message ?? e));
    }
  }

  async function onRemove(n: string) {
    await removeInstance(n);
    await load();
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Instances" subtitle="View and control every KnightLoader from one dashboard." />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <InstanceCard name="This instance" url={location.host} base="/api" />
        {peers.map((p) => (
          <InstanceCard
            key={p.name}
            name={p.name}
            url={p.url}
            base={`/api/instances/${encodeURIComponent(p.name)}`}
            onOpen={() => navigate(`/downloads?instance=${encodeURIComponent(p.name)}`)}
            onRemove={() => onRemove(p.name)}
          />
        ))}
      </div>

      <Card className="flex flex-col gap-4 max-w-2xl">
        <h2 className="text-base font-semibold text-carbon-text">Add an instance</h2>
        <div className="grid grid-cols-1 sm:grid-cols-[1fr_2fr_auto] gap-3 items-end">
          <Field label="Name">
            <TextInput placeholder="e.g. cellar" value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="URL">
            <TextInput placeholder="http://host:8749" value={url} onChange={(e) => setUrl(e.target.value)} />
          </Field>
          <Button kind="secondary" onClick={onAdd} disabled={!name.trim() || !url.trim()}>
            Add instance
          </Button>
        </div>
        {err && <div className="text-statusFail text-sm">{err}</div>}
      </Card>
    </div>
  );
}
