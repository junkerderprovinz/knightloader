import { useEffect, useState } from 'react';
import {
  type AuthState,
  type Settings,
  fetchAuth,
  fetchSettings,
  saveSettings,
  setPassword,
} from '../lib/api';
import { useResource } from '../lib/useResource';
import { useT } from '../lib/i18n';
import {
  PageHeader,
  Card,
  Button,
  Field,
  NumberInput,
  TextInput,
  TextArea,
  Toggle,
  SectionTitle,
  LoadingCard,
  ErrorCard,
} from '../components/ui';

export function SettingsPage() {
  const { t } = useT();
  const { data: cfg, setData: setCfg, loading, failed, reload } = useResource<Settings>(fetchSettings);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');

  async function onSave() {
    if (!cfg) return;
    setError('');
    try {
      const applied = await saveSettings(cfg);
      setCfg(applied);
      setSaved(true);
      setTimeout(() => setSaved(false), 1800);
    } catch (e) {
      setError(String(e));
    }
  }

  return (
    <div className="flex flex-col gap-6 max-w-2xl">
      <PageHeader title={t('settings.title')} subtitle={t('settings.subtitle')} />

      {loading && <LoadingCard label={t('common.loading')} />}
      {failed && <ErrorCard message={t('common.loadFailed')} retry={reload} retryLabel={t('common.retry')} />}

      {cfg && (
        <>
          <SectionTitle>{t('settings.sectionDownloads')}</SectionTitle>
          <Card className="flex flex-col gap-5">
            <Field label={t('settings.downloadDir')} hint={t('settings.downloadDirHint')}>
              <TextInput
                value={cfg.downloadDir}
                placeholder="/downloads"
                spellCheck={false}
                onChange={(e) => setCfg({ ...cfg, downloadDir: e.target.value })}
              />
            </Field>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <Field label={t('settings.maxConcurrent')}>
                <NumberInput
                  value={cfg.maxConcurrent}
                  min={1}
                  max={64}
                  onValue={(v) => setCfg({ ...cfg, maxConcurrent: v })}
                />
              </Field>
              <Field label={t('settings.maxPerHost')}>
                <NumberInput value={cfg.maxPerHost} min={1} max={64} onValue={(v) => setCfg({ ...cfg, maxPerHost: v })} />
              </Field>
              <Field label={t('settings.speedLimit')} hint={t('settings.speedHint')}>
                <NumberInput
                  value={Math.round(cfg.speedLimit / 1024)}
                  min={0}
                  step={256}
                  onValue={(v) => setCfg({ ...cfg, speedLimit: Math.max(0, v) * 1024 })}
                />
              </Field>
            </div>
            <Field label={t('settings.maxRetries')} hint={t('settings.maxRetriesHint')}>
              <NumberInput
                value={cfg.maxRetries}
                min={0}
                max={20}
                onValue={(v) => setCfg({ ...cfg, maxRetries: v })}
              />
            </Field>
            <div className="flex flex-col gap-3">
              <Toggle
                checked={cfg.subfolderByPackage}
                onChange={(v) => setCfg({ ...cfg, subfolderByPackage: v })}
                label={t('settings.subfolderByPackage')}
              />
              <Toggle
                checked={cfg.autoStart}
                onChange={(v) => setCfg({ ...cfg, autoStart: v })}
                label={t('settings.autoStart')}
              />
            </div>
          </Card>

          <SectionTitle>{t('settings.sectionArchives')}</SectionTitle>
          <Card className="flex flex-col gap-5">
            <div className="flex flex-col gap-3">
              <Toggle
                checked={cfg.extract}
                onChange={(v) => setCfg({ ...cfg, extract: v })}
                label={t('settings.extract')}
              />
              <Toggle
                checked={cfg.deleteArchive}
                onChange={(v) => setCfg({ ...cfg, deleteArchive: v })}
                label={t('settings.deleteArchive')}
              />
            </div>
            <Field label={t('settings.archivePasswords')} hint={t('settings.archivePasswordsHint')}>
              <TextArea
                rows={4}
                spellCheck={false}
                value={(cfg.archivePasswords ?? []).join('\n')}
                onChange={(e) =>
                  setCfg({ ...cfg, archivePasswords: e.target.value.split('\n').filter((p) => p.trim() !== '') })
                }
              />
            </Field>
          </Card>

          <div className="flex items-center gap-3">
            <Button onClick={onSave}>{t('settings.save')}</Button>
            {saved && <span className="text-statusOk text-sm">{t('settings.saved')}</span>}
            {error && <span className="text-statusFail text-sm">{error}</span>}
          </div>

          <SectionTitle>{t('settings.sectionSecurity')}</SectionTitle>
          <PasswordCard />
        </>
      )}
    </div>
  );
}

// PasswordCard owns the password lock. It saves on its own button rather than
// with the rest of the settings: a password is not a preference you change by
// accident while adjusting the speed limit.
function PasswordCard() {
  const { t } = useT();
  const [auth, setAuth] = useState<AuthState | null>(null);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    fetchAuth().then(setAuth).catch(() => setAuth(null));
  }, []);

  async function onApply() {
    setError('');
    try {
      setAuth(await setPassword(current, next));
      setCurrent('');
      setNext('');
      setDone(true);
      setTimeout(() => setDone(false), 1800);
    } catch (e) {
      setError(String(e).replace(/^Error:\s*/, ''));
    }
  }

  const locked = auth?.enabled ?? false;

  return (
    <Card className="flex flex-col gap-5">
      <p className={`text-sm ${locked ? 'text-statusOk' : 'text-carbon-textSub'}`}>
        {locked ? t('settings.lockOn') : t('settings.lockOff')}
      </p>
      {locked && (
        <Field label={t('settings.passwordCurrent')}>
          <TextInput type="password" value={current} onChange={(e) => setCurrent(e.target.value)} />
        </Field>
      )}
      <Field label={t('settings.passwordNew')} hint={t('settings.passwordHint')}>
        <TextInput type="password" value={next} onChange={(e) => setNext(e.target.value)} />
      </Field>
      <div className="flex items-center gap-3">
        <Button kind="secondary" onClick={onApply} disabled={locked ? current === '' : next === ''}>
          {next === '' && locked ? t('settings.removePassword') : t('settings.setPassword')}
        </Button>
        {done && <span className="text-statusOk text-sm">{t('settings.passwordSaved')}</span>}
        {error && <span className="text-statusFail text-sm">{error}</span>}
      </div>
    </Card>
  );
}
