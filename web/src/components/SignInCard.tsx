import { useEffect, useId, useRef, useState, useSyncExternalStore, type FormEvent, type ReactNode } from 'react';
import {
  createSignInFlow,
  focusFirstField,
  focusRefused,
  resetBlocked,
  signInBlocked,
  signInStage,
  type SignInButton,
  type SignInHandlers,
  type SignInText,
} from '../lib/signIn';
import { IconBack, IconForgotPassword, IconKey, IconSignIn } from '../lib/icons';
import { Button, Card, InfoBubble, PasswordInput, TextInput } from './ui';

// The way into a locked instance (GlimStone 4.2.2, "The sign-in card"),
// ported from @glimstone/react onto KnightLoader's own controls.

export interface SignInCardProps extends SignInHandlers {
  /** The app's logo, as an address. It stands large above the name. */
  logo: string;
  appName: string;
  text: SignInText;
  /** Offers Sign in with a passkey. Pass it only where a passkey can answer on this address. */
  passkey?: boolean;
  /** Offers Forgot password? and the reset form. */
  reset?: boolean;
  /** What is wrong with a new password before it goes out, or null when nothing is. */
  passwordProblem: (password: string) => string | null;
  /** A step the app runs itself, in place of the card's forms. */
  step?: ReactNode;
}

export function SignInField({
  label,
  htmlFor,
  hint,
  children,
}: {
  label: string;
  htmlFor: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <div className="glim-signin-field">
      <label htmlFor={htmlFor} className="glim-signin-label">
        {label}
        {hint && <InfoBubble tip={hint} />}
      </label>
      {children}
    </div>
  );
}

export function SignInCard({
  logo,
  appName,
  text,
  passkey = false,
  reset = false,
  passwordProblem,
  step,
  ...handlers
}: SignInCardProps) {
  const latest = useRef({ ...handlers, text, passwordProblem });
  latest.current = { ...handlers, text, passwordProblem };
  const [flow] = useState(() => createSignInFlow(() => latest.current));
  const s = useSyncExternalStore(flow.subscribe, flow.state, flow.state);
  const ownId = useId();
  const id = (name: string) => `${ownId}-${name}`;
  const card = useRef<HTMLDivElement>(null);

  const stage = step ? 'step' : signInStage(s);
  useEffect(() => focusFirstField(card.current), [stage]);

  const refusal = s.refusal;
  useEffect(() => {
    if (refusal) focusRefused(card.current, refusal);
  }, [refusal?.count]);
  // The refused button shakes once per refusal; the others hold still.
  const shake = (button: SignInButton) => (refusal?.button === button ? refusal.count : 0);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void flow.submit();
  };
  const error = s.error && (
    <p className="glim-signin-error" role="alert">
      {s.error}
    </p>
  );
  const back = (
    <Button kind="secondary" icon={<IconBack />} type="button" data-signin="back" className="w-full" disabled={!!s.busy} onClick={flow.back}>
      {text.back}
    </Button>
  );

  return (
    <div className="glim-signin">
      <div ref={card} className="glim-signin-card-wrap">
        <Card className="glim-signin-card" padding="none">
          <div className="glim-signin-head">
            <span className="glim-signin-logo" aria-hidden="true">
              <img src={logo} alt="" draggable={false} />
            </span>
            <h1 className="glim-signin-name">{appName}</h1>
          </div>

          {step ??
            (s.view === 'reset' ? (
              <form className="glim-signin-form" noValidate onSubmit={submit}>
                <SignInField label={text.resetCode} htmlFor={id('reset-code')} hint={text.resetCodeTip}>
                  <TextInput
                    id={id('reset-code')}
                    data-signin-field="resetCode"
                    className="glim-signin-log-code"
                    value={s.resetCode}
                    onChange={(e) => flow.set('resetCode', e.target.value)}
                    autoComplete="one-time-code"
                    autoCapitalize="characters"
                    spellCheck={false}
                    placeholder="XXXX-XXXX-XXXX"
                    dir="ltr"
                  />
                </SignInField>
                <SignInField label={text.newPassword} htmlFor={id('new')}>
                  <PasswordInput
                    id={id('new')}
                    field="newPassword"
                    value={s.newPassword}
                    onChange={(v) => flow.set('newPassword', v)}
                    autoComplete="new-password"
                    showLabel={text.show}
                    hideLabel={text.hide}
                  />
                </SignInField>
                <SignInField label={text.confirmPassword} htmlFor={id('confirm')}>
                  <PasswordInput
                    id={id('confirm')}
                    field="confirm"
                    value={s.confirm}
                    onChange={(v) => flow.set('confirm', v)}
                    autoComplete="new-password"
                    showLabel={text.show}
                    hideLabel={text.hide}
                  />
                </SignInField>
                {error}
                {/* Stacked, since the longer translations of the two labels do not fit side by side. */}
                <Button
                  icon={<IconKey />}
                  type="submit"
                  data-signin="set-password" className="w-full"
                  shake={shake('set-password')}
                  disabled={resetBlocked(s)}
                >
                  {text.setPassword}
                </Button>
                {back}
              </form>
            ) : (
              <form className="glim-signin-form" noValidate onSubmit={submit}>
                {/* Kept while a passkey asks for the code, so the password survives a step back. */}
                <div className="glim-signin-field" hidden={s.viaPasskey}>
                  <label htmlFor={id('password')} className="glim-signin-label">
                    {text.password}
                  </label>
                  <PasswordInput
                    id={id('password')}
                    field="password"
                    value={s.password}
                    onChange={(v) => flow.set('password', v)}
                    autoComplete="current-password"
                    showLabel={text.show}
                    hideLabel={text.hide}
                  />
                </div>
                {s.needCode && (
                  <div className="glim-signin-field">
                    <label htmlFor={id('code')} className="glim-signin-label">
                      {text.code}
                    </label>
                    <TextInput
                      id={id('code')}
                      data-signin-field="code"
                      className="glim-signin-code"
                      value={s.code}
                      onChange={(e) => flow.set('code', e.target.value)}
                      inputMode="numeric"
                      autoComplete="one-time-code"
                      spellCheck={false}
                      placeholder="000000"
                      dir="ltr"
                    />
                    <p className="glim-signin-hint">{text.codeHint}</p>
                  </div>
                )}
                {error}
                <Button
                  icon={<IconSignIn />}
                  type="submit"
                  data-signin="submit" className="w-full"
                  shake={shake('submit')}
                  disabled={signInBlocked(s)}
                >
                  {text.signIn}
                </Button>
                {/* Left out rather than disabled where it cannot work: a disabled
                    control on a sign-in page reads as being locked out. */}
                {!s.needCode && passkey && (
                  <Button
                    kind="secondary"
                    icon={<IconKey />}
                    type="button"
                    data-signin="passkey" className="w-full"
                    shake={shake('passkey')}
                    disabled={!!s.busy}
                    onClick={() => void flow.passkey()}
                  >
                    {text.passkey}
                  </Button>
                )}
                {s.needCode
                  ? back
                  : reset && (
                      <Button
                        kind="secondary"
                        icon={<IconForgotPassword />}
                        type="button"
                        data-signin="forgot" className="w-full"
                        shake={shake('forgot')}
                        disabled={!!s.busy}
                        onClick={() => void flow.forgot()}
                      >
                        {text.forgot}
                      </Button>
                    )}
              </form>
            ))}
        </Card>
      </div>
    </div>
  );
}
