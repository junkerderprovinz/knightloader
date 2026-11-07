import { useEffect, useState, type ReactNode } from 'react';
import {
  ApiError,
  askForResetCode,
  fetchAuth,
  fetchPasskeys,
  login,
  onUnauthorized,
  passkeysInBrowser,
  resetPassword,
  signInWithPasskey,
  type AuthState,
  type ResetCodePlace,
  type ResetRemoved,
} from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button } from '../components/ui';
import { SignInCard } from '../components/SignInCard';
import { IconForward } from '../lib/icons';
import { LENGTH_PROBLEM, lengthProblem, refusedLength } from '../lib/passwordLength';
import type { SignInText } from '../lib/signIn';
import logoUrl from '../assets/logo.svg';

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

  // A session can end under an open page: somebody signed out everywhere,
  // changed the password or reset it. The next refused call brings the
  // sign-in screen back, once the server confirms the refusal was its own.
  useEffect(
    () =>
      onUnauthorized(() => {
        fetchAuth()
          .then((a) => {
            if (a.enabled && !a.authenticated) setAuth(a);
          })
          .catch(() => undefined);
      }),
    [],
  );

  if (!auth) return null;
  if (auth.enabled && !auth.authenticated) return <SignIn onSignedIn={setAuth} />;
  return <>{children}</>;
}

const CODE_LABEL: Record<ResetCodePlace['place'], TranslationKey> = {
  container: 'auth.reset.code.container',
  process: 'auth.reset.code.process',
  file: 'auth.reset.code.file',
};

const WHERE: Record<ResetCodePlace['place'], TranslationKey> = {
  container: 'auth.reset.where.container',
  process: 'auth.reset.where.process',
  file: 'auth.reset.where.file',
};

/** status is the HTTP status of a refused call, when it was one. */
function status(e: unknown): number | undefined {
  return e instanceof ApiError ? e.status : undefined;
}

/**
 * The sign-in screen on GlimStone's sign-in card. The code is a second step
 * because the server only says whether a second factor is armed once the
 * password has been accepted, and a reset asks the server to write a code
 * where only somebody with access to the machine can read it.
 */
function SignIn({ onSignedIn }: { onSignedIn: (a: AuthState) => void }) {
  const { t } = useT();
  // A passkey is bound to its domain, so the button only appears where a key
  // registered for this address exists.
  const [passkeyHere, setPasskeyHere] = useState(false);
  // Where the server wrote the reset code, which names the field and its (i).
  const [place, setPlace] = useState<ResetCodePlace>({ place: 'container' });
  // The answer to a reset that went through, held until the reader has seen
  // what it removed.
  const [done, setDone] = useState<(AuthState & { removed?: ResetRemoved }) | null>(null);

  useEffect(() => {
    if (!passkeysInBrowser()) return;
    fetchPasskeys()
      .then((s) => setPasskeyHere(s.supported && s.here > 0))
      .catch(() => setPasskeyHere(false));
  }, []);

  const text: SignInText = {
    password: t('auth.password'),
    signIn: t('auth.signIn'),
    passkey: t('auth.passkey.signIn'),
    forgot: t('auth.forgot'),
    code: t('auth.twoFactor.confirmLabel'),
    codeHint: t('auth.twoFactor.signInHint'),
    codeInvalid: t('auth.twoFactor.codeWrong'),
    back: t('onboarding.back'),
    resetCode: t(CODE_LABEL[place.place]),
    resetCodeTip: [
      t(WHERE[place.place], { file: place.file ?? '' }),
      place.dir ? t('auth.reset.where.folder', { dir: place.dir }) : '',
      t('auth.reset.effect'),
    ]
      .filter(Boolean)
      .join(' '),
    newPassword: t('settings.passwordNew'),
    confirmPassword: t('auth.reset.repeat'),
    setPassword: t('settings.setPassword'),
    mismatch: t('auth.reset.mismatch'),
    resetRejected: t('auth.reset.rejected'),
    failed: t('common.loadFailed'),
    show: t('common.showPassword'),
    hide: t('common.hidePassword'),
  };

  return (
    <SignInCard
      logo={logoUrl}
      appName="KnightLoader"
      text={text}
      passkey={passkeyHere}
      reset
      passwordProblem={(p) => {
        const problem = lengthProblem(p);
        return problem ? t(LENGTH_PROBLEM[problem]) : null;
      }}
      step={done ? <ResetDone removed={done.removed} onContinue={() => onSignedIn(done)} /> : undefined}
      onSignIn={async ({ password, code }) => {
        try {
          const res = await login(password, code ?? '');
          if (res.authenticated) {
            onSignedIn(res);
            return { ok: true };
          }
          return { needCode: true, error: res.codeRejected ? t('auth.twoFactor.codeWrong') : undefined };
        } catch (e) {
          // The throttle refuses the right password too, so it says to wait
          // rather than that the password is wrong.
          return { error: status(e) === 429 ? t('auth.tooMany') : t('auth.wrong') };
        }
      }}
      onPasskey={async () => {
        try {
          onSignedIn(await signInWithPasskey());
          return { ok: true };
        } catch (e) {
          return { error: status(e) === 429 ? t('auth.tooMany') : t('auth.passkey.signInFailed') };
        }
      }}
      onAskCode={async () => {
        try {
          setPlace(await askForResetCode());
          return { ok: true };
        } catch (e) {
          const unavailable = e instanceof ApiError && e.code === 'resetUnavailable';
          return { error: unavailable ? t('auth.reset.unavailable') : t('common.loadFailed') };
        }
      }}
      onSetPassword={async ({ code, password }) => {
        try {
          setDone(await resetPassword(code, password));
          return { ok: true };
        } catch (e) {
          // A refusal does not say whether the code was mistyped or has run
          // out; the card asks for a code again, which writes a new one only
          // in the second case.
          if (status(e) === 401) return { rejected: true };
          if (status(e) === 429) return { error: t('auth.tooMany') };
          const length = status(e) === 400 ? refusedLength(e) : undefined;
          return { error: t(length ? LENGTH_PROBLEM[length] : 'auth.reset.failed') };
        }
      }}
    />
  );
}

/** ResetDone lists what the reset took away before the app opens. */
function ResetDone({ removed, onContinue }: { removed?: ResetRemoved; onContinue: () => void }) {
  const { t } = useT();
  const lines = [
    removed?.twoFactor && t('auth.reset.removed.twoFactor'),
    removed?.passkeys ? t('auth.reset.removed.passkeys', { count: removed.passkeys }) : '',
    removed?.tokens ? t('auth.reset.removed.tokens', { count: removed.tokens }) : '',
    t('auth.reset.removed.sessions'),
    removed?.group && t('auth.reset.removed.group'),
  ].filter((line): line is string => Boolean(line));
  return (
    <div role="status" className="flex flex-col gap-4 text-sm text-carbon-text">
      <p>{t('auth.reset.done')}</p>
      <ul className="flex list-disc flex-col gap-2 ps-5 text-carbon-textSub">
        {lines.map((line) => (
          <li key={line}>{line}</li>
        ))}
      </ul>
      <Button icon={<IconForward />} className="w-full" autoFocus onClick={onContinue}>
        {t('auth.reset.continue')}
      </Button>
    </div>
  );
}
