import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { dismissPopup, onLinkPopup, openFromPopup, popupNow, type PopupBatch } from '../lib/desktop';
import { useT } from '../lib/i18n';
import { LOGO_SCOPE_ID, logoInline } from '../lib/logoInline';

/** How long the popup stays while nobody points at it. */
export const LINGER_MS = 5000;

/** The least it stays once the pointer leaves, so it does not vanish under a
 *  pointer that only brushed past the end of its time. */
const AFTER_HOVER_MS = 1500;

/** How long the leaving fade may take before the window goes anyway, for a
 *  page whose stylesheet never ends the fade. */
export const LEAVE_MS = 800;

/**
 * The desktop app's popup in a corner of the screen, for links the clipboard
 * watch took into the collector. The knight salutes each take-in, quick
 * copies add up in the one popup, a click opens the collector, and the time
 * stands still while the pointer is on it. desktop/linkpopup.go decides when
 * it shows; the window itself never takes the focus.
 */
export function LinkPopup() {
  const { t } = useT();
  const [batch, setBatch] = useState<PopupBatch | null>(null);
  const [leaving, setLeaving] = useState(false);
  const shown = useRef<PopupBatch | null>(null);
  shown.current = batch;
  const hovered = useRef(false);
  const clock = useRef<{ timer?: ReturnType<typeof setTimeout>; left: number; since: number }>({
    left: LINGER_MS,
    since: 0,
  });
  // A theme or language picked in the main window waits for the popup to go.
  const stale = useRef(false);

  useLayoutEffect(() => {
    document.documentElement.classList.add('kl-popup-page');
  }, []);

  const run = useCallback(() => {
    const c = clock.current;
    clearTimeout(c.timer);
    c.since = Date.now();
    c.timer = setTimeout(() => setLeaving(true), c.left);
  }, []);

  const pause = useCallback(() => {
    const c = clock.current;
    clearTimeout(c.timer);
    c.left = Math.max(0, c.left - (Date.now() - c.since));
  }, []);

  useEffect(() => {
    void popupNow()
      .then((b) => {
        if (b.seq) setBatch((now) => now ?? b);
      })
      .catch(() => {});
    const follow = () => {
      stale.current = true;
      if (!shown.current) window.location.reload();
    };
    window.addEventListener('storage', follow);
    const off = onLinkPopup(setBatch);
    return () => {
      off();
      window.removeEventListener('storage', follow);
      clearTimeout(clock.current.timer);
    };
  }, []);

  // Every take-in gives the popup its full time again.
  const seq = batch?.seq ?? 0;
  useEffect(() => {
    if (!seq) return;
    setLeaving(false);
    clock.current.left = LINGER_MS;
    if (!hovered.current) run();
  }, [seq, run]);

  const finish = useCallback(() => {
    const gone = shown.current;
    if (!gone) return;
    setBatch(null);
    setLeaving(false);
    void dismissPopup(gone.seq).catch(() => {});
    if (stale.current) window.location.reload();
  }, []);

  useEffect(() => {
    if (!leaving) return;
    const late = setTimeout(finish, LEAVE_MS);
    return () => clearTimeout(late);
  }, [leaving, finish]);

  if (!batch) return null;

  const enter = () => {
    hovered.current = true;
    pause();
    setLeaving(false);
  };
  const leave = () => {
    hovered.current = false;
    clock.current.left = Math.max(clock.current.left, AFTER_HOVER_MS);
    run();
  };
  const open = () => {
    clearTimeout(clock.current.timer);
    setBatch(null);
    void openFromPopup().catch(() => {});
  };

  return (
    <button
      key={batch.from}
      type="button"
      className={`kl-popup${leaving ? ' kl-popup-leave' : ''}`}
      onClick={open}
      onMouseEnter={enter}
      onMouseLeave={leave}
      onAnimationEnd={(e) => {
        if (e.target === e.currentTarget && e.animationName === 'kl-popup-out') finish();
      }}
    >
      {/* Keyed on the take-in, so the knight salutes each one. */}
      <span
        key={batch.seq}
        id={LOGO_SCOPE_ID}
        className="kl-popup-knight"
        aria-hidden="true"
        dangerouslySetInnerHTML={{ __html: logoInline }}
      />
      <span className="flex min-w-0 flex-col items-start gap-0.5 text-start">
        <span key={batch.seq} className="kl-count text-body font-semibold text-carbon-text">
          {batch.n === 1 ? t('linkPopup.caughtOne') : t('linkPopup.caught', { n: batch.n })}
        </span>
        {batch.host && (
          <span className="max-w-full truncate text-meta text-carbon-textSub">
            {t('linkPopup.from', { host: batch.host })}
          </span>
        )}
        <span className="text-meta text-carbon-textMuted">{t('linkPopup.open')}</span>
      </span>
    </button>
  );
}
