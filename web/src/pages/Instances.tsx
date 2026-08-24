import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { type Instance, fetchInstances, addInstance, removeInstance, redeemPairingCode } from '../lib/api';
import { useT } from '../lib/i18n';
import { PageHeader, Card, Button, Field, TextInput, TextArea, SectionTitle } from '../components/ui';
import { InstanceCard } from '../components/InstanceCard';
import { FirstTouchHint } from '../components/FirstTouchHint';

export function Instances() {
  const { t } = useT();
  const [peers, setPeers] = useState<Instance[]>([]);
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [err, setErr] = useState('');
  const [code, setCode] = useState('');
  const [pairing, setPairing] = useState(false);
  const [pairErr, setPairErr] = useState('');
  const [pairOk, setPairOk] = useState('');
  const navigate = useNavigate();

  const load = () => fetchInstances().then(setPeers);
  useEffect(() => {
    load();
  }, []);

  async function onAdd() {
    setErr('');
    try {
      const r = await addInstance(name.trim(), url.trim());
      if (!r.online) setErr(t('instances.offlineWarning'));
      setName('');
      setUrl('');
      await load();
    } catch (e: any) {
      setErr(String(e?.message ?? e));
    }
  }

  // Redeem a code from the OTHER instance's own Access tab (settings/Access.tsx's
  // own "Generate pairing code" card, backed by POST /api/instances/pairing-code)
  // instead of typing that instance's name and address here by hand - one call
  // registers both directions (internal/api/routes_pairing.go's own doc comment
  // on why the redeem handler completes the other side before adding it locally).
  async function onPair() {
    setPairErr('');
    setPairOk('');
    setPairing(true);
    try {
      const r = await redeemPairingCode(code.trim());
      setPairOk(t('instances.pairSuccess', { name: r.name }));
      setCode('');
      await load();
    } catch (e: any) {
      setPairErr(String(e?.message ?? e));
    } finally {
      setPairing(false);
    }
  }

  async function onRemove(n: string) {
    await removeInstance(n);
    await load();
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('instances.title')} subtitle={t('instances.subtitle')} />

      <FirstTouchHint id="instances" />

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <InstanceCard name={t('instances.thisInstance')} url={location.host} base="/api" />
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

      <Card className="flex flex-col gap-4">
        <SectionTitle>{t('instances.add')}</SectionTitle>
        <div className="grid grid-cols-1 sm:grid-cols-[1fr_2fr_auto] gap-3 items-end">
          <Field label={t('instances.name')}>
            <TextInput placeholder="cellar" value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t('instances.url')}>
            <TextInput placeholder="http://host:8749" value={url} onChange={(e) => setUrl(e.target.value)} />
          </Field>
          <Button kind="secondary" onClick={onAdd} disabled={!name.trim() || !url.trim()}>
            {t('instances.addButton')}
          </Button>
        </div>
        {err && <div className="text-statusFail text-sm">{err}</div>}
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle hint={t('instances.pairHint')}>{t('instances.pairTitle')}</SectionTitle>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div className="min-w-0 flex-1">
            <TextArea
              rows={2}
              dir="ltr"
              placeholder={t('instances.pairPlaceholder')}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              className="text-xs"
            />
          </div>
          <Button kind="secondary" onClick={() => void onPair()} disabled={!code.trim() || pairing}>
            {t('instances.pairButton')}
          </Button>
        </div>
        {pairErr && <div className="text-statusFail text-sm">{pairErr}</div>}
        {pairOk && <div className="text-statusOk text-sm">{pairOk}</div>}
      </Card>
    </div>
  );
}
