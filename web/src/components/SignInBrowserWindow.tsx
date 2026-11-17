// The sign-in browser's window (internal/signinbrowser): a site's login page
// running in a browser on the server, for a container where the extension
// cannot reach. The page arrives as JPEG frames over a socket and the user's
// pointer and keys go back up it. KnightLoader keeps the session the browser
// ends up holding, never what was typed.
import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { createPortal } from 'react-dom';
import {
  type SiteLogin,
  beginSiteLogin,
  closeSignInBrowser,
  fetchSignInBrowser,
  fetchSiteLogins,
  openSignInBrowser,
} from '../lib/api';
import { openTab } from '../lib/external';
import { useT, type TranslationKey } from '../lib/i18n';
import { message } from '../lib/intake';
import { confirmInTab } from '../lib/siteLogins';
import {
  type PageInput,
  type PageStream,
  type Viewport,
  keyMessage,
  openPageStream,
  pointerMessage,
  viewportFor,
  wheelMessage,
} from '../lib/signInBrowser';
import { useToast } from '../lib/toast';
import { IconBack, IconBrowser, IconConfirm, IconExternalLink, IconRetry, IconSignIn } from '../lib/icons';
import { Button, Modal } from './ui';

type Notice =
  | { kind: 'blocked'; host: string }
  | { kind: 'elsewhere' }
  | { kind: 'signedIn'; account?: string }
  | { kind: 'refused' }
  | { kind: 'closed'; reason: string };

const CLOSED: Record<string, TranslationKey> = {
  idle: 'signInBrowser.closedIdle',
  elsewhere: 'signInBrowser.closedElsewhere',
};

const signedIn = (s: SiteLogin) => s.state === 'signedIn' || s.state === 'unchecked';

const mac = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform);

// Two presses this close in time and place are a double click, as a desktop
// counts them, since a pointerdown carries no count of its own.
const CLICK_MS = 500;
const CLICK_PX = 4;

// How often the window asks whether a sign-in made in the user's own browser
// has turned up, as the site's own window does.
const ELSEWHERE_POLL_MS = 3000;

/**
 * SignInBrowserWindow shows a site's login page from the server's browser.
 * onChanged hears how the site stands whenever that changes, and the window
 * closes itself with Done, storing the session.
 */
export function SignInBrowserWindow({
  site,
  hue,
  onChanged,
  onClose,
}: {
  site: SiteLogin;
  hue?: number;
  onChanged?: (s: SiteLogin) => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [phase, setPhase] = useState<'starting' | 'live' | 'failed' | 'closed'>('starting');
  const [error, setError] = useState('');
  const [host, setHost] = useState('');
  const [canGoBack, setCanGoBack] = useState(false);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [busy, setBusy] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [shown, setShown] = useState({ width: 0, height: 0 });
  const [elsewhere, setElsewhere] = useState(false);
  // The user's own browser helps only where this instance reads the sign-in
  // back from the browsers on its computer. A hoster's own page has no login
  // address and is signed in to here only.
  const ownBrowser = !!site.loginUrl && site.readsBrowsers;
  const tab = useRef<Window | null>(null);

  const panel = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const sink = useRef<HTMLTextAreaElement>(null);
  const socket = useRef<PageStream | null>(null);
  const vp = useRef<Viewport>({ width: 0, height: 0, scale: 1 });
  const heard = useRef({ onChanged, onClose });
  heard.current = { onChanged, onClose };

  const send = useCallback((input: PageInput) => socket.current?.send(input), []);

  // The frame is drawn as large as the panel allows without changing its
  // shape, which only differs from the panel where the page has a minimum size.
  const fit = useCallback(() => {
    const box = panel.current;
    if (!box || !vp.current.width) return;
    const scale = Math.min(box.clientWidth / vp.current.width, box.clientHeight / vp.current.height, 1);
    setShown({ width: vp.current.width * scale, height: vp.current.height * scale });
  }, []);

  useEffect(() => {
    const box = panel.current!;
    let cancelled = false;
    let stream: PageStream | null = null;
    setPhase('starting');
    setError('');
    setNotice(null);
    vp.current = viewportFor(box.clientWidth, box.clientHeight, window.devicePixelRatio || 1);
    fit();

    openSignInBrowser(site.id, vp.current.width, vp.current.height, vp.current.scale)
      .then(() => {
        if (cancelled) return;
        stream = openPageStream({
          frame: (jpeg) => {
            void createImageBitmap(jpeg).then((frame) => {
              const c = canvas.current;
              if (!c) return;
              if (c.width !== frame.width || c.height !== frame.height) {
                c.width = frame.width;
                c.height = frame.height;
              }
              c.getContext('2d')?.drawImage(frame, 0, 0);
              frame.close();
              setPhase((p) => (p === 'starting' ? 'live' : p));
            });
          },
          message: (raw) => {
            const msg = JSON.parse(raw) as { type: string; [k: string]: unknown };
            switch (msg.type) {
              case 'page':
                setHost(String(msg.host ?? ''));
                setCanGoBack(Boolean(msg.canGoBack));
                break;
              case 'blocked':
                setNotice({ kind: 'blocked', host: String(msg.host ?? '') });
                break;
              case 'refused':
                setNotice({ kind: 'refused' });
                break;
              case 'login': {
                const login = msg.login as SiteLogin;
                heard.current.onChanged?.(login);
                setNotice(signedIn(login) ? { kind: 'signedIn', account: login.account } : null);
                break;
              }
              case 'closed':
                if (msg.reason !== 'done') {
                  setPhase('closed');
                  setNotice({ kind: 'closed', reason: String(msg.reason ?? '') });
                }
                break;
            }
          },
          closed: () => {
            if (!cancelled) setPhase((p) => (p === 'failed' ? p : 'closed'));
          },
        });
        socket.current = stream;
      })
      .catch((e) => {
        if (cancelled) return;
        setPhase('failed');
        setError(message(e).replace(/^(Error|ApiError):\s*/, ''));
      });

    return () => {
      cancelled = true;
      stream?.close();
      socket.current = null;
    };
  }, [site.id, attempt, fit]);

  // The page follows the panel, so a window dragged larger shows more of it.
  useEffect(() => {
    const box = panel.current!;
    let timer = 0;
    const seen = new ResizeObserver(() => {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        const next = viewportFor(box.clientWidth, box.clientHeight, window.devicePixelRatio || 1);
        if (next.width !== vp.current.width || next.height !== vp.current.height || next.scale !== vp.current.scale) {
          vp.current = next;
          send({ type: 'resize', ...next });
        }
        fit();
      }, 200);
    });
    seen.observe(box);
    return () => {
      window.clearTimeout(timer);
      seen.disconnect();
    };
  }, [send, fit]);

  // A blocked page is said once and then gets out of the way.
  useEffect(() => {
    if (notice?.kind !== 'blocked') return;
    const timer = window.setTimeout(() => setNotice(null), 6000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  const finish = async () => {
    setBusy(true);
    socket.current?.close();
    try {
      const after = await closeSignInBrowser();
      if (after) {
        heard.current.onChanged?.(after);
        if (signedIn(after)) toast(t('siteLogins.signedInToast', { site: site.name }), 'ok');
      }
    } catch {
      // The window goes either way, and the row reads the state again.
    }
    heard.current.onClose();
  };

  // The site opens first, while the click still lets it. The wait that
  // begins makes the browsers here be read every few seconds and tells the
  // extension to hand the session over.
  const openElsewhere = () => {
    tab.current = openTab(site.loginUrl);
    setElsewhere(true);
    setNotice({ kind: 'elsewhere' });
    void beginSiteLogin(site.id).catch(() => {});
  };

  const finishing = useRef(finish);
  finishing.current = finish;
  useEffect(() => {
    if (!elsewhere) return;
    const timer = window.setInterval(() => {
      void fetchSiteLogins().then((all) => {
        const latest = all.find((s) => s.id === site.id);
        if (latest && signedIn(latest) && (latest.source === 'browser' || latest.source === 'extension')) {
          window.clearInterval(timer);
          confirmInTab(tab.current, site.name);
          void finishing.current();
        }
      }, () => {});
    }, ELSEWHERE_POLL_MS);
    return () => window.clearInterval(timer);
  }, [elsewhere, site.id, site.name]);

  const focusPage = () => sink.current?.focus({ preventScroll: true });

  const last = useRef({ at: 0, x: 0, y: 0, count: 0 });
  const moving = useRef<PageInput | null>(null);
  const touch = useRef<{ x: number; y: number; moved: boolean } | null>(null);

  const rect = () => canvas.current!.getBoundingClientRect();

  const onPointerDown = (e: ReactPointerEvent) => {
    if (phase !== 'live') return;
    e.preventDefault();
    focusPage();
    if (e.pointerType === 'touch') {
      touch.current = { x: e.clientX, y: e.clientY, moved: false };
      return;
    }
    const l = last.current;
    const near = e.timeStamp - l.at < CLICK_MS && Math.hypot(e.clientX - l.x, e.clientY - l.y) < CLICK_PX;
    last.current = { at: e.timeStamp, x: e.clientX, y: e.clientY, count: near ? Math.min(l.count + 1, 3) : 1 };
    send(pointerMessage('mousedown', e.nativeEvent, rect(), vp.current, last.current.count));
    // Held, so a drag that ends outside the frame still lets go in the page.
    e.currentTarget.setPointerCapture(e.pointerId);
  };

  const onPointerUp = (e: ReactPointerEvent) => {
    if (phase !== 'live') return;
    if (e.pointerType === 'touch') {
      // A tap is a click, and a drag was a scroll.
      const tap = touch.current && !touch.current.moved;
      touch.current = null;
      if (tap) {
        send(pointerMessage('mousedown', e.nativeEvent, rect(), vp.current, 1));
        send(pointerMessage('mouseup', e.nativeEvent, rect(), vp.current, 1));
      }
      return;
    }
    send(pointerMessage('mouseup', e.nativeEvent, rect(), vp.current, last.current.count));
  };

  const onPointerMove = (e: ReactPointerEvent) => {
    if (phase !== 'live') return;
    if (e.pointerType === 'touch') {
      const tch = touch.current;
      if (!tch) return;
      const dx = tch.x - e.clientX;
      const dy = tch.y - e.clientY;
      if (Math.hypot(dx, dy) < CLICK_PX && !tch.moved) return;
      send(wheelMessage({ clientX: e.clientX, clientY: e.clientY, deltaX: dx, deltaY: dy, deltaMode: 0 }, rect(), vp.current));
      touch.current = { x: e.clientX, y: e.clientY, moved: true };
      return;
    }
    // One move a frame is all a page can show.
    const pending = moving.current;
    moving.current = pointerMessage('mousemove', e.nativeEvent, rect(), vp.current, 0);
    if (!pending) {
      requestAnimationFrame(() => {
        if (moving.current) send(moving.current);
        moving.current = null;
      });
    }
  };

  // What an input method or a phone's keyboard composed waits in the text
  // field, and goes up whole.
  const flushText = () => {
    const field = sink.current;
    if (!field?.value) return;
    send({ type: 'text', text: field.value });
    field.value = '';
  };

  useEffect(() => {
    const field = sink.current!;
    // A phone's keyboard deletes through beforeinput, with no key to read.
    const deleting = (e: InputEvent) => {
      if (e.inputType === 'deleteContentBackward' && !field.value) {
        e.preventDefault();
        send({ type: 'keydown', key: 'Backspace', code: 'Backspace', keyCode: 8, mods: 0 });
        send({ type: 'keyup', key: 'Backspace', code: 'Backspace', keyCode: 8, mods: 0 });
      }
    };
    field.addEventListener('beforeinput', deleting);
    return () => field.removeEventListener('beforeinput', deleting);
  }, [send]);

  const live = phase === 'live';
  const noticeText =
    notice?.kind === 'blocked'
      ? t('signInBrowser.blocked', { site: site.name, host: notice.host })
      : notice?.kind === 'signedIn'
        ? notice.account
          ? t('siteLogins.signedInAs', { account: notice.account })
          : t('siteLogins.signedIn')
        : notice?.kind === 'elsewhere'
          ? t('siteLogins.signInHere', { site: site.name })
          : notice?.kind === 'refused'
            ? t('signInBrowser.refused')
            : notice?.kind === 'closed'
              ? t(CLOSED[notice.reason] ?? 'signInBrowser.closedGone')
              : '';
  const noticeTone =
    notice?.kind === 'signedIn'
      ? 'bg-statusOkBg text-statusOk'
      : notice?.kind === 'elsewhere'
        ? 'bg-statusNeutralBg text-statusNeutral'
        : notice
          ? 'bg-statusWarnBg text-statusWarn'
          : '';

  return (
    <Modal
      title={t('siteLogins.dialogTitle', { site: site.name })}
      hint={t('signInBrowser.hint', { site: site.name })}
      hue={hue}
      widest
      height="screen"
      onClose={() => void finish()}
      footer={
        <>
          <Button
            kind="secondary"
            icon={<IconBack className="rtl:-scale-x-100" />}
            title={t('signInBrowser.back')}
            disabled={!live || !canGoBack}
            onClick={() => send({ type: 'back' })}
          />
          <Button
            kind="secondary"
            icon={<IconRetry />}
            title={t('signInBrowser.reload')}
            disabled={!live}
            onClick={() => send({ type: 'reload' })}
          />
          <span
            className="inline-flex min-w-0 items-center gap-1.5 text-meta text-carbon-textSub"
            title={host || undefined}
            dir="ltr"
          >
            <IconBrowser width={14} height={14} className="shrink-0" />
            <span className="truncate">{host || site.host}</span>
          </span>
          <span className="flex-1" />
          {ownBrowser && (
            <Button kind="secondary" icon={<IconExternalLink />} data-new="open-in-browser" disabled={busy} onClick={openElsewhere}>
              {t('signInBrowser.openInBrowser')}
            </Button>
          )}
          {(phase === 'closed' || phase === 'failed') && (
            <Button kind="secondary" icon={<IconRetry />} onClick={() => setAttempt((n) => n + 1)} disabled={busy}>
              {t('signInBrowser.openAgain')}
            </Button>
          )}
          <Button data-signin-done icon={<IconConfirm />} onClick={() => void finish()} disabled={busy}>
            {t('signInBrowser.done')}
          </Button>
        </>
      }
    >
      <div className="relative flex min-h-0 flex-1 flex-col">
        {/* Over the page rather than above it, so a notice coming and going
            does not resize the page under the pointer. */}
        {/* The window's own surface under the tint, since the page beneath
            can be any colour. */}
        {(noticeText || error) && (
          <div className="pointer-events-none absolute inset-x-3 top-3 z-10 rounded-[var(--radius-control)] bg-carbon-surface shadow-[var(--elevation)]">
            <p
              role="status"
              dir="auto"
              className={`rounded-[var(--radius-control)] px-3 py-2 text-sm ${error ? 'bg-statusWarnBg text-statusWarn' : noticeTone}`}
            >
              {error ? `${t('signInBrowser.failed')} ${error}` : noticeText}
            </p>
          </div>
        )}
        <div
          ref={panel}
          className="glim-well relative grid min-h-0 flex-1 place-items-center overflow-hidden focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-[var(--focus-ring)]"
          // A window takes the width its content asks for, and a frame of a
          // fixed size asks for little, so the panel claims the room the
          // window could have: the screen less the margins, up to the widest
          // window.
          style={{ touchAction: 'none', minWidth: 'min(calc(100vw - 88px), 984px)' }}
          onPointerDown={onPointerDown}
          onPointerUp={onPointerUp}
          onPointerMove={onPointerMove}
          onPointerEnter={(e) => {
            if (live && e.pointerType === 'mouse') focusPage();
          }}
          onPointerLeave={(e) => {
            if (e.pointerType === 'mouse' && document.activeElement === sink.current) sink.current?.blur();
          }}
          onWheel={(e) => {
            if (live) send(wheelMessage(e.nativeEvent, rect(), vp.current));
          }}
          onContextMenu={(e) => e.preventDefault()}
        >
          <canvas
            ref={canvas}
            aria-hidden
            className={`block bg-white transition-opacity ${live ? 'opacity-100' : 'opacity-0'}`}
            style={{ width: shown.width, height: shown.height }}
          />
          {phase === 'starting' && (
            <p className="absolute inset-x-6 text-center text-sm text-carbon-textSub" dir="auto">
              {t('signInBrowser.starting')}
            </p>
          )}
          {/* The keys land in this field: a phone shows its keyboard only for
              one, and an input method composes into it. Nothing stays in it. */}
          <textarea
            ref={sink}
            aria-label={t('signInBrowser.page', { site: site.name })}
            autoCapitalize="off"
            autoComplete="off"
            autoCorrect="off"
            spellCheck={false}
            className="absolute inset-0 resize-none opacity-0"
            style={{ background: 'transparent', caretColor: 'transparent', cursor: 'default' }}
            disabled={!live}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                // Out of the page and onto Done, where a second Escape closes.
                e.preventDefault();
                e.stopPropagation();
                panel.current?.closest('[role="dialog"]')?.querySelector<HTMLElement>('[data-signin-done]')?.focus();
                return;
              }
              const input = keyMessage(e.nativeEvent, mac);
              if (!input) return;
              // Tab and Enter belong to the page, not to the window around it.
              e.preventDefault();
              e.stopPropagation();
              send(input);
            }}
            onKeyUp={(e) => {
              const input = keyMessage(e.nativeEvent, mac);
              if (input) send(input);
            }}
            onInput={(e) => {
              if (!(e.nativeEvent as InputEvent).isComposing) flushText();
            }}
            onCompositionEnd={flushText}
            onPaste={(e) => {
              e.preventDefault();
              const text = e.clipboardData.getData('text/plain');
              if (text) send({ type: 'text', text });
            }}
          />
        </div>
      </div>
    </Modal>
  );
}

// Whether this server has a sign-in browser does not change while the page is
// open, so it is asked once.
let availability: Promise<boolean> | null = null;

/** useSignInBrowserAvailable is whether a browser is there to sign in with, null until the server says. */
export function useSignInBrowserAvailable(): boolean | null {
  const [available, setAvailable] = useState<boolean | null>(null);
  useEffect(() => {
    availability ??= fetchSignInBrowser().then(
      (s) => s.available,
      () => false,
    );
    let live = true;
    void availability.then((a) => live && setAvailable(a));
    return () => {
      live = false;
    };
  }, []);
  return available;
}

/**
 * SignInHereButton opens the sign-in browser for a site. Where no browser is
 * there to sign in with, it refuses and says what is missing.
 */
export function SignInHereButton({
  site,
  hue,
  disabled,
  onChanged,
}: {
  site: SiteLogin;
  hue?: number;
  disabled?: boolean;
  onChanged?: (s: SiteLogin) => void;
}) {
  const { t } = useT();
  const available = useSignInBrowserAvailable();
  const [open, setOpen] = useState(false);
  if (available === null) return null;
  return (
    <>
      <Button
        kind="secondary"
        icon={<IconSignIn />}
        data-new="sign-in-here"
        disabled={disabled || !available}
        hint={available ? undefined : t('signInBrowser.none')}
        onClick={() => setOpen(true)}
      >
        {t('signInBrowser.signInHere')}
      </Button>
      {/* Out to <body>, as the folder chooser goes: a window drawn inside
          the window it was opened from would be held to that one's box. */}
      {open &&
        createPortal(
          <SignInBrowserWindow site={site} hue={hue} onChanged={onChanged} onClose={() => setOpen(false)} />,
          document.body,
        )}
    </>
  );
}
