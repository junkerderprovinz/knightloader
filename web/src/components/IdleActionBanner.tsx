import { useEffect, useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { cancelIdleAction, connectWS, fetchIdleAction, type IdleActionState } from '../lib/api';
import { Button } from './ui';
import { IconClock, IconCode, IconMoon, IconPause, IconPower, IconWarning } from '../lib/icons';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';

// An action from a newer server falls back to idleAction.actionFallback.
const actionKey: Record<string, TranslationKey> = {
  pause: 'idleAction.action.pause',
  quit: 'idleAction.action.quit',
  command: 'idleAction.action.command',
  suspend: 'idleAction.action.suspend',
};

// The server sends a problem code and the words are chosen here, in the
// reader's language.
const problemKey: Record<string, TranslationKey> = {
  empty: 'idleAction.problem.empty',
  notFound: 'idleAction.problem.notFound',
  notExecutable: 'idleAction.problem.notExecutable',
  permission: 'idleAction.problem.permission',
  timeout: 'idleAction.problem.timeout',
  exit: 'idleAction.problem.exit',
  notSupported: 'idleAction.problem.notSupported',
};

export interface IdleProblemVars {
  program?: string;
  deployment?: string;
  action?: string;
  code?: number;
  output?: string;
  seconds?: number;
}

/**
 * idleProblemText turns a failed run's problem code into a sentence, shared
 * with the settings card. A container gets its own "not found" text naming
 * what the image ships, and a refused suspend its own permission text.
 */
export function idleProblemText(
  t: (k: TranslationKey, vars?: Record<string, string | number>) => string,
  problem: string | undefined,
  vars: IdleProblemVars,
): string {
  if (!problem) return '';
  if (problem === 'permission' && vars.action === 'suspend') return t('idleAction.problem.suspendRefused');
  if (problem === 'notFound' && vars.deployment === 'container') {
    return t('idleAction.problem.notFoundContainer', { program: vars.program ?? '' });
  }
  const key = problemKey[problem];
  if (!key) return t('idleAction.runFailed');
  return t(key, {
    program: vars.program ?? '',
    path: vars.program ?? '',
    code: vars.code ?? 0,
    output: vars.output ?? '',
    seconds: vars.seconds ?? 0,
  });
}

const actionIcon: Record<string, ReactNode> = {
  pause: <IconPause width={15} height={15} />,
  quit: <IconPower width={15} height={15} />,
  command: <IconCode width={15} height={15} />,
  suspend: <IconMoon width={15} height={15} />,
};

function fmtCountdown(totalSeconds: number): string {
  const s = Math.max(0, totalSeconds);
  const m = Math.floor(s / 60);
  const rem = s % 60;
  if (m === 0) return `${rem}s`;
  return `${m}:${String(rem).padStart(2, '0')}`;
}

// Go's encoding/json writes a zero time.Time as year 1 rather than omitting it.
const GO_ZERO_YEAR = 1;

function fireAtMs(iso: string | undefined): number | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= GO_ZERO_YEAR) return null;
  return d.getTime();
}

/**
 * IdleActionBanner shows the server's end-of-queue countdown with a Cancel
 * button, and a failed run until it is dismissed. The countdown runs on the
 * server against an absolute FireAt, so it survives a reload.
 */
export function IdleActionBanner() {
  const { t } = useT();
  const { toast } = useToast();
  const [state, setState] = useState<IdleActionState | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [cancelling, setCancelling] = useState(false);
  // The dismissed failure's instant, per tab, so a later failure shows again.
  const [dismissed, setDismissed] = useState<string | null>(null);

  // "snapshot" arrives on every reconnect without a subscription (Hub.SendTo),
  // and refetching then catches a change missed while the socket was down.
  useEffect(() => {
    let live = true;
    const apply = (s: IdleActionState) => {
      if (live) setState(s);
    };
    fetchIdleAction().then(apply, () => {});
    const close = connectWS(
      (type, data) => {
        if (type === 'snapshot') {
          fetchIdleAction().then(apply, () => {});
        } else if (type === 'idleAction') {
          apply(data as IdleActionState);
        }
      },
      ['idleAction'],
    );
    return () => {
      live = false;
      close();
    };
  }, []);

  // Ticks only while a real deadline is on screen.
  useEffect(() => {
    if (!state?.armed || fireAtMs(state.fireAt) === null) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [state?.armed, state?.fireAt]);

  const failed = state?.lastRun && !state.lastRun.ok ? state.lastRun : null;

  if (!state?.armed) {
    if (!failed || dismissed === failed.at) return null;
    return (
      <div className="fixed bottom-5 left-5 z-40 max-w-sm">
        <div role="status" aria-live="polite" className="glim-card glim-fade flex items-start gap-3 px-4 py-3 text-xs">
          <span className="text-statusWarn" aria-hidden="true">
            <IconWarning width={15} height={15} />
          </span>
          <span className="flex flex-col gap-1">
            <span className="text-carbon-text">{t('idleAction.failedTitle')}</span>
            {/* No deployment, which would cost a request on every page load;
                the linked settings card gives the container-specific text. */}
            <span className="text-carbon-textSub">
              {idleProblemText(t, failed.problem, {
                program: failed.program,
                action: failed.action,
                code: failed.exitCode,
                output: failed.output,
              })}
            </span>
            <Link to="/settings/automation" className="text-accent hover:underline">
              {t('idleAction.openSettings')}
            </Link>
          </span>
          <Button kind="secondary" onClick={() => setDismissed(failed.at)} className="px-2.5 text-xs">
            {t('idleAction.dismiss')}
          </Button>
        </div>
      </div>
    );
  }

  const fireAt = fireAtMs(state.fireAt);
  const remaining = fireAt === null ? null : Math.max(0, Math.round((fireAt - now) / 1000));
  const key = state.action ? actionKey[state.action] : undefined;
  const actionLabel = key ? t(key) : t('idleAction.actionFallback', { action: state.action ?? '' });

  async function handleCancel() {
    setCancelling(true);
    try {
      // From the response, without waiting for the broadcast other tabs get.
      setState(await cancelIdleAction());
    } catch {
      toast(t('idleAction.cancelFailed'), 'fail');
    } finally {
      setCancelling(false);
    }
  }

  return (
    <div className="fixed bottom-5 left-5 z-40">
      <div
        role="status"
        aria-live="polite"
        className="glim-card glim-fade flex items-center gap-3 px-4 py-3 text-xs"
      >
        <span className="text-carbon-textMuted" aria-hidden="true">
          {actionIcon[state.action ?? ''] ?? <IconClock width={15} height={15} />}
        </span>
        <span className="flex flex-col gap-0.5">
          <span className="text-carbon-text">{t('idleAction.title')}</span>
          <span className="glim-num text-carbon-textSub">
            {actionLabel} {remaining !== null && t('idleAction.in', { countdown: fmtCountdown(remaining) })}
          </span>
        </span>
        <Button kind="secondary" onClick={handleCancel} disabled={cancelling} className="px-2.5 text-xs">
          {cancelling ? t('idleAction.cancelling') : t('idleAction.cancel')}
        </Button>
      </div>
    </div>
  );
}
