import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Button } from '../components/ui';
import { dismissPopup, onLinkPopup, openFromPopup, popupNow, type PopupBatch } from '../lib/desktop';
import { useT } from '../lib/i18n';
import { IconClose } from '../lib/icons';
import { LOGO_SCOPE_ID, logoInline } from '../lib/logoInline';

/** The least the popup stays once the pointer leaves, so it does not vanish
 *  under a pointer that only brushed past the end of its time. */
const AFTER_HOVER_MS = 1500;

/** How long the leaving animation may take before the window goes anyway, for
 *  a page whose stylesheet never ends it. */
export const LEAVE_MS = 800;

/** The leaving animations: the way out, and the fade that replaces it under reduced motion. */
const LEAVING = new Set(['kl-popup-away', 'kl-popup-out']);

/** The sparks the knight's sword throws off, by angle. */
const SPARKS = [-160, -125, -90, -55, -20];

/**
 * The desktop app's popup for links the clipboard watch took into the
 * collector. It slides in from the edge of the screen it sits at, the knight
 * celebrates the catch, further copies bump the count of the same popup, and
 * it leaves the way it came once its time is up. The time stands still while
 * the pointer is on it, a click opens the collector and the cross only closes
 * it. desktop/linkpopup.go decides when it shows, where and for how long; the
 * window never takes the focus.
 */
export function LinkPopup() {
  const { t } = useT();
  const [batch, setBatch] = useState<PopupBatch | null>(null);
  const [leaving, setLeaving] = useState(false);
  const shown = useRef<PopupBatch | null>(null);
  shown.current = batch;
  const hovered = useRef(false);
  const clock = useRef<{ timer?: ReturnType<typeof setTimeout>; left: number; since: number }>({ left: 0, since: 0 });
  // A theme or language picked in the main window waits for the popup to go.
  const stale = useRef(false);

  useLayoutEffect(() => {
    document.documentElement.classList.add('kl-popup-page');
  }, []);

  // A popup that stays until clicked has no time to run.
  const run = useCallback(() => {
    const c = clock.current;
    clearTimeout(c.timer);
    if (!shown.current?.seconds) return;
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
    if (!shown.current) return;
    setLeaving(false);
    clock.current.left = shown.current.seconds * 1000;
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
  const close = () => {
    clearTimeout(clock.current.timer);
    setLeaving(true);
  };
  const grown = batch.seq !== batch.from;

  return (
    <div className={`kl-popup-stage kl-popup-at-${batch.place}`}>
      <div
        key={batch.from}
        className={`kl-popup${leaving ? ' kl-popup-leave' : ''}`}
        onMouseEnter={enter}
        onMouseLeave={leave}
        onAnimationEnd={(e) => {
          if (e.target === e.currentTarget && LEAVING.has(e.animationName)) finish();
        }}
      >
        <button type="button" className="kl-popup-open" onClick={open}>
          {/* Keyed on the take-in, so the ring answers each copy. */}
          <span key={batch.seq} className={`kl-popup-badge${grown ? ' kl-popup-again' : ''}`} aria-hidden="true">
            <span id={LOGO_SCOPE_ID} className="kl-popup-knight" dangerouslySetInnerHTML={{ __html: logoInline }} />
            {!grown &&
              SPARKS.map((angle, i) => (
                <span
                  key={angle}
                  className="kl-popup-spark"
                  style={{ ['--spark-a' as string]: `${angle}deg`, ['--spark-i' as string]: i }}
                />
              ))}
          </span>
          <span className="flex min-w-0 flex-col items-start gap-0.5 text-start">
            <span key={batch.seq} className={`text-body font-semibold text-carbon-text${grown ? ' kl-popup-bump' : ''}`}>
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
        <Button
          kind="secondary"
          className="kl-popup-close"
          icon={<IconClose width={14} height={14} />}
          aria-label={t('common.dismiss')}
          onClick={close}
        />
      </div>
    </div>
  );
}
