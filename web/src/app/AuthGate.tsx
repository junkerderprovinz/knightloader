import { useEffect, useState, type ReactNode } from 'react';
import { type AuthState, fetchAuth, login } from '../lib/api';
import { useT } from '../lib/i18n';
import { Button, Card, Field, PasswordInput } from '../components/ui';
import { LanguagePicker } from '../components/LanguagePicker';

// AuthGate shows the app, or the sign-in screen when the instance is locked.
// While the state is still unknown it renders nothing, so a locked instance
// never flashes the UI it is supposed to be protecting.
export function AuthGate({ children }: { children: ReactNode }) {
  const [auth, setAuth] = useState<AuthState | null>(null);

  useEffect(() => {
    // An unreachable API is not a locked one: fall through to the app, which
    // has its own error states for that.
    fetchAuth()
      .then(setAuth)
      .catch(() => setAuth({ enabled: false, authenticated: true }));
  }, []);

  if (!auth) return null;
  if (auth.enabled && !auth.authenticated) return <SignIn onSignedIn={setAuth} />;
  return <>{children}</>;
}

function SignIn({ onSignedIn }: { onSignedIn: (a: AuthState) => void }) {
  const { t } = useT();
  const [password, setPassword] = useState('');
  const [error, setError] = useState(false);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(false);
    try {
      onSignedIn(await login(password));
    } catch {
      setError(true);
      setPassword('');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-carbon-background p-6">
      <div className="flex w-full max-w-sm flex-col gap-5">
        <div className="flex items-baseline gap-3">
          <span className="text-[22px] font-semibold tracking-tight text-carbon-text">KnightLoader</span>
          <span className="flex-1" />
          <LanguagePicker />
        </div>
        <Card className="flex flex-col gap-5">
          <div>
            <h1 className="text-sm font-semibold text-carbon-text">{t('auth.title')}</h1>
            <p className="text-carbon-textMuted mt-1 text-xs">{t('auth.subtitle')}</p>
          </div>
          <form className="flex flex-col gap-5" onSubmit={submit}>
            <Field label={t('auth.password')}>
              <PasswordInput
                value={password}
                onChange={setPassword}
                autoComplete="current-password"
                autoFocus
                showLabel={t('common.showPassword')}
                hideLabel={t('common.hidePassword')}
              />
            </Field>
            <div className="flex items-center gap-3">
              <Button type="submit" disabled={busy || password === ''}>
                {t('auth.signIn')}
              </Button>
              {error && <span className="text-statusFail text-sm">{t('auth.wrong')}</span>}
            </div>
          </form>
        </Card>
      </div>
    </div>
  );
}
