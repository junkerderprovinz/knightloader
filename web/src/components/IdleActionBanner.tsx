import { useEffect, useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { cancelIdleAction, connectWS, fetchIdleAction, type IdleActionState } from '../lib/api';
import { Button } from './ui';
import { IconClock, IconCode, IconMoon, IconPause, IconPower, IconWarning } from '../lib/icons';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';

// IdleActionBanner is the courtesy half of build-plan.md's Wave 10B row: the
// countdown itself is entirely server-side (internal/idleaction.Controller),
// arms and fires whether or not anyone is looking at this tab, and survives a
// reload because GET /api/idle-action always answers with the same absolute
// FireAt the server is counting down to (see IdleActionState's own doc
// comment, lib/api.ts) rather than a duration this component would have to
// guess had already been ticking down unseen. This component exists only so
// a person who IS looking gets a chance to press Cancel.
//
// Mounted once in app/Layout.tsx, beside CaptchaModal and StatusStrip, for
// the identical reason those two are there: an idle countdown has nothing to
// do with which page happens to be open, and a copy mounted per page would
// remount - and re-fetch, and flicker - on every navigation.
//
// Renders nothing whenever nothing is armed, the same "renders nothing the
// instant there is nothing to show" rule StatusStrip and CaptchaModal both
// follow, so this is never a permanent fixture with "no countdown" written on
// it somewhere on screen.
//
// IT ALSO REPORTS A FAILED RUN, which is new and is the reason this file is
// not only a countdown any more. An end-of-queue action used to be a pause: it
// either happened or it did not, and either way the queue in front of the
// person said which. A command that could not start, or a machine that refused
// to sleep, says nothing anywhere - fireIdleAction only ever called log.Printf
// - so the one surface an action has would have gone on rendering nothing at
// exactly the moment there was something to say. See IdleRun in lib/api.ts.
//
// actionKey mirrors columns.tsx's own reasonKey: only the actions this build
// knows get a proper label; the server's `action` is an open string (a newer
// backend can arm one this build has never heard of), and that falls back to
// idleAction.actionFallback rather than a raw, untranslated token.
const actionKey: Record<string, TranslationKey> = {
  pause: 'idleAction.action.pause',
  quit: 'idleAction.action.quit',
  command: 'idleAction.action.command',
  suspend: 'idleAction.action.suspend',
};

/**
 * How a problem code becomes a sentence, shared with the settings card so
 * that one failure reads the same on both surfaces.
 *
 * The server sends a CODE and never a sentence, for the reason
 * reconnect.ConfigProblem's own comment sets out: translating on the server
 * would need the reader's language on every request and would write the log in
 * whichever language the last reader happened to prefer. So the code crosses
 * the wire and the words are picked here.
 *
 * Two codes get a second variant chosen from the deployment, because the same
 * fact needs a different second half: "that program is not here" inside a
 * container has to name what the image actually ships (yt-dlp, ffmpeg and a
 * JRE - no systemctl, no curl, no ssh, no sudo, no bash), and a refused sleep
 * is nearly always the policy manager rather than a broken call.
 */
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

// GO_ZERO_YEAR mirrors CaptchaModal's own constant: Go's encoding/json does
// not drop a zero time.Time, so an absent FireAt would arrive (if it ever
// did) as "0001-01-01T00:00:00Z" rather than simply missing. State.FireAt is
// `omitempty` on a *pointer*, so this is defensive rather than load-bearing
// today - kept for the same reason CaptchaModal keeps its own copy: a nil
// check alone is one refactor away from being wrong again.
const GO_ZERO_YEAR = 1;

function fireAtMs(iso: string | undefined): number | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= GO_ZERO_YEAR) return null;
  return d.getTime();
}

export function IdleActionBanner() {
  const { t } = useT();
  const { toast } = useToast();
  const [state, setState] = useState<IdleActionState | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [cancelling, setCancelling] = useState(false);
  // Which failure this viewer has already read, by its own instant. Held per
  // tab rather than on the server: a failed run is one fact and two people
  // looking at it have two separate "I have seen that" answers, and a shared
  // dismissal would take the notice off the other person's screen. A LATER
  // failure has a later instant and therefore appears again, which is the
  // whole reason this is the timestamp and not a boolean.
  const [dismissed, setDismissed] = useState<string | null>(null);

  // Initial snapshot, then live over the hub - the same load()-then-connectWS
  // shape every other always-mounted piece in this app uses (CaptchaModal,
  // StatusStrip). "snapshot" fires on every (re)connect; re-fetching on it is
  // what catches an arm/disarm broadcast missed during a dropped socket,
  // rather than this banner silently going stale until the next change.
  useEffect(() => {
    let live = true;
    const apply = (s: IdleActionState) => {
      if (live) setState(s);
    };
    fetchIdleAction().then(apply, () => {
      /* the banner simply does not appear; nothing here is worth a toast */
    });
    // 'snapshot' is not in kinds below and still arrives every time - see
    // lib/useTasks.ts's identical note on why (Hub.SendTo bypasses a
    // connection's own subscription filter).
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

  // The countdown's own clock - one shared interval, only while something
  // with a real deadline is on screen, the same rule CaptchaModal's
  // countdown already follows.
  useEffect(() => {
    if (!state?.armed || fireAtMs(state.fireAt) === null) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [state?.armed, state?.fireAt]);

  const failed = state?.lastRun && !state.lastRun.ok ? state.lastRun : null;

  // Nothing armed: the countdown is over or was never started, and the only
  // thing left worth a corner of the screen is a failure nobody has read yet.
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
            {/* No deployment is passed, on purpose: this banner is mounted on
                every page and fetching GET /api/system/deployment for a notice
                that is usually not shown would be a request on every load for
                nothing. The consequence is that a container gets the short
                "that program is not here" rather than the longer one naming
                what the image ships - and the link right below leads to the
                card that does have it. */}
            <span className="text-carbon-textSub">
              {idleProblemText(t, failed.problem, {
                program: failed.program,
                action: failed.action,
                code: failed.exitCode,
                output: failed.output,
              })}
            </span>
            <Link to="/settings/downloads" className="text-accent hover:underline">
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
      setState(await cancelIdleAction());
      // The hub broadcast this also triggers (app_idle.go's OnChange) would
      // patch every OTHER open tab; this tab's own click gets the answer
      // straight from the response instead of waiting for its own broadcast
      // to arrive, the same reason handleContinue in CaptchaModal does.
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
        {/* One glyph per action, each of them one this app already draws for
            the same idea, and the clock for an action this build has no icon
            for - the same fallback the label above it makes. */}
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
