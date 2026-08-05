import { useEffect, useState } from 'react';
import { fetchAccounts, saveAccount } from '../lib/api';
import { PageHeader, Card, Button, Field, TextInput } from '../components/ui';

export function Accounts() {
  const [services, setServices] = useState<string[]>([]);
  const [torboxKey, setTorboxKey] = useState('');
  const [saved, setSaved] = useState(false);

  const load = () => fetchAccounts().then(setServices);
  useEffect(() => {
    load();
  }, []);

  const hasTorbox = services.includes('torbox');

  async function onSaveKey() {
    if (!torboxKey.trim()) return;
    await saveAccount('torbox', torboxKey.trim());
    setTorboxKey('');
    await load();
    setSaved(true);
    setTimeout(() => setSaved(false), 1800);
  }

  async function onRemove() {
    await saveAccount('torbox', '');
    await load();
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Accounts" subtitle="Premium and debrid accounts stay yours, stored encrypted on this instance." />

      <Card className="flex flex-col gap-4 max-w-2xl">
        <div className="flex items-center gap-3">
          <div className="grid h-10 w-10 place-items-center rounded-lg bg-carbon-surface2 font-bold text-accent">TB</div>
          <div className="min-w-0">
            <h2 className="text-base font-semibold text-carbon-text">TorBox</h2>
            <p className="text-carbon-textMuted text-xs">Debrid — unlocks 100+ file hosters into direct downloads.</p>
          </div>
          <span className="flex-1" />
          <span
            className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-[11px] font-medium ${
              hasTorbox ? 'text-statusOk bg-statusOkBg' : 'text-statusNeutral bg-statusNeutralBg'
            }`}
          >
            {hasTorbox ? 'Connected' : 'Not connected'}
          </span>
        </div>

        <Field
          label="TorBox API key"
          hint={hasTorbox ? 'A key is stored. Enter a new one to replace it. Applied on restart.' : 'Get it at torbox.app → Settings → API. Applied on restart.'}
        >
          <TextInput
            type="password"
            placeholder={hasTorbox ? '••••••••' : 'Paste your key…'}
            value={torboxKey}
            onChange={(e) => setTorboxKey(e.target.value)}
          />
        </Field>
        <div className="flex items-center gap-3">
          <Button onClick={onSaveKey} disabled={!torboxKey.trim()}>
            {hasTorbox ? 'Replace key' : 'Connect'}
          </Button>
          {hasTorbox && (
            <Button kind="ghost" onClick={onRemove}>
              Disconnect
            </Button>
          )}
          {saved && <span className="text-statusOk text-sm">Saved.</span>}
        </div>
      </Card>

      <p className="text-carbon-textMuted text-sm">More debrid and premium-hoster accounts will land here.</p>
    </div>
  );
}
