import { useState } from 'react';
import {
  Button,
  Card,
  Field,
  IconBadge,
  SectionTitle,
  TextInput,
} from '../../../components/ui';
import { QRCode } from '../../../components/QRCode';
import { ApiError, confirmTOTP, disableTOTP, setupTOTP, type TOTPEnrolment } from '../../../lib/api';
import { copyToClipboard } from '../../../lib/clipboard';
import { useT } from '../../../lib/i18n';
import { IconCheck, IconClipboard } from '../../../lib/icons';
import { useToast } from '../../../lib/toast';

/**
 * The second factor: a six-digit code from an authenticator app, beside the
 * password and never in its place.
 *
 * THE CARD RENDERS THE STEP IT IS ON, not every control at once with most of
 * them disabled. Off is one button. The enrolment is a code to scan, the same
 * secret in type-able form, and a field to prove the app took it. Then the
 * recovery codes, once. Then on, with the way out.
 *
 * Three rules this interface has to carry, because each has a failure that
 * looks like success:
 *
 *   - the status line reads the SERVER's answer, never the step this component
 *     is on. A half-finished enrolment that has shown a QR code and had no code
 *     confirmed still says off, because that is what the login will do;
 *   - the recovery codes are shown exactly once, so the card says so BEFORE it
 *     shows them and asks for an acknowledgement rather than a dismissal.
 *     Nothing else leaves that step;
 *   - turning it off costs the same proof as using it. Stricter than this app's
 *     ordinary "are you sure" windows and for a different reason: not regret,
 *     but a session somebody walked away from.
 *
 * And the one that has no equivalent in an app with user accounts: there is
 * nobody here to unlock anything. So the (i) on the title says, before anybody
 * arms this, what happens if the phone and the paper are both gone.
 */
export function TwoFactorCard({
  hue,
  /** Whether a password is set at all. Without one there is nothing for a
   *  second factor to be second to, and the server refuses the enrolment. */
  passwordSet,
  /** The server's answer, and the only thing the status line reads. */
  enabled,
  recoveryLeft,
  /** Called after the factor is armed or removed, so the page re-reads it. */
  onChanged,
}: {
  hue: number;
  passwordSet: boolean;
  enabled: boolean;
  recoveryLeft?: number;
  onChanged: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  // One state, three shapes, so the card cannot render two steps at once - the
  // failure a handful of independent booleans produces the first time two of
  // them are true.
  const [step, setStep] = useState<
    { kind: 'idle' } | ({ kind: 'scan' } & TOTPEnrolment) | { kind: 'codes'; codes: string[] }
  >({ kind: 'idle' });
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [disarming, setDisarming] = useState(false);
  const [copied, setCopied] = useState(false);
  // The failing button's own counter, keyed onto it, so a second identical
  // refusal builds a fresh node and shakes again.
  const [shake, setShake] = useState(0);

  /**
   * The toast, and the one decision in it worth writing down.
   *
   * A rejected code is the failure this card produces most often, and the
   * server answers it with an English sentence, because a server's errors are
   * diagnostics. So the ONE case this app has its own words for gets them -
   * `auth.twoFactor.codeWrong`, in the reader's language - and everything else
   * falls back to what the server said, which is better than a generic sentence
   * of ours that says less. The server's 401 is what distinguishes the two;
   * matching on the text would break the moment either side is reworded.
   *
   * That is the fallback rule right way round: the app's own copy first, the
   * server's string only where the app has nothing better.
   */
  function fail(e: unknown) {
    const rejected = e instanceof ApiError && e.status === 401;
    toast(rejected ? t('auth.twoFactor.codeWrong') : String(e).replace(/^Error:\s*/, ''), 'fail');
    setShake((n) => n + 1);
  }

  async function begin() {
    setBusy(true);
    try {
      setStep({ kind: 'scan', ...(await setupTOTP()) });
      setCode('');
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }

  async function confirm() {
    setBusy(true);
    try {
      setStep({ kind: 'codes', codes: await confirmTOTP(code) });
      setCode('');
      onChanged();
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }

  async function disable() {
    setBusy(true);
    try {
      await disableTOTP(code);
      setStep({ kind: 'idle' });
      setCode('');
      setDisarming(false);
      onChanged();
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  }

  async function copy(text: string) {
    if (await copyToClipboard(text)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('auth.twoFactor.hint')}>{t('auth.twoFactor.title')}</SectionTitle>

      {/* The status line, and it reads `enabled` - the server's word - rather
          than which step this component happens to be showing. */}
      <div className="flex items-center gap-2">
        <span
          className={`inline-block h-2 w-2 rounded-[var(--radius-pill)] ${
            enabled ? 'bg-statusOkSolid' : 'bg-carbon-textMuted'
          }`}
        />
        <span className="text-sm text-carbon-text">
          {enabled ? t('auth.twoFactor.on') : t('auth.twoFactor.off')}
        </span>
      </div>

      {!passwordSet && <p className="text-sm text-carbon-textSub">{t('auth.twoFactor.needsPassword')}</p>}

      {/* OFF. One button, and the sentence above it saying what the next screen
          will show - the "a secret shown once says so beforehand" rule, said
          before the enrolment rather than on the screen that is already too
          late to go back from. */}
      {passwordSet && !enabled && step.kind === 'idle' && (
        <div className="flex flex-col gap-3">
          <p className="text-sm text-carbon-textSub">{t('auth.twoFactor.beforeYouStart')}</p>
          <div>
            <Button
              key={shake}
              className={shake > 0 ? 'glim-shake' : ''}
              kind="secondary"
              hue={hue}
              disabled={busy}
              onClick={() => void begin()}
            >
              {t('auth.twoFactor.enable')}
            </Button>
          </div>
        </div>
      )}

      {/* Scan, then prove. Both on screen together, because the code has to be
          typed while the app is still open on the same secret. */}
      {step.kind === 'scan' && (
        <div className="flex flex-col gap-5">
          <p className="text-sm text-carbon-text">{t('auth.twoFactor.scan')}</p>
          {/* self-start, or the card's flex column stretches the code's white
              ground across the full width and leaves the modules in one corner
              of a white slab. Measured in the browser, not reasoned about: the
              wrapper is inline-block and looks safe, but a flex child stretches
              regardless of its own display. */}
          {step.qr && (
            <div className="self-start">
              <QRCode matrix={step.qr} label={t('auth.twoFactor.qrLabel')} />
            </div>
          )}
          <Field label={t('auth.twoFactor.secretManual')}>
            <div className="flex items-center gap-2">
              <div className="min-w-0 flex-1 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
                <code className="glim-num block overflow-x-auto whitespace-nowrap text-xs text-carbon-text" dir="ltr">
                  {step.secret}
                </code>
              </div>
              <IconBadge
                labelled
                hue={hue}
                icon={copied ? <IconCheck width={16} height={16} /> : <IconClipboard width={16} height={16} />}
                title={copied ? t('common.copied') : t('common.copy')}
                aria-label={copied ? t('common.copied') : t('common.copy')}
                onClick={() => void copy(step.secret)}
              />
            </div>
          </Field>
          <Field label={t('auth.twoFactor.confirmLabel')}>
            <TextInput
              value={code}
              onChange={(e) => setCode(e.target.value)}
              inputMode="numeric"
              autoComplete="one-time-code"
              placeholder="000000"
              dir="ltr"
              onKeyDown={(e) => {
                if (e.key === 'Enter' && code.trim() !== '' && !busy) void confirm();
              }}
            />
          </Field>
          {/* Cancel opens the row, the control that goes ahead ends it. */}
          <div className="flex items-center gap-3">
            <span className="flex-1" />
            <Button
              kind="ghost"
              onClick={() => {
                setStep({ kind: 'idle' });
                setCode('');
              }}
              disabled={busy}
            >
              {t('common.cancel')}
            </Button>
            <Button
              key={shake}
              className={shake > 0 ? 'glim-shake' : ''}
              kind="primary"
              disabled={busy || code.trim() === ''}
              onClick={() => void confirm()}
            >
              {t('auth.twoFactor.confirm')}
            </Button>
          </div>
        </div>
      )}

      {/* The codes, once. There is exactly one control out of this step and its
          label is an acknowledgement, not a dismissal: no cancel, nothing that
          advances on its own, and the card is armed already, so leaving by any
          other route would mean a locked instance with no way back. */}
      {step.kind === 'codes' && (
        <div className="flex flex-col gap-3">
          <p className="text-sm font-medium text-carbon-text">{t('auth.twoFactor.codesTitle')}</p>
          <p className="text-sm text-carbon-textSub">{t('auth.twoFactor.codesHint')}</p>
          <ul
            className="grid grid-cols-2 gap-x-6 gap-y-1 rounded-[var(--radius-control)] bg-carbon-surface2 p-4"
            dir="ltr"
          >
            {step.codes.map((c) => (
              <li key={c} className="glim-num text-sm text-carbon-text">
                {c}
              </li>
            ))}
          </ul>
          <div className="flex items-center gap-3">
            <Button
              kind="ghost"
              icon={copied ? <IconCheck width={16} height={16} /> : <IconClipboard width={16} height={16} />}
              onClick={() => void copy(step.codes.join('\n'))}
            >
              {copied ? t('common.copied') : t('common.copy')}
            </Button>
            <span className="flex-1" />
            <Button kind="primary" onClick={() => setStep({ kind: 'idle' })}>
              {t('auth.twoFactor.codesAck')}
            </Button>
          </div>
        </div>
      )}

      {/* ON. How much of the sheet is left, and the way out - which asks for a
          code before it does anything, so an unattended session cannot remove
          the protection it is sitting behind. */}
      {enabled && step.kind === 'idle' && (
        <div className="flex flex-col gap-3">
          {recoveryLeft !== undefined && (
            <p className="text-sm text-carbon-textSub">
              {t('auth.twoFactor.recoveryLeft').replace('{n}', String(recoveryLeft))}
            </p>
          )}
          {!disarming ? (
            <div>
              <Button kind="secondary" hue={hue} onClick={() => setDisarming(true)}>
                {t('auth.twoFactor.disable')}
              </Button>
            </div>
          ) : (
            <div className="flex flex-col gap-3">
              <Field label={t('auth.twoFactor.disablePrompt')}>
                <TextInput
                  autoFocus
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  placeholder="000000"
                  dir="ltr"
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && code.trim() !== '' && !busy) void disable();
                  }}
                />
              </Field>
              <div className="flex items-center gap-3">
                <span className="flex-1" />
                <Button
                  kind="ghost"
                  onClick={() => {
                    setDisarming(false);
                    setCode('');
                  }}
                  disabled={busy}
                >
                  {t('common.cancel')}
                </Button>
                <Button
                  key={shake}
                  className={shake > 0 ? 'glim-shake' : ''}
                  kind="primary"
                  disabled={busy || code.trim() === ''}
                  onClick={() => void disable()}
                >
                  {t('auth.twoFactor.disable')}
                </Button>
              </div>
            </div>
          )}
        </div>
      )}
    </Card>
  );
}
