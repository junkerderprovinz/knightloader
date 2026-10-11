import { useState } from 'react';
import {
  ApiError,
  askForResetCode,
  resetPassword,
  type AuthState,
  type ResetCodePlace,
  type ResetRemoved,
} from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button, Field, PasswordInput, TextInput } from '../components/ui';
import { LENGTH_PROBLEM, lengthProblem, refusedLength } from '../lib/passwordLength';

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

type Failure = '' | 'mismatch' | 'short' | 'long' | 'rejected' | 'wait' | 'failed';

const FAILURE: Record<Exclude<Failure, ''>, TranslationKey> = {
  mismatch: 'auth.reset.mismatch',
  ...LENGTH_PROBLEM,
  rejected: 'auth.reset.rejected',
  wait: 'auth.tooMany',
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
  // The answer to a reset that went through, held until the reader has seen
  // what it removed.
  const [done, setDone] = useState<(AuthState & { removed?: ResetRemoved }) | null>(null);

  function fail(f: Failure) {
    setFailure(f);
    setShake((n) => n + 1);
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    if (password !== again) return fail('mismatch');
    const length = lengthProblem(password);
    if (length) return fail(length);
    setBusy(true);
    setFailure('');
    try {
      setDone(await resetPassword(code, password));
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
        fail(refusedLength(err) ?? 'failed');
      } else {
        fail('failed');
      }
    } finally {
      setBusy(false);
    }
  }

  if (done) return <ResetDone removed={done.removed} onContinue={() => onDone(done)} />;

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
          placeholder="XXXX-XXXX-XXXX"
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
      <div className="flex justify-end">
        <Button autoFocus onClick={onContinue}>
          {t('auth.reset.continue')}
        </Button>
      </div>
    </div>
  );
}
