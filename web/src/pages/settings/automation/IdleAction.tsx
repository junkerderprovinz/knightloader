import { useEffect, useState, type ReactNode } from 'react';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  Modal,
  NumberInput,
  SectionTitle,
  TextArea,
  TextInput,
} from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { idleProblemText } from '../../../components/IdleActionBanner';
import {
  checkIdleCommand,
  fetchDeploymentInfo,
  fetchIdleAction,
  fetchIdleActions,
  runIdleCommand,
  type IdleCommandCheck,
  type IdleRun,
} from '../../../lib/api';
import { IconClock, IconClose, IconCode, IconMoon, IconPause, IconPower } from '../../../lib/icons';
import { fmtDate } from '../../../lib/format';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useDraft } from '../context';

// The idle action: what happens once the wait queue has nothing left, and how
// long the cancellable countdown runs first. The menu comes from the server,
// which offers only the actions this build can carry out.
//
// The stored command comes back masked, because the same redaction feeds the
// diagnostics bundle. The box stays empty while the draft keeps the mask, which
// the save merges back into the real value, as Reconnect.tsx does for the
// router password. The Check button prints what would really run.

/** The mask for a stored command; a protocol value shared with idleaction.RedactedCommand. */
const REDACTED = '********';

/** Menu labels; an id without one falls back to itself. */
const ACTION_KEYS: Record<string, TranslationKey> = {
  none: 'settings.downloads.idleActionNone',
  pause: 'settings.downloads.idleActionPause',
  quit: 'settings.downloads.idleActionQuit',
  command: 'settings.downloads.idleActionCommand',
  suspend: 'settings.downloads.idleActionSuspend',
};

/** The (i) beside the menu explains the selected action. */
const ACTION_HINTS: Record<string, TranslationKey> = {
  quit: 'settings.downloads.idleQuitHint',
  suspend: 'settings.downloads.idleSuspendHint',
};

const ACTION_ICONS: Record<string, ReactNode> = {
  pause: <IconPause width={16} height={16} />,
  quit: <IconPower width={16} height={16} />,
  command: <IconCode width={16} height={16} />,
  suspend: <IconMoon width={16} height={16} />,
};

/** The bounds of internal/idleaction, shown so a save does not change the number. */
const DELAY = { lo: 5, hi: 86400 };
const TIMEOUT = { lo: 5, hi: 3600 };

/**
 * useIdleActions is the menu the server offers, empty until it answers or when
 * it cannot, in which case the picker stays out rather than guessing.
 */
export function useIdleActions(): string[] {
  const [actions, setActions] = useState<string[]>([]);
  useEffect(() => {
    let live = true;
    void fetchIdleActions().then(
      (a) => {
        if (live) setActions(a);
      },
      () => {
        /* no menu rather than a guess at one */
      },
    );
    return () => {
      live = false;
    };
  }, []);
  return actions;
}

/**
 * IdleActionPicker is the choice of action as this card draws it. The shell
 * bar's quick settings show the same strip, so both places offer the same
 * menu and write the same value. The panel there is too narrow for the big
 * well, which would stack one action per line, so it asks for `sm`.
 */
export function IdleActionPicker({
  actions,
  value,
  onValue,
  size = 'md',
}: {
  actions: string[];
  value: string;
  onValue: (action: string) => void;
  size?: 'sm' | 'md';
}) {
  const { t } = useT();
  const hint = ACTION_HINTS[value];
  return (
    <FieldGroup
      layout="row"
      label={t('settings.downloads.idleAction')}
      hint={hint ? `${t('settings.downloads.idleActionHint')} ${t(hint)}` : t('settings.downloads.idleActionHint')}
    >
      <Tabs
        variant="well"
        size={size}
        labelled
        label={t('settings.downloads.idleAction')}
        active={value}
        onSelect={onValue}
        items={actions.map((id) => ({ id, label: ACTION_KEYS[id] ? t(ACTION_KEYS[id]) : id, icon: ACTION_ICONS[id] }))}
      />
    </FieldGroup>
  );
}

export function IdleActionCard({ hue }: { hue: number }) {
  const { t } = useT();
  // dirty gates the check and run buttons, which act on the stored command.
  const { cfg, patch, dirty } = useDraft();

  const actions = useIdleActions();
  const [deployment, setDeployment] = useState('');
  const [check, setCheck] = useState<IdleCommandCheck | null>(null);
  const [checking, setChecking] = useState(false);
  const [lastRun, setLastRun] = useState<IdleRun | null>(null);
  const [running, setRunning] = useState(false);
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    let live = true;
    void fetchDeploymentInfo().then(
      (d) => {
        if (live) setDeployment(d.deployment);
      },
      () => {
        /* the deployment sentence stays out */
      },
    );
    // The last run, once; IdleActionBanner holds the live subscription.
    void fetchIdleAction().then(
      (s) => {
        if (live) setLastRun(s.lastRun ?? null);
      },
      () => {
        /* no report rather than a guess at one */
      },
    );
    return () => {
      live = false;
    };
  }, []);

  const command = cfg.idleAction.command ?? { program: '', args: [], timeoutSeconds: 60 };
  const action = cfg.idleAction.action;
  const storedProgram = command.program === REDACTED;
  const storedArgs = (command.args ?? []).length > 0 && (command.args ?? []).every((a) => a === REDACTED);
  // A masked command is called "the stored command" in messages.
  const probeName = storedProgram ? t('settings.downloads.idleCommandStoredShort') : command.program;

  // Every write spreads, because a named nested field replaces the whole object.
  const setCommand = (fields: Partial<typeof command>) =>
    patch({ idleAction: { ...cfg.idleAction, command: { ...command, ...fields } } });

  const deploymentHint =
    deployment === 'desktop'
      ? t('settings.downloads.idleDeploymentDesktop')
      : deployment === 'container'
        ? t('settings.downloads.idleDeploymentContainer')
        : undefined;

  async function handleCheck() {
    setChecking(true);
    try {
      setCheck(await checkIdleCommand());
    } catch {
      setCheck(null);
    } finally {
      setChecking(false);
    }
  }

  async function handleRun() {
    setConfirming(false);
    setRunning(true);
    try {
      setLastRun(await runIdleCommand());
    } catch {
      // The button only shows when the route accepts it, so this is a request
      // that never arrived.
      setLastRun({ action: 'command', at: new Date().toISOString(), ok: false });
    } finally {
      setRunning(false);
    }
  }

  if (actions.length === 0) return null;

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={deploymentHint}>{t('settings.downloads.idleTitle')}</SectionTitle>

      <IdleActionPicker
        actions={actions}
        value={action}
        onValue={(id) => patch({ idleAction: { ...cfg.idleAction, action: id } })}
      />

      {action !== 'none' && (
        <Field
          label={t('settings.downloads.idleCountdown')}
          hint={`${t('settings.downloads.idleCountdownHint')} ${t('settings.downloads.idleArmHint')}`}
        >
          <NumberInput
            value={cfg.idleAction.delaySeconds}
            min={DELAY.lo}
            max={DELAY.hi}
            onValue={(v) => patch({ idleAction: { ...cfg.idleAction, delaySeconds: v } })}
          />
        </Field>
      )}

      {action === 'command' && (
        <>
          <Field
            label={t('settings.downloads.idleCommandProgram')}
            hint={`${t('settings.downloads.idleCommandProgramHint')} ${t('settings.downloads.idleCommandSecretHint')}`}
          >
            <TextInput
              value={storedProgram ? '' : command.program}
              placeholder={storedProgram ? t('settings.downloads.idleCommandStored') : '/usr/bin/systemctl'}
              spellCheck={false}
              dir="ltr"
              onChange={(e) => setCommand({ program: e.target.value })}
            />
          </Field>

          <Field label={t('settings.downloads.idleCommandArgs')} hint={t('settings.downloads.idleCommandArgsHint')}>
            <TextArea
              rows={3}
              spellCheck={false}
              dir="ltr"
              value={storedArgs ? '' : (command.args ?? []).join('\n')}
              placeholder={storedArgs ? t('settings.downloads.idleCommandStored') : 'suspend'}
              // Blank lines are dropped, as CommandSpec.Sanitize does.
              onChange={(e) => setCommand({ args: e.target.value.split('\n').filter((a) => a.trim() !== '') })}
            />
          </Field>

          <Field
            label={t('settings.downloads.idleCommandTimeout')}
            hint={t('settings.downloads.idleCommandTimeoutHint')}
          >
            <NumberInput
              value={command.timeoutSeconds}
              min={TIMEOUT.lo}
              max={TIMEOUT.hi}
              onValue={(v) => setCommand({ timeoutSeconds: v })}
            />
          </Field>

          <FieldGroup
            label={t('settings.downloads.idleCommandVerify')}
            hint={t('settings.downloads.idleCommandVerifyHint')}
          >
            <div className="flex flex-col gap-2">
              <div className="flex flex-wrap gap-2">
                <Button
                  kind="secondary"
                  className="w-fit"
                  disabled={checking || dirty}
                  onClick={() => void handleCheck()}
                >
                  {checking ? t('settings.downloads.idleCommandChecking') : t('settings.downloads.idleCommandCheck')}
                </Button>
                {/* Only for the command action; the run route answers 409
                    otherwise. */}
                <Button
                  kind="secondary"
                  className="w-fit"
                  disabled={running || dirty}
                  onClick={() => setConfirming(true)}
                >
                  {t('settings.downloads.idleCommandRun')}
                </Button>
              </div>

              {check && (
                <div className="glim-well flex flex-col gap-1 p-3 text-xs">
                  {check.problem ? (
                    <p className="text-statusWarn">
                      {idleProblemText(t, check.problem, { program: probeName, deployment })}
                    </p>
                  ) : (
                    <p className="text-carbon-textSub">
                      {t('settings.downloads.idleCommandCheckOk', {
                        path: check.resolvedPath ?? '',
                        argv: (check.argv ?? []).join(' '),
                      })}
                    </p>
                  )}
                </div>
              )}
            </div>
          </FieldGroup>
        </>
      )}

      {action !== 'none' && (
        <FieldGroup label={t('idleAction.lastRun')} hint={t('idleAction.lastRunHint')}>
          <div className="glim-well flex flex-col gap-1 p-3 text-xs">
            {!lastRun ? (
              <p className="text-carbon-textSub">{t('idleAction.lastRunNever')}</p>
            ) : lastRun.ok ? (
              <p className="text-carbon-textSub">{t('idleAction.lastRunOk', { at: fmtDate(lastRun.at) })}</p>
            ) : (
              <>
                <p className="text-statusWarn">{t('idleAction.lastRunFailed', { at: fmtDate(lastRun.at) })}</p>
                <p className="text-carbon-textSub">
                  {idleProblemText(t, lastRun.problem, {
                    program: lastRun.program ?? probeName,
                    deployment,
                    action: lastRun.action,
                    code: lastRun.exitCode,
                    output: lastRun.output,
                    seconds: command.timeoutSeconds,
                  })}
                </p>
              </>
            )}
          </div>
        </FieldGroup>
      )}

      {confirming && (
        <Modal
          title={t('settings.downloads.idleCommandRun')}
          onClose={() => setConfirming(false)}
          footer={
            <>
              {/* The spacer puts Run at the end of Modal's plain flex footer. */}
              <span className="flex-1" />
              <Button
                kind="ghost"
                labelled
                icon={<IconClose />}
                title={t('common.cancel')}
                onClick={() => setConfirming(false)}
              />
              <Button onClick={() => void handleRun()}>{t('settings.downloads.idleCommandRun')}</Button>
            </>
          }
        >
          <p className="text-sm text-carbon-textSub">
            {t('settings.downloads.idleCommandRunConfirm', { program: probeName })}
          </p>
        </Modal>
      )}
    </Card>
  );
}
