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
import { IconClock, IconCode, IconMoon, IconPause, IconPower } from '../../../lib/icons';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useDraft } from '../context';

/**
 * What happens once the wait queue has nothing left to do, and how long the
 * cancellable countdown runs first.
 *
 * IT LEFT DownloadsSettings.tsx for the reason the other eight cards did: this
 * one grew from two controls to a menu, three command fields, a preflight, a
 * run button and a report, and a page that answers twelve questions in one
 * component is a file nobody can edit two things in at once.
 *
 * THE MENU IS THE SERVER'S. GET /api/idle-action/actions answers
 * idleaction.Offered(capabilities) - which actions this BUILD can actually
 * carry out, decided by which function fields are wired on the App and never
 * by which binary is running. So the container offers quit and not sleep, the
 * desktop offers sleep and not quit, and neither offers an entry that would do
 * nothing when it fired. Anything this build has no label for still renders as
 * its own id rather than as a blank tab, the same fallback IdleActionBanner
 * makes.
 *
 * THE STORED COMMAND IS NEVER RENDERED BACK. The server sends "********" in
 * place of it (idleaction.CommandSpec.Redacted) because Settings.Redacted also
 * feeds the diagnostics bundle, which is a file people attach to public bug
 * reports. The box therefore shows EMPTY with a placeholder saying so, while
 * the draft keeps the mask - which is what the save merges back into the real
 * value. That is the identical arrangement Reconnect.tsx uses for the router
 * password, including why: painting the mask into the field teaches people
 * their command is eight characters long, and clearing the draft on load would
 * wipe the stored command on the next save of any other setting on this page.
 * "What would actually run" is answered by the Check button instead, which
 * resolves the stored spec live and prints the real path and argument vector.
 */

/**
 * What the server sends instead of a stored command line. A protocol value
 * shared with idleaction.RedactedCommand, not a string somebody may prettify -
 * the same constant Reconnect.tsx and Advanced.tsx already keep for the same
 * reason.
 */
const REDACTED = '********';

/**
 * The menu labels. internal/idleaction.Actions() is the source of truth for
 * WHICH ids exist; this is only what each one reads as, and an id with no
 * entry here falls back to itself.
 */
const ACTION_KEYS: Record<string, TranslationKey> = {
  none: 'settings.downloads.idleActionNone',
  pause: 'settings.downloads.idleActionPause',
  quit: 'settings.downloads.idleActionQuit',
  command: 'settings.downloads.idleActionCommand',
  suspend: 'settings.downloads.idleActionSuspend',
};

/**
 * The sentence that explains the SELECTED action, shown in the (i) beside the
 * menu rather than as a paragraph under it. One bubble that changes with the
 * choice, instead of four blocks of text that are wrong three at a time.
 */
const ACTION_HINTS: Record<string, TranslationKey> = {
  quit: 'settings.downloads.idleQuitHint',
  suspend: 'settings.downloads.idleSuspendHint',
};

/** Every glyph is one this app already draws for the same idea. */
const ACTION_ICONS: Record<string, ReactNode> = {
  pause: <IconPause width={16} height={16} />,
  quit: <IconPower width={16} height={16} />,
  command: <IconCode width={16} height={16} />,
  suspend: <IconMoon width={16} height={16} />,
};

/** The server's own bounds (internal/idleaction), shown rather than enforced
 *  quietly, so nobody meets them by typing 3 and finding 60 after the save. */
const DELAY = { lo: 5, hi: 86400 };
const TIMEOUT = { lo: 5, hi: 3600 };

export function IdleActionCard({ hue }: { hue: number }) {
  const { t } = useT();
  // dirty gates the two buttons below. Both routes ask the server about what
  // is STORED - a preflight of the saved command, a run of the saved command -
  // so pressing either one with an unsaved edit on screen would answer a
  // question about a different command than the one being looked at, which is
  // the worst possible answer for a control whose whole job is telling you
  // what will really happen.
  const { cfg, patch, dirty } = useDraft();

  const [actions, setActions] = useState<string[]>([]);
  const [deployment, setDeployment] = useState('');
  const [check, setCheck] = useState<IdleCommandCheck | null>(null);
  const [checking, setChecking] = useState(false);
  const [lastRun, setLastRun] = useState<IdleRun | null>(null);
  const [running, setRunning] = useState(false);
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    let live = true;
    void fetchIdleActions().then(
      (a) => {
        if (live) setActions(a);
      },
      () => {
        /* the card stays out rather than offering a guess at the actions */
      },
    );
    void fetchDeploymentInfo().then(
      (d) => {
        if (live) setDeployment(d.deployment);
      },
      () => {
        /* the deployment sentence is simply not shown */
      },
    );
    // The last run, once. This card is a form and IdleActionBanner is the live
    // surface: it already holds the "idleAction" subscription and repaints on
    // every arm, cancel and run, so a second socket subscription here would
    // buy a repaint of a page somebody is editing rather than watching.
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
  // What to CALL the program in a sentence about it. The draft holds the mask
  // whenever something is stored, and "******** does not exist on this
  // instance" is a sentence about nothing - so a message about a hidden
  // command names it as such, and the Check button is what prints the real
  // path when somebody wants it. A run record carries its own unmasked
  // program and is preferred wherever there is one.
  const probeName = storedProgram ? t('settings.downloads.idleCommandStoredShort') : command.program;

  // EVERY write spreads. lib/api.ts spells out why: a nested object field is
  // replaced WHOLE when it is named, so a patch that rebuilt idleAction from
  // two fields would wipe the stored command off disk with no error anywhere.
  const setCommand = (fields: Partial<typeof command>) =>
    patch({ idleAction: { ...cfg.idleAction, command: { ...command, ...fields } } });

  const actionLabel = (id: string) => {
    const key = ACTION_KEYS[id];
    return key ? t(key) : id;
  };

  const deploymentHint =
    deployment === 'desktop'
      ? t('settings.downloads.idleDeploymentDesktop')
      : deployment === 'container'
        ? t('settings.downloads.idleDeploymentContainer')
        : undefined;

  const actionHint = ACTION_HINTS[action];

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
      // The route refuses with 409 while the configured action is not the
      // command one, which is a state this button is not offered in - so a
      // failure here is a request that never arrived, and the report line
      // says exactly that rather than inventing a problem code.
      setLastRun({ action: 'command', at: new Date().toISOString(), ok: false });
    } finally {
      setRunning(false);
    }
  }

  if (actions.length === 0) return null;

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={deploymentHint}>{t('settings.downloads.idleTitle')}</SectionTitle>

      <FieldGroup
        layout="row"
        label={t('settings.downloads.idleAction')}
        hint={actionHint ? `${t('settings.downloads.idleActionHint')} ${t(actionHint)}` : t('settings.downloads.idleActionHint')}
      >
        {/* The well track wraps rather than scrolling (Tabs.tsx), which is what
            keeps five entries with sentences for labels readable on a phone:
            they stack into rows instead of hiding behind a horizontal
            scrollbar. */}
        <Tabs
          variant="well"
          label={t('settings.downloads.idleAction')}
          active={action}
          onSelect={(id) => patch({ idleAction: { ...cfg.idleAction, action: id } })}
          items={actions.map((id) => ({ id, label: actionLabel(id), icon: ACTION_ICONS[id] }))}
        />
      </FieldGroup>

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
              // Only non-empty lines survive, matching CommandSpec.Sanitize:
              // a blank argument becomes an empty argv entry, which some
              // programs read as an empty positional and others as an error,
              // and neither is what a stray newline meant.
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
                {/* Offered only while the command action is the configured
                    one, because that is the only state the server accepts it
                    in: POST /api/idle-action/run answers 409 otherwise, and a
                    button that suspends the machine because THAT is what
                    happened to be configured is not a test. */}
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
              <p className="text-carbon-textSub">{t('idleAction.lastRunOk', { at: when(lastRun.at) })}</p>
            ) : (
              <>
                <p className="text-statusWarn">{t('idleAction.lastRunFailed', { at: when(lastRun.at) })}</p>
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
              <Button kind="ghost" onClick={() => setConfirming(false)}>
                {t('common.cancel')}
              </Button>
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

/**
 * The instant a run happened, in the reader's own locale. toLocaleString and
 * not a hand-rolled format: the server sends an absolute instant precisely so
 * that every client renders it in its own zone, the same reason
 * IdleActionBanner counts down from an absolute FireAt.
 */
function when(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString();
}
