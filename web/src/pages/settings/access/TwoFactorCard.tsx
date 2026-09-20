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
import { IconCheck, IconClipboard, IconShieldCheck } from '../../../lib/icons';
import { useToast } from '../../../lib/toast';

/**
 * TwoFactorCard sets up a six-digit authenticator code beside the password and
 * shows one step at a time: off, scan and confirm, the recovery codes once,
 * then on. The status line reads the server's answer, not the step, and turning
 * it off asks for a code so an unattended session cannot remove it.
 */
export function TwoFactorCard({
  hue,
  /** Whether a password is set; without one the server refuses the enrolment. */
  passwordSet,
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
  // One state, so the card cannot render two steps at once.
  const [step, setStep] = useState<
    { kind: 'idle' } | ({ kind: 'scan' } & TOTPEnrolment) | { kind: 'codes'; codes: string[] }
  >({ kind: 'idle' });
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [disarming, setDisarming] = useState(false);
  const [copied, setCopied] = useState(false);
  // Keyed onto the failing button so a repeated refusal shakes again.
  const [shake, setShake] = useState(0);

  /**
   * A rejected code (401) gets the translated `auth.twoFactor.codeWrong`;
   * anything else shows the server's message.
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

      {/* The sentence warns that the recovery codes are shown once, before the
          enrolment starts. The button shares its word with the passkey card's
          and is told apart by the shield. */}
      {passwordSet && !enabled && step.kind === 'idle' && (
        <div className="flex flex-col gap-3">
          <p className="text-sm text-carbon-textSub">{t('auth.twoFactor.beforeYouStart')}</p>
          <div>
            <Button
              key={shake}
              className={shake > 0 ? 'glim-shake' : ''}
              kind="secondary"
              hue={hue}
              icon={<IconShieldCheck width={16} height={16} />}
              disabled={busy}
              onClick={() => void begin()}
            >
              {t('auth.twoFactor.enable')}
            </Button>
          </div>
        </div>
      )}

      {/* Scan and confirm share the screen, since the code is typed while the
          app shows the new secret. */}
      {step.kind === 'scan' && (
        <div className="flex flex-col gap-5">
          <p className="text-sm text-carbon-text">{t('auth.twoFactor.scan')}</p>
          {/* self-start, or the flex column stretches the white ground across
              the card whatever the wrapper's display. */}
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

      {/* The factor is armed already, so the only way out is acknowledging
          that the codes were saved. */}
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

      {/* Turning it off asks for a code first. */}
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
