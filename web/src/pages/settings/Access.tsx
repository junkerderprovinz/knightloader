import { useEffect, useState } from 'react';
import { Button, Card, Field, InfoBubble, SectionTitle, TextInput } from '../../components/ui';
import { type AuthState, fetchAuth, setPassword } from '../../lib/api';
import { useT } from '../../lib/i18n';
import { useFeatures } from './context';
import { label, useTx } from './tx';

export function Access() {
  const { tx } = useTx();
  const { features } = useFeatures();

  // The listeners this instance answers on are a property of the build and the
  // environment, not of settings.json, so they are read out of the module
  // registry rather than described a second time here.
  const listeners = features.modules.filter((m) => m.page === 'access');

  return (
    <div className="flex flex-col gap-6">
      <PasswordCard />

      {listeners.length > 0 && (
        <>
          <SectionTitle>{tx('settings.sectionIntakePorts')}</SectionTitle>
          <Card className="flex flex-col gap-4">
            {listeners.map((m) => (
              <div key={m.id} className="flex items-baseline gap-3">
                <span className="flex items-center text-sm text-carbon-text">
                  {label(tx, 'settings.module.', m.id)}
                  {m.reason && <InfoBubble tip={m.reason} />}
                </span>
                <span className="flex-1" />
                <span className="text-[11px] text-carbon-textMuted" dir="ltr">
                  {m.detail || tx(m.enabled ? 'settings.modules.on' : 'settings.modules.off')}
                </span>
              </div>
            ))}
          </Card>
        </>
      )}
    </div>
  );
}

// PasswordCard owns the password lock. It saves on its own button rather than
// with the rest of the settings: a password is not a preference you change by
// accident while adjusting the speed limit, and it does not go through
// PUT /api/settings at all.
function PasswordCard() {
  const { t } = useT();
  const [auth, setAuth] = useState<AuthState | null>(null);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    fetchAuth()
      .then(setAuth)
      .catch(() => setAuth(null));
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
