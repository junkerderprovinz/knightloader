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
  type CaptchaUnsupportedPayload,
} from '../lib/api';
import { Button, Modal, TextInput } from './ui';
import { IconClock, IconClose } from '../lib/icons';
import { useT } from '../lib/i18n';
import { captchaIsNew, forgetCaptcha, seedCaptchasSeen } from '../lib/notify';
import { useToast } from '../lib/toast';

// The captcha prompt for internal/captcha, mounted once in Layout.tsx. The
// countdown pauses while the answer field has focus. A 'widget' challenge runs
// in an iframe of routes_captcha_widget.go's page, which posts its answer back;
// this file never runs a vendor script.
//
// A 'click' answer uses JD's shapes: ClickedPoint {x:int,y:int} for one point
// and MultiClickedPoint {x:int[],y:int[]} for several. Kind does not say which
// JD expects, so the number of clicked points decides.

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

interface ClickPoint {
  // Fractions of the rendered image, converted to natural pixels on submit.
  xFrac: number;
  yFrac: number;
}

export function CaptchaModal() {
  const { t } = useT();
  const { toast } = useToast();

  const [challenges, setChallenges] = useState<Record<string, CaptchaChallenge>>({});
  const [now, setNow] = useState(() => Date.now());
  const [answer, setAnswer] = useState('');
  const [points, setPoints] = useState<ClickPoint[]>([]);
  const [focused, setFocused] = useState(false);
  const [frozenRemaining, setFrozenRemaining] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [widgetStatus, setWidgetStatus] = useState<'loading' | 'ready' | 'expired' | 'error'>('loading');
  const [widgetKey, setWidgetKey] = useState(0);
  const imgRef = useRef<HTMLImageElement>(null);

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
    setFocused(false);
    setFrozenRemaining(null);
    setMoreOpen(false);
    setWidgetKey((k) => k + 1);
  }, [current?.id]);

  // Ticks only while a real deadline is on screen.
  useEffect(() => {
    if (!current || expiryMs(current.expiresAt) === null) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [current?.id, current?.expiresAt]);

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
      else if (d.kind === 'error') setWidgetStatus('error');
      else if (d.kind === 'solved' && d.detail) {
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

  return (
    <Modal title={title} onClose={() => handleSkip('skip-once')}
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
            autoFocus
          />
        </div>
      )}

      {current.kind === 'click' && (
        <div className="flex flex-col gap-2">
          <p className="text-[11px] text-carbon-textMuted">{t('captcha.clickHint')}</p>
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
          <div className="flex items-center gap-3 text-[11px] text-carbon-textMuted">
            <span>{t('captcha.clickCount', { n: points.length })}</span>
            {points.length > 0 && (
              <button
                type="button"
                className="underline-offset-2 hover:text-carbon-textSub hover:underline"
                onClick={() => setPoints([])}
              >
                {t('captcha.clickClear')}
              </button>
            )}
          </div>
        </div>
      )}

      {current.kind === 'widget' && (
        <div className="flex flex-col gap-2">
          <p className="text-[11px] text-carbon-textMuted">{t('captcha.widgetHint')}</p>
          <div className="overflow-hidden rounded-[var(--radius-control)] bg-white">
            <iframe
              key={widgetKey}
              src={captchaWidgetUrl(current)}
              title={t('captcha.title')}
              className="h-72 w-full border-0"
              onLoad={() => setWidgetStatus((s) => (s === 'loading' ? 'ready' : s))}
            />
          </div>
          {widgetStatus === 'expired' && <p className="text-[11px] text-statusFail">{t('captcha.tooLate')}</p>}
          {widgetStatus === 'error' && <p className="text-[11px] text-statusFail">{t('captcha.widgetUnavailable')}</p>}
        </div>
      )}

      {current.kind === 'unsupported' && (
        <div className="flex flex-col gap-1.5">
          <p className="text-sm text-carbon-text">
            {t('captcha.unsupported', { vendor: (current.payload as CaptchaUnsupportedPayload | undefined)?.vendor || '?' })}
          </p>
          <p className="text-xs text-carbon-textSub">{t('captcha.unsupportedHint')}</p>
        </div>
      )}

      <div className="flex flex-col items-start gap-1">
        <button
          type="button"
          className="text-[11px] text-carbon-textMuted underline-offset-2 hover:text-carbon-textSub hover:underline"
          onClick={() => setMoreOpen((v) => !v)}
        >
          {t('captcha.moreOptions')}
        </button>
        {moreOpen && (
          <div className="flex flex-col items-start gap-1 ps-0.5">
            <button
              type="button"
              className="text-[11px] text-carbon-textMuted hover:text-carbon-textSub"
              disabled={busy}
              onClick={() => handleSkip('blacklist-hoster')}
            >
              {t('captcha.blockHoster', { host: current.host || '?' })}
            </button>
            <button
              type="button"
              className="text-[11px] text-carbon-textMuted hover:text-carbon-textSub"
              disabled={busy}
              onClick={() => handleSkip('blacklist-everywhere')}
            >
              {t('captcha.blockEverywhere')}
            </button>
          </div>
        )}
      </div>
    </Modal>
  );
}
