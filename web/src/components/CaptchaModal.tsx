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
import { IconClock } from '../lib/icons';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';

// A hoster (or an account's own login gate) asking a human something before a
// download can continue - the prompt side of internal/captcha, mounted once
// (see Layout.tsx) so it is reachable from every route, the same always-
// visible pattern ToastProvider's own overlay already uses in this app.
//
// Two JD affordances the plan explicitly forbids, per build-plan.md section
// 8's Wave 7 note: no Buy-Premium button (no affiliate arrangement exists in
// this app) and no cancel-countdown-on-mouse-move (a NAS instance's viewer is
// not sitting at the machine) - replaced below with the countdown pausing
// while the answer field has keyboard focus.
//
// 'widget' is rendered from routes_captcha_widget.go's own page (7C's route,
// this wave) inside an <iframe>; this file only builds that URL from data it
// already holds and relays the page's postMessage answer back through
// answerCaptcha - it never runs a vendor's script itself. 'click' has no
// separate click-region data on the wire (ClickPayload is the identical type
// to ImagePayload - see internal/captcha/challenge.go's own doc comment), so
// the points a person clicks are collected here and encoded as JSON matching
// JD's own ClickedPoint / MultiClickedPoint shape - verified against JD's
// source (org.jdownloader.captcha.v2.challenge.clickcaptcha.ClickedPoint:
// {x:int,y:int}; .multiclickcaptcha.MultiClickedPoint: {x:int[],y:int[]}) -
// but WHICH of the two a given challenge is is not disclosed by Kind
// (jdKindByClass maps both ClickCaptchaChallenge and
// MultiClickCaptchaChallenge to the same KindClick), so this sends the
// single-point shape for exactly one clicked point and the array shape for
// more than one: the natural way a person answers each respectively (a
// single-click challenge visually asks for one thing; a multi-click one asks
// to mark several), not a verified disambiguator.

// GO_ZERO_YEAR mirrors format.ts's own constant: Go's encoding/json does not
// drop a zero time.Time on omitempty, so ExpiresAt arrives as
// "0001-01-01T00:00:00Z" rather than an absent field when the source could
// not say - never a real deadline, and never "already expired".
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

// pickCurrent mirrors internal/captcha.Store.List's own ordering (nearest
// expiry first, an unknown deadline last, ties on id) so which challenge
// this modal shows first agrees with what GET /api/captcha would have
// returned, whether this browser got here from that fetch or from a live
// "captcha" event.
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
  // Fractions (0..1) of the rendered image at click time, not raw pixels -
  // resize-safe for the marker overlay, converted to the image's own
  // natural pixel space only at submission time (see submitText).
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

  // Initial snapshot, then live over the hub - the same load()-then-
  // connectWS shape useTasks (lib/useTasks.ts) already uses. "snapshot" is
  // fired on every (re)connect, task-related, but it doubles here as the one
  // signal this file has that the socket just came back after a drop - a
  // captcha "removed"/"changed" broadcast missed during that gap would
  // otherwise leave this modal showing a challenge that already resolved,
  // with nothing to notice.
  useEffect(() => {
    let live = true;
    const applyList = (list: CaptchaChallenge[]) => {
      if (live) setChallenges(Object.fromEntries(list.map((c) => [c.id, c])));
    };
    fetchCaptchas().then(applyList);
    // 'snapshot' is not in kinds below and still arrives every time - see
    // lib/useTasks.ts's identical note on why (Hub.SendTo bypasses a
    // connection's own subscription filter).
    const close = connectWS(
      (type, data) => {
        if (type === 'snapshot') {
          fetchCaptchas().then(applyList);
        } else if (type === 'captcha') {
          const c = data as CaptchaChallenge;
          setChallenges((p) => ({ ...p, [c.id]: c }));
        } else if (type === 'captchaResolved') {
          const r = data as CaptchaResolution;
          setChallenges((p) => {
            if (!(r.id in p)) return p;
            const n = { ...p };
            delete n[r.id];
            return n;
          });
          // Only the ambient reasons: "solved"/"expired"/"aborted" are always
          // the direct result of a POST this browser (or the one that pressed
          // Continue/Cancel) just made and already has synchronous feedback
          // for - see handleContinue/handleSkip. Toasting those here too would
          // say the same thing twice. "timedOut"/"resolved" are the two this
          // file has no other way to learn about: nobody in this tab did
          // anything, and a modal that just silently vanishes reads as "did I
          // lose the download?" rather than as the captcha having lapsed.
          if (r.reason === 'timedOut') {
            // Styled as 'info', not 'fail' - nothing broke - but still a
            // critical kind: the download it was blocking is now stuck with
            // nobody having answered for it, which is exactly the outcome
            // quiet mode's CRITICAL table exists to never swallow.
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

  // Fresh local state whenever the displayed challenge changes - a stale
  // typed answer or clicked point must never survive onto a different
  // challenge's image.
  useEffect(() => {
    setAnswer('');
    setPoints([]);
    setWidgetStatus('loading');
    setFocused(false);
    setFrozenRemaining(null);
    setMoreOpen(false);
    setWidgetKey((k) => k + 1);
  }, [current?.id]);

  // The countdown's own clock - only runs while something with a real
  // deadline is on screen, one shared interval rather than a timer per
  // field.
  useEffect(() => {
    if (!current || expiryMs(current.expiresAt) === null) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [current?.id, current?.expiresAt]);

  // The widget page's own answer path back (routes_captcha_widget.go's doc
  // comment, "THE ANSWER PATH BACK"): a window.postMessage from the iframe,
  // checked against this page's own origin before anything in it is
  // trusted - the frame-ancestors CSP on that page only stops a foreign
  // page from embedding it, not this same-origin page from mishandling what
  // it receives.
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
      // The resolved challenge leaves `challenges` through the
      // "captchaResolved" broadcast this call also triggers server-side
      // (app_captcha.go's settleCaptcha), not by this handler patching state
      // itself - the same "wait for the hub, never patch locally" rule
      // every other mutation in this app already follows (lib/useTasks.ts).
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
          {showContinue && (
            <Button kind="primary" onClick={handleContinue} disabled={continueDisabled}>
              {t('captcha.continue')}
            </Button>
          )}
          <Button kind="secondary" onClick={() => handleSkip('skip-once')} disabled={busy}>
            {t('captcha.cancel')}
          </Button>
          <Button kind="ghost" onClick={handleRefresh} disabled={busy}>
            {t('captcha.refresh')}
          </Button>
          <span className="flex-1" />
          {displayRemaining !== null && (
            <span className="inline-flex shrink-0 items-center gap-1 text-[11px] text-carbon-textMuted">
              <IconClock width={12} height={12} />
              {fmtCountdown(displayRemaining)}
            </span>
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
                  className="pointer-events-none absolute h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-full bg-accent ring-2 ring-white"
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
