import { useState } from 'react';
import { ApiError, askForResetCode, resetPassword, type AuthState, type ResetCodePlace } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button, Field, PasswordInput, TextInput } from '../components/ui';

/**
 * The lengths the server takes, counted the way it counts them, in bytes. It
 * refuses the others in English.
 */
const MIN_PASSWORD = 8;
const MAX_PASSWORD = 72;

function fits(password: string): boolean {
  const bytes = new TextEncoder().encode(password).length;
  return bytes >= MIN_PASSWORD && bytes <= MAX_PASSWORD;
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

type Failure = '' | 'mismatch' | 'length' | 'rejected' | 'wait' | 'failed';

const FAILURE: Record<Exclude<Failure, ''>, TranslationKey> = {
  mismatch: 'auth.reset.mismatch',
  length: 'auth.reset.length',
  rejected: 'auth.reset.rejected',
  wait: 'auth.reset.wait',
  failed: 'auth.reset.failed',
};

/**
 * PasswordReset takes the code the server wrote where only somebody with
 * access to the machine can read it, and a new password. The caller asks for
 * the code before showing this, so the form never asks for one that does not
 * exist.
 */
export function PasswordReset({
  place,
  onPlace,
  onDone,
  onBack,
}: {
  place: ResetCodePlace;
  /** Called when the server names the place again after a refused code. */
  onPlace: (p: ResetCodePlace) => void;
  /** Called once the password is set. The answer already carries the session. */
  onDone: (a: AuthState) => void;
  onBack: () => void;
}) {
  const { t } = useT();
  const [code, setCode] = useState('');
  const [password, setPassword] = useState('');
  const [again, setAgain] = useState('');
  const [failure, setFailure] = useState<Failure>('');
  const [shake, setShake] = useState(0);
  const [busy, setBusy] = useState(false);

  function fail(f: Failure) {
    setFailure(f);
    setShake((n) => n + 1);
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    if (password !== again) return fail('mismatch');
    if (!fits(password)) return fail('length');
    setBusy(true);
    setFailure('');
    try {
      onDone(await resetPassword(code, password));
    } catch (err) {
      const status = err instanceof ApiError ? err.status : undefined;
      if (status === 401) {
        // A refusal does not say whether the code was mistyped or has run
        // out. Asking again writes a new one in the second case and nothing
        // in the first, so the sentence below is true either way.
        setCode('');
        await askForResetCode()
          .then(onPlace)
          .catch(() => undefined);
        fail('rejected');
      } else if (status === 429) {
        fail('wait');
      } else if (status === 400) {
        fail('length');
      } else {
        fail('failed');
      }
    } finally {
      setBusy(false);
    }
  }

  const where = [
    t(WHERE[place.place], { file: place.file ?? '' }),
    place.dir ? t('auth.reset.where.folder', { dir: place.dir }) : '',
    t('auth.reset.effect'),
  ];

  return (
    <form className="flex flex-col gap-5" onSubmit={(e) => void submit(e)}>
      <Field label={t(CODE_LABEL[place.place])} hint={where}>
        <TextInput
          autoFocus
          value={code}
          onChange={(e) => setCode(e.target.value)}
          autoComplete="one-time-code"
          autoCapitalize="characters"
          spellCheck={false}
          placeholder="XXXX-XXXX"
          dir="ltr"
          className="uppercase"
        />
      </Field>
      <Field label={t('settings.passwordNew')}>
        <PasswordInput
          value={password}
          onChange={setPassword}
          autoComplete="new-password"
          showLabel={t('common.showPassword')}
          hideLabel={t('common.hidePassword')}
        />
      </Field>
      <Field label={t('auth.reset.repeat')}>
        <PasswordInput
          value={again}
          onChange={setAgain}
          autoComplete="new-password"
          showLabel={t('common.showPassword')}
          hideLabel={t('common.hidePassword')}
        />
      </Field>
      {failure && (
        <p role="alert" className="text-statusFail text-sm">
          {t(FAILURE[failure])}
        </p>
      )}
      <div className="flex items-center justify-between gap-3">
        <Button type="button" kind="secondary" disabled={busy} onClick={onBack}>
          {t('onboarding.back')}
        </Button>
        <Button type="submit" shake={shake} disabled={busy || code.trim() === '' || password === ''}>
          {t('settings.setPassword')}
        </Button>
      </div>
    </form>
  );
}
