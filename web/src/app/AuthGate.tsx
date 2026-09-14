import { useEffect, useState, type ReactNode } from 'react';
import {
  fetchAuth,
  fetchPasskeys,
  login,
  passkeysInBrowser,
  signInWithPasskey,
  type AuthState,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { Button, Card, Field, PasswordInput, TextInput } from '../components/ui';
import { IconKey } from '../lib/icons';
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

/**
 * The sign-in screen, which is now two steps rather than one on an instance
 * with a second factor armed, plus the passkey door beside them.
 *
 * WHY THE CODE FIELD IS A SECOND STEP AND NOT A THIRD BOX ON THE FIRST. The
 * screen cannot know whether a code is wanted until the password has been
 * accepted - the server deliberately says nothing about an instance's defences
 * to somebody who has not got past the first one yet. So the password goes up
 * alone, and the answer decides whether there is a second question. Showing an
 * empty code box to everybody would ask most people for something they do not
 * have.
 */
function SignIn({ onSignedIn }: { onSignedIn: (a: AuthState) => void }) {
  const { t } = useT();
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  // 'password' or 'code': which question is on screen. Driven by the server's
  // answer, never guessed here.
  const [step, setStep] = useState<'password' | 'code'>('password');
  const [error, setError] = useState<'' | 'password' | 'code' | 'passkey'>('');
  const [busy, setBusy] = useState(false);
  // Whether a passkey could answer on THIS address: a key is bound to the
  // domain it was created for, so a button offered where none can answer is a
  // button that opens a browser prompt and fails.
  const [passkeyHere, setPasskeyHere] = useState(false);

  useEffect(() => {
    if (!passkeysInBrowser()) return;
    fetchPasskeys()
      .then((s) => setPasskeyHere(s.supported && s.here > 0))
      .catch(() => setPasskeyHere(false));
  }, []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      const res = await login(password, step === 'code' ? code : '');
      if (res.authenticated) {
        onSignedIn(res);
        return;
      }
      // Asked for a code, or told the one just sent was wrong. Both keep the
      // password: retyping it to fix a mistyped digit would be a punishment for
      // the wrong mistake.
      setStep('code');
      setCode('');
      if (res.codeRejected) setError('code');
    } catch {
      setError('password');
      setPassword('');
      setCode('');
      setStep('password');
    } finally {
      setBusy(false);
    }
  }

  async function withPasskey() {
    setBusy(true);
    setError('');
    try {
      onSignedIn(await signInWithPasskey());
    } catch {
      setError('passkey');
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
            <h1 className="text-sm font-semibold text-carbon-text">
              {step === 'code' ? t('auth.twoFactor.title') : t('auth.title')}
            </h1>
            <p className="text-carbon-textMuted mt-1 text-xs">
              {step === 'code' ? t('auth.twoFactor.signInHint') : t('auth.subtitle')}
            </p>
          </div>
          <form className="flex flex-col gap-5" onSubmit={submit}>
            {step === 'password' ? (
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
            ) : (
              <Field label={t('auth.twoFactor.confirmLabel')}>
                <TextInput
                  autoFocus
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  placeholder="000000"
                  dir="ltr"
                />
              </Field>
            )}
            <div className="flex items-center gap-3">
              <Button type="submit" disabled={busy || (step === 'code' ? code === '' : password === '')}>
                {step === 'code' ? t('auth.twoFactor.verify') : t('auth.signIn')}
              </Button>
              {error === 'password' && <span className="text-statusFail text-sm">{t('auth.wrong')}</span>}
              {error === 'code' && <span className="text-statusFail text-sm">{t('auth.twoFactor.codeWrong')}</span>}
              {error === 'passkey' && (
                <span className="text-statusFail text-sm">{t('auth.passkey.signInFailed')}</span>
              )}
            </div>
          </form>

          {/* The passkey door. Offered only where a registered key can actually
              answer on the address in the bar - never as a button that raises a
              prompt the browser will refuse. It sits under the password rather
              than above it because the password is the way in that always
              works; this is the other one. */}
          {passkeyHere && step === 'password' && (
            <div className="flex flex-col gap-3 border-t border-carbon-border/40 pt-4">
              <div>
                <Button
                  kind="secondary"
                  icon={<IconKey width={16} height={16} />}
                  disabled={busy}
                  onClick={() => void withPasskey()}
                >
                  {t('auth.passkey.signIn')}
                </Button>
              </div>
            </div>
          )}
        </Card>
      </div>
    </div>
  );
}
