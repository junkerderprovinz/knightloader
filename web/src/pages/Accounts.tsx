import { useEffect, useState } from 'react';
import { type Account, fetchAccounts, saveAccount, testAccount } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { PageHeader, Card, Button, Field, TextInput } from '../components/ui';

// The catalogue of services KnightLoader can route through. `id` matches the
// backend resolver id and the key under which the secret is stored.
const SERVICES: {
  id: string;
  label: string;
  mark: string;
  blurb: TranslationKey;
  where: string;
}[] = [
  { id: 'torbox', label: 'TorBox', mark: 'TB', blurb: 'accounts.blurb.torbox', where: 'torbox.app → Settings → API' },
  {
    id: 'alldebrid',
    label: 'AllDebrid',
    mark: 'AD',
    blurb: 'accounts.blurb.alldebrid',
    where: 'alldebrid.com → Account → API keys',
  },
  {
    id: 'realdebrid',
    label: 'Real-Debrid',
    mark: 'RD',
    blurb: 'accounts.blurb.realdebrid',
    where: 'real-debrid.com → My Account → API token',
  },
];

export function Accounts() {
  const { t } = useT();
  const [accounts, setAccounts] = useState<Account[]>([]);

  const load = () => fetchAccounts().then(setAccounts);
  useEffect(() => {
    load();
  }, []);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('accounts.title')} subtitle={t('accounts.subtitle')} />

      <div className="flex flex-col gap-4 max-w-2xl">
        {SERVICES.map((s) => (
          <ServiceCard
            key={s.id}
            service={s}
            state={accounts.find((a) => a.id === s.id)}
            onChanged={load}
          />
        ))}
      </div>

      <p className="text-carbon-textMuted text-sm max-w-2xl">{t('accounts.more')}</p>
    </div>
  );
}

function ServiceCard({
  service,
  state,
  onChanged,
}: {
  service: (typeof SERVICES)[number];
  state?: Account;
  onChanged: () => void;
}) {
  // blurb is a translation key so the catalogue stays language-agnostic.
  const { t } = useT();
  const [key, setKey] = useState('');
  const [saved, setSaved] = useState(false);
  const [testing, setTesting] = useState(false);
  const [result, setResult] = useState<Account | null>(null);
  const connected = state?.configured ?? false;

  async function onSave() {
    if (!key.trim()) return;
    await saveAccount(service.id, key.trim());
    setKey('');
    setResult(null);
    onChanged();
    setSaved(true);
    setTimeout(() => setSaved(false), 1800);
  }

  async function onRemove() {
    await saveAccount(service.id, '');
    setResult(null);
    onChanged();
  }

  // Testing asks the service itself, so a typo in a key shows up here instead of
  // on the first download.
  async function onTest() {
    setTesting(true);
    try {
      setResult(await testAccount(service.id));
    } finally {
      setTesting(false);
    }
  }

  return (
    <Card className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <div className="grid h-10 w-10 shrink-0 place-items-center rounded-[var(--radius-control)] bg-carbon-surface2 text-sm font-bold text-carbon-textSub">
          {service.mark}
        </div>
        <div className="min-w-0">
          <h2 className="text-sm font-semibold text-carbon-text">{service.label}</h2>
          <p className="text-carbon-textMuted text-xs">{t(service.blurb)}</p>
        </div>
        <span className="flex-1" />
        <span
          className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-[11px] font-medium ${
            connected ? 'text-statusOk bg-statusOkBg' : 'text-statusNeutral bg-statusNeutralBg'
          }`}
        >
          <span className={`h-1.5 w-1.5 rounded-full ${connected ? 'bg-statusOkSolid' : 'bg-statusNeutralSolid'}`} />
          {connected ? t('accounts.connected') : t('accounts.notConnected')}
        </span>
      </div>

      <Field
        label={t('accounts.keyLabel', { service: service.label })}
        hint={connected ? t('accounts.keyStored') : `${service.where} · ${t('accounts.keyHint')}`}
      >
        <TextInput
          type="password"
          placeholder={connected ? '••••••••' : t('accounts.placeholder')}
          value={key}
          onChange={(e) => setKey(e.target.value)}
        />
      </Field>

      {/* Secondary on purpose: three cards means three equal choices, so no
          single one may claim the page's one accent button. */}
      <div className="flex flex-wrap items-center gap-3">
        <Button kind="secondary" onClick={onSave} disabled={!key.trim()}>
          {connected ? t('accounts.replace') : t('accounts.connect')}
        </Button>
        {connected && (
          <Button kind="secondary" onClick={onTest} disabled={testing}>
            {testing ? t('accounts.testing') : t('accounts.test')}
          </Button>
        )}
        {connected && !state?.fromEnv && (
          <Button kind="ghost" onClick={onRemove}>
            {t('accounts.disconnect')}
          </Button>
        )}
        {saved && <span className="text-statusOk text-sm">{t('accounts.saved')}</span>}
      </div>

      {state?.fromEnv && <p className="text-carbon-textMuted text-xs">{t('accounts.fromEnv')}</p>}

      {result && (
        <p className={`text-xs ${result.ok ? 'text-statusOk' : 'text-statusFail'}`}>
          {result.ok
            ? `${t('accounts.ok')} · ${result.hosts} ${t('accounts.hosts')}`
            : `${t('accounts.failed')} · ${result.detail}`}
        </p>
      )}
    </Card>
  );
}
