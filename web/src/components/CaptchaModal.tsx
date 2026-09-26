import { useEffect, useMemo, useRef, useState } from 'react';
import {
  answerCaptcha,
  captchaWidgetUrl,
  connectWS,
  fetchCaptchas,
  refreshCaptchas,
  skipCaptcha,
  type CaptchaAbortScope,
  type CaptchaChallenge,
  type CaptchaImagePayload,
  type CaptchaResolution,
  type CaptchaSolverRefusal,
  type CaptchaSolverReport,
  type CaptchaUnsupportedPayload,
  type CaptchaWidgetPayload,
} from '../lib/api';
import { Button, InfoBubble, Modal, TextInput } from './ui';
import { IconChevronDown, IconClock, IconClose } from '../lib/icons';
import { isDesktop } from '../lib/desktop';
import { useT, type TranslationKey } from '../lib/i18n';
import { captchaIsNew, forgetCaptcha, seedCaptchasSeen } from '../lib/notify';
import { useToast } from '../lib/toast';
import { isOnTop } from '../lib/windowStack';

// The captcha prompt for internal/captcha, mounted once in Layout.tsx. The
// countdown pauses while the answer field has focus. A 'widget' challenge runs
// in an iframe of routes_captcha_widget.go's page, which posts its answer back;
// this file never runs a vendor script.
//
// A 'click' answer uses JD's shapes: ClickedPoint {x:int,y:int} for one point
// and MultiClickedPoint {x:int[],y:int[]} for several. Kind does not say which
// JD expects, so the number of clicked points decides.
//
// The socket reports whether this tab is in the foreground: with "only when
// nobody is watching" on, the paid solvers wait while it is, for every captcha
// but a Turnstile, which nobody can answer here. In the desktop app the shell
// reports its window instead, since a webview's visibilityState is not
// reliable.

// Go's encoding/json writes a zero time.Time as year 1 rather than omitting it.
const GO_ZERO_YEAR = 1;

function expiryMs(iso: string | undefined): number | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= GO_ZERO_YEAR) return null;
  return d.getTime();
}

function fmtCountdown(totalSeconds: number): string {
  const m = Math.floor(totalSeconds / 60);
  const s = totalSeconds % 60;
  return `${m}:${String(s).padStart(2, '0')}`;
}

// pickCurrent orders like internal/captcha.Store.List: nearest expiry first,
// unknown deadlines last, ties on id.
function pickCurrent(challenges: Record<string, CaptchaChallenge>): CaptchaChallenge | undefined {
  const list = Object.values(challenges);
  list.sort((a, b) => {
    const ea = expiryMs(a.expiresAt);
    const eb = expiryMs(b.expiresAt);
    if (ea === null && eb === null) return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
    if (ea === null) return 1;
    if (eb === null) return -1;
    if (ea !== eb) return ea - eb;
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  });
  return list[0];
}

// The widget page's error details that mean the vendor was never reached, as
// opposed to a code the vendor sent back.
const UNREACHABLE = ['script', 'timeout', 'network'];

// Why the widget page gave up on a challenge before loading anything.
const UNSOLVABLE_WHY: Partial<Record<string, TranslationKey>> = {
  vendor: 'captcha.unsolvableVendor',
  turnstile: 'captcha.unsolvableTurnstile',
  action: 'captcha.unsolvableAction',
};

interface ClickPoint {
  // Fractions of the rendered image, converted to natural pixels on submit.
  xFrac: number;
  yFrac: number;
}

/**
 * SolverStatus says what the paid solvers are doing with the challenge on
 * screen: waiting for the person watching, solving it, or done without an
 * answer, with each solver's reason in the bubble. The why speaks to somebody
 * who can answer the challenge, so it is left out where nobody can.
 */
export function SolverStatus({
  report,
  now,
  answerable,
}: {
  report: CaptchaSolverReport;
  now: number;
  answerable: boolean;
}) {
  const { t } = useT();

  function line(r: CaptchaSolverRefusal): string {
    if (r.code === 'unsupported') return t('captcha.solverUnsupported', { solver: r.solver });
    if (r.code === 'noAnswer') return t('captcha.solverNoAnswer', { solver: r.solver });
    if (r.code === 'failed') return t('captcha.solverFailed', { solver: r.solver });
    const reason = r.detail ? `${r.detail} (${r.code})` : r.code;
    return t(r.taken ? 'captcha.solverGaveUp' : 'captcha.solverRefused', { solver: r.solver, reason });
  }

  // The solvers stop at one that may hold the task, so there is one at most.
  const taken = report.refusals?.find((r) => r.taken);
  let text: string;
  let hint: string | undefined;
  if (report.state === 'waiting') {
    const until = expiryMs(report.until);
    const left = until === null ? 0 : Math.max(0, Math.round((until - now) / 1000));
    text = t('captcha.solverWaiting', { time: fmtCountdown(left) });
    hint = t('captcha.solverWaitingHint');
  } else if (report.state === 'solving') {
    text = t('captcha.solverSolving', { solver: report.solver ?? '?' });
    hint = t('captcha.solverSolvingHint');
  } else if (taken) {
    text = t('captcha.solverStoppedTaken', { solver: taken.solver });
    hint = t('captcha.solverNotPassedOn', { solver: taken.solver });
  } else {
    text = t('captcha.solverStopped');
  }
  if (!answerable) hint = undefined;
  const lines = (report.refusals ?? []).map(line);

  return (
    <p className="flex items-center gap-1.5 text-[11px] text-carbon-textMuted">
      <span dir="auto">{text}</span>
      {(hint || lines.length > 0) && (
        <InfoBubble
          label={text}
          tip={
            <span className="flex flex-col gap-1.5">
              {hint && <span>{hint}</span>}
              {lines.map((l, i) => (
                <span key={i} dir="auto">
                  {l}
                </span>
              ))}
            </span>
          }
        />
      )}
    </p>
  );
}

export function CaptchaModal() {
  const { t, lang } = useT();
  const { toast } = useToast();

  const [challenges, setChallenges] = useState<Record<string, CaptchaChallenge>>({});
  const [now, setNow] = useState(() => Date.now());
  const [answer, setAnswer] = useState('');
  const [points, setPoints] = useState<ClickPoint[]>([]);
  const [focused, setFocused] = useState(false);
  const [frozenRemaining, setFrozenRemaining] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [widgetStatus, setWidgetStatus] = useState<'loading' | 'ready' | 'expired' | 'error' | 'unsolvable'>(
    'loading',
  );
  const [widgetError, setWidgetError] = useState<string | null>(null);
  const [widgetKey, setWidgetKey] = useState(0);
  const imgRef = useRef<HTMLImageElement>(null);
  const answerRef = useRef<HTMLInputElement>(null);

  const current = useMemo(() => pickCurrent(challenges), [challenges]);
  const moreWaiting = Math.max(0, Object.keys(challenges).length - (current ? 1 : 0));

  // "snapshot" arrives on every reconnect without a subscription (Hub.SendTo),
  // and refetching then drops challenges resolved while the socket was down.
  useEffect(() => {
    let live = true;
    const applyList = (list: CaptchaChallenge[]) => {
      // Pending challenges are not arrivals. Seeded regardless of `live`, since
      // the set is module-scoped and must survive a StrictMode remount.
      seedCaptchasSeen(list.map((c) => c.id));
      if (live) setChallenges(Object.fromEntries(list.map((c) => [c.id, c])));
    };
    fetchCaptchas().then(applyList);
    const close = connectWS(
      (type, data) => {
        if (type === 'snapshot') {
          fetchCaptchas().then(applyList);
        } else if (type === 'captcha') {
          const c = data as CaptchaChallenge;
          setChallenges((p) => ({ ...p, [c.id]: c }));
          // The server also broadcasts changed challenges every two seconds,
          // so only a new id notifies.
          if (captchaIsNew(c.id)) toast(t('captcha.waiting', { host: c.host || '?' }), 'info', 'captcha-needs-answer');
        } else if (type === 'captchaResolved') {
          const r = data as CaptchaResolution;
          forgetCaptcha(r.id);
          setChallenges((p) => {
            if (!(r.id in p)) return p;
            const n = { ...p };
            delete n[r.id];
            return n;
          });
          // Solved, expired and aborted follow a click that already gave
          // feedback; only a timeout or a resolution elsewhere needs a word.
          if (r.reason === 'timedOut') {
            // 'info' styling, but a critical kind that quiet mode never hides:
            // the download is now stuck.
            toast(t('captcha.timedOut', { host: r.host }), 'info', 'captcha-failed');
          } else if (r.reason === 'resolved') {
            toast(t('captcha.resolvedElsewhere', { host: r.host }), 'info', 'captcha-resolved');
          }
        }
      },
      ['captcha', 'captchaResolved'],
      !isDesktop(),
    );
    return () => {
      live = false;
      close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // A typed answer or clicked point must not carry over to another challenge.
  useEffect(() => {
    setAnswer('');
    setPoints([]);
    setWidgetStatus('loading');
    setWidgetError(null);
    setFocused(false);
    setFrozenRemaining(null);
    setMoreOpen(false);
    setWidgetKey((k) => k + 1);
  }, [current?.id]);

  // A captcha arrives by itself, possibly under a window somebody is typing in,
  // such as the folder chooser or the command palette. The answer box takes the
  // focus only where this window is the top one.
  useEffect(() => {
    const el = answerRef.current;
    if (el && isOnTop(el)) el.focus();
  }, [current?.id, current?.kind]);

  // Ticks only while a real deadline is on screen: the captcha's own, or when
  // a waiting solver takes over.
  const solverWaiting = current?.solver?.state === 'waiting';
  useEffect(() => {
    if (!current || (expiryMs(current.expiresAt) === null && !solverWaiting)) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [current?.id, current?.expiresAt, solverWaiting]);

  // The widget answers by postMessage, trusted only from this origin: the
  // page's frame-ancestors CSP does not protect what this side receives.
  useEffect(() => {
    if (!current || current.kind !== 'widget') return;
    const id = current.id;
    function onMessage(e: MessageEvent) {
      if (e.origin !== window.location.origin) return;
      const d = e.data as { source?: string; id?: string; kind?: string; detail?: string } | null;
      if (!d || d.source !== 'knightloader-captcha-widget' || d.id !== id) return;
      if (d.kind === 'ready') setWidgetStatus('ready');
      else if (d.kind === 'expired') setWidgetStatus('expired');
      else if (d.kind === 'error' || d.kind === 'unsolvable') {
        setWidgetStatus(d.kind);
        setWidgetError(d.detail ?? null);
      } else if (d.kind === 'solved' && d.detail) {
        answerCaptcha(id, d.detail).then(
          ({ stillValid }) => {
            if (!stillValid) toast(t('captcha.tooLate'), 'fail', 'captcha-failed');
          },
          () => toast(t('captcha.networkError'), 'fail', 'captcha-failed'),
        );
      }
    }
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [current?.id, current?.kind, toast, t]);

  if (!current) return null;

  const rawRemaining = (() => {
    const em = expiryMs(current.expiresAt);
    return em === null ? null : Math.max(0, Math.round((em - now) / 1000));
  })();
  const displayRemaining = focused && frozenRemaining !== null ? frozenRemaining : rawRemaining;

  function submitText(): string {
    if (current!.kind !== 'click') return answer;
    const img = imgRef.current;
    const w = img?.naturalWidth || 0;
    const h = img?.naturalHeight || 0;
    if (points.length <= 1) {
      const p = points[0];
      return JSON.stringify({ x: p ? Math.round(p.xFrac * w) : 0, y: p ? Math.round(p.yFrac * h) : 0 });
    }
    return JSON.stringify({
      x: points.map((p) => Math.round(p.xFrac * w)),
      y: points.map((p) => Math.round(p.yFrac * h)),
    });
  }

  async function handleContinue() {
    setBusy(true);
    try {
      const { stillValid } = await answerCaptcha(current!.id, submitText());
      if (!stillValid) toast(t('captcha.tooLate'), 'fail', 'captcha-failed');
      // The challenge leaves through the "captchaResolved" broadcast.
    } catch {
      toast(t('captcha.networkError'), 'fail', 'captcha-failed');
    } finally {
      setBusy(false);
    }
  }

  async function handleSkip(scope: CaptchaAbortScope = 'skip-once') {
    setBusy(true);
    setMoreOpen(false);
    try {
      await skipCaptcha(current!.id, scope);
    } catch {
      toast(t('captcha.networkError'), 'fail', 'captcha-failed');
    } finally {
      setBusy(false);
    }
  }

  async function handleRefresh() {
    setBusy(true);
    try {
      const list = await refreshCaptchas();
      setChallenges(Object.fromEntries(list.map((c) => [c.id, c])));
      setWidgetStatus('loading');
      setWidgetError(null);
      setWidgetKey((k) => k + 1);
    } catch {
      toast(t('captcha.networkError'), 'fail', 'captcha-failed');
    } finally {
      setBusy(false);
    }
  }

  const showContinue = current.kind === 'image' || current.kind === 'click';
  const continueDisabled = busy || (current.kind === 'image' ? answer.trim() === '' : points.length === 0);
  const title = moreWaiting > 0 ? t('captcha.titleMore', { n: moreWaiting }) : t('captcha.title');
  const widget = current.kind === 'widget' ? (current.payload as CaptchaWidgetPayload | undefined) : undefined;
  // The widget page renders reCAPTCHA and hCaptcha only, so a Turnstile is
  // left to the solvers without loading it.
  const turnstile = widget?.vendor === 'turnstile';
  const unsolvable = turnstile || widgetStatus === 'unsolvable';
  const why = turnstile
    ? 'captcha.unsolvableTurnstile'
    : widgetStatus === 'unsolvable' && widgetError
      ? UNSOLVABLE_WHY[widgetError]
      : undefined;
  // A score-based reCAPTCHA has nothing to click: the page asks for the token
  // itself.
  const widgetHint = widget?.v3Action ? t('captcha.widgetScoreHint') : t('captcha.widgetHint');
  const hint =
    current.kind === 'click'
      ? t('captcha.clickHint')
      : current.kind === 'widget' && !turnstile
        ? widgetHint
        : current.kind === 'unsupported' || turnstile
          ? t('captcha.unsupportedHint')
          : undefined;

  return (
    <Modal title={title} hint={hint} onClose={() => handleSkip('skip-once')}
      footer={
        <>
          {/* The forward button ends the row, so the clock goes first. */}
          {displayRemaining !== null && (
            <span className="inline-flex shrink-0 items-center gap-1 text-[11px] text-carbon-textMuted">
              <IconClock width={12} height={12} />
              {fmtCountdown(displayRemaining)}
            </span>
          )}
          <span className="flex-1" />
          <Button
            kind="secondary"
            labelled
            icon={<IconClose />}
            title={t('captcha.cancel')}
            onClick={() => handleSkip('skip-once')}
            disabled={busy}
          />
          {/* Refresh neither answers nor cancels, so it sits between them. */}
          <Button kind="ghost" onClick={handleRefresh} disabled={busy}>
            {t('captcha.refresh')}
          </Button>
          {showContinue && (
            <Button kind="primary" onClick={handleContinue} disabled={continueDisabled}>
              {t('captcha.continue')}
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-1">
        <p className="text-sm text-carbon-text" dir="auto">
          {t('captcha.forHost', { host: current.host || '?' })}
        </p>
        {current.prompt && (
          <p className="text-xs text-carbon-textSub" dir="auto">
            {current.prompt}
          </p>
        )}
        {current.solver && <SolverStatus report={current.solver} now={now} answerable={!unsolvable} />}
      </div>

      {current.kind === 'image' && (
        <div className="flex flex-col gap-3">
          <div className="flex justify-center overflow-hidden rounded-[var(--radius-control)] bg-white p-2">
            <img
              src={(current.payload as CaptchaImagePayload | undefined)?.dataUrl}
              alt={t('captcha.title')}
              className="max-w-full"
            />
          </div>
          <TextInput
            value={answer}
            onChange={(e) => setAnswer(e.target.value)}
            onFocus={() => {
              setFocused(true);
              setFrozenRemaining(rawRemaining);
            }}
            onBlur={() => setFocused(false)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !continueDisabled) handleContinue();
            }}
            placeholder={t('captcha.answerPlaceholder')}
            aria-label={t('captcha.answerLabel')}
            ref={answerRef}
          />
        </div>
      )}

      {current.kind === 'click' && (
        <div className="flex flex-col gap-2">
          <div className="flex justify-center overflow-hidden rounded-[var(--radius-control)] bg-white p-2">
            <div className="relative inline-block">
              <img
                ref={imgRef}
                src={(current.payload as CaptchaImagePayload | undefined)?.dataUrl}
                alt={t('captcha.title')}
                draggable={false}
                className="block max-w-full cursor-crosshair select-none"
                onClick={(e) => {
                  const rect = e.currentTarget.getBoundingClientRect();
                  setPoints((p) => [
                    ...p,
                    { xFrac: (e.clientX - rect.left) / rect.width, yFrac: (e.clientY - rect.top) / rect.height },
                  ]);
                }}
              />
              {points.map((p, i) => (
                <span
                  key={i}
                  className="pointer-events-none absolute h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-[var(--radius-pill)] bg-accent ring-2 ring-white"
                  style={{ left: `${p.xFrac * 100}%`, top: `${p.yFrac * 100}%` }}
                />
              ))}
            </div>
          </div>
          <div className="flex items-center gap-3">
            <span className="text-[11px] text-carbon-textMuted">{t('captcha.clickCount', { n: points.length })}</span>
            {points.length > 0 && (
              <Button kind="ghost" onClick={() => setPoints([])}>
                {t('captcha.clickClear')}
              </Button>
            )}
          </div>
        </div>
      )}

      {current.kind === 'widget' && widgetStatus !== 'error' && !unsolvable && (
        <div className="flex flex-col gap-2">
          <div className="overflow-hidden rounded-[var(--radius-control)] bg-white">
            <iframe
              key={widgetKey}
              src={captchaWidgetUrl(current, lang)}
              title={t('captcha.title')}
              className="h-72 w-full border-0"
              onLoad={() => setWidgetStatus((s) => (s === 'loading' ? 'ready' : s))}
            />
          </div>
          {widgetStatus === 'expired' && <p className="text-[11px] text-statusFail">{t('captcha.tooLate')}</p>}
        </div>
      )}

      {current.kind === 'widget' && widgetStatus === 'error' && !turnstile && (
        <p className="flex items-center gap-1.5 text-sm text-statusFail">
          {t('captcha.widgetUnavailable')}
          <InfoBubble
            tip={
              !widgetError || UNREACHABLE.includes(widgetError)
                ? t('captcha.widgetUnreachable')
                : t('captcha.widgetRefused', { code: widgetError })
            }
          />
        </p>
      )}

      {current.kind === 'widget' && unsolvable && (
        <p className="flex items-center gap-1.5 text-sm text-carbon-text">
          {t('captcha.unsolvable')}
          {why && <InfoBubble tip={t(why)} />}
        </p>
      )}

      {current.kind === 'unsupported' && (
        <p className="text-sm text-carbon-text">
          {t('captcha.unsupported', { vendor: (current.payload as CaptchaUnsupportedPayload | undefined)?.vendor || '?' })}
        </p>
      )}

      <div className="flex flex-col items-start gap-2">
        <Button
          kind="secondary"
          icon={<IconChevronDown className={moreOpen ? 'rotate-180' : ''} />}
          aria-expanded={moreOpen}
          onClick={() => setMoreOpen((v) => !v)}
        >
          {t('captcha.moreOptions')}
        </Button>
        {moreOpen && (
          <div className="flex flex-col items-start gap-2">
            <Button kind="secondary" disabled={busy} onClick={() => handleSkip('blacklist-hoster')}>
              {t('captcha.blockHoster', { host: current.host || '?' })}
            </Button>
            <Button kind="secondary" disabled={busy} onClick={() => handleSkip('blacklist-everywhere')}>
              {t('captcha.blockEverywhere')}
            </Button>
          </div>
        )}
      </div>
    </Modal>
  );
}
