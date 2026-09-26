// The notification centre: typed events behind the corner bubbles, and one
// quiet-mode switch.
//
// toast() is the one funnel every notification passes through, so it also
// feeds the session event log (lib/eventLog.ts) and routes a kind to the
// operating system or to silence per the matrix in lib/notify.ts.
//
// A call that passes only (message, tone) still gets a real kind through
// KIND_BY_TONE, so quiet mode has something to filter on everywhere. Quiet
// mode keeps what somebody would lose by never seeing it: an unanswered
// captcha blocks its download, a benched account stops being used, and a
// failure is never expected. Completions and plain saves are already visible
// where they happened.
import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { Button } from '../components/ui';
import { recordEvent, type EventSubject } from './eventLog';
import { IconClose, IconShield } from './icons';
import { useT } from './i18n';
import { NOTIFY_EVENTS, channelFor, showSystem } from './notify';
import { useUIState } from './uistate';

export type ToastTone = 'ok' | 'fail' | 'info';

// captcha-needs-answer and account-benched have no toast() call yet, since
// CaptchaModal and the account strip report them on their own surfaces.
export type NotificationKind =
  | 'download-done'
  | 'download-failed'
  | 'extraction-done'
  | 'extraction-failed'
  | 'captcha-needs-answer'
  | 'captcha-resolved'
  | 'captcha-failed'
  | 'account-benched'
  | 'account-restored'
  | 'action-done'
  | 'action-failed'
  | 'info';

/**
 * A button a bubble can offer, typically to undo something destructive that
 * has already happened. `holdMs` keeps the bubble up as long as the offer is
 * valid (such as the server's undo window); otherwise it lives DURATION_MS.
 */
export interface ToastAction {
  label: string;
  run: () => void | Promise<void>;
  holdMs?: number;
}

interface ToastMessage {
  id: number;
  message: string;
  tone: ToastTone;
  kind: NotificationKind;
  action?: ToastAction;
  /** The parade (docs/easter-eggs.md). See PARADE_AT below. */
  parade?: boolean;
}

// The parade (docs/easter-eggs.md) fires when this many files finish within the
// window. Completions are counted per link, and a multi-volume set is usually
// three to six parts, so eight keeps the egg a surprise. The window makes it
// "in one go". The count lives in a ref, so nothing is stored.
const PARADE_AT = 8;
const PARADE_WINDOW_MS = 12_000;

/** How many shields ride in the row; five still read as a procession at this size. */
const PARADE_SHIELDS = 5;

// Parade draws the row of shields and then the checkmark, whose delay is the
// length of the sweep. pathLength="1" lets .glim-check-draw draw the whole glyph.
function Parade() {
  return (
    <>
      <span className="kl-parade-row" aria-hidden>
        {Array.from({ length: PARADE_SHIELDS }, (_, i) => (
          <span
            key={i}
            className="kl-parade"
            style={{ ['--stagger-index' as string]: i }}
          >
            <IconShield width={11} height={11} />
          </span>
        ))}
      </span>
      <svg width={14} height={14} viewBox="0 0 20 20" className="shrink-0" aria-hidden focusable="false">
        <path
          className="glim-check-draw kl-parade-check"
          pathLength="1"
          d="M4.5 10.5 8.5 14.5 15.5 6"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </>
  );
}

interface ToastAPI {
  /**
   * `target` names the row the event is about, so the event log can jump to
   * it. It is positional because only two of the many call sites have one.
   */
  toast: (
    message: string,
    tone?: ToastTone,
    kind?: NotificationKind,
    action?: ToastAction,
    target?: EventSubject,
  ) => void;
}

const Ctx = createContext<ToastAPI>({ toast: () => {} });

export const useToast = () => useContext(Ctx);

const toneClass: Record<ToastTone, string> = {
  ok: 'text-statusOk',
  fail: 'text-statusFail',
  info: 'text-statusInfo',
};
// Exported so the event panel's rows carry the same dots as the bubbles.
export const TONE_DOT: Record<ToastTone, string> = {
  ok: 'bg-statusOkSolid',
  fail: 'bg-statusFailSolid',
  info: 'bg-statusInfoSolid',
};

// What an untyped call becomes. fail always lands on a critical kind and
// ok and info never do.
const KIND_BY_TONE: Record<ToastTone, NotificationKind> = {
  ok: 'action-done',
  fail: 'action-failed',
  info: 'info',
};

// Quiet mode in one table: true survives it, false is swallowed.
const CRITICAL: Record<NotificationKind, boolean> = {
  'download-done': false,
  'download-failed': true,
  'extraction-done': false,
  'extraction-failed': true,
  'captcha-needs-answer': true,
  'captcha-resolved': false,
  'captcha-failed': true,
  'account-benched': true,
  'account-restored': false,
  'action-done': false,
  'action-failed': true,
  info: false,
};

// Long enough to read a short line, short enough not to pile up.
const DURATION_MS = 4000;

const QUIET_KEY = 'notifications.quiet';

/**
 * One bubble, with its own auto-dismiss timer. Hover and focus are tracked
 * separately and the timer resumes only when both let go. A pause keeps the
 * remaining time rather than restarting it.
 */
function ToastBubble({ item, onDismiss }: { item: ToastMessage; onDismiss: (id: number) => void }) {
  const { t } = useT();
  const remaining = useRef(item.action?.holdMs ?? DURATION_MS);
  const armedAt = useRef(0);
  // React 19's useRef requires an initial value.
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const held = useRef({ hover: false, focus: false });

  const clear = useCallback(() => {
    if (timer.current === undefined) return;
    clearTimeout(timer.current);
    timer.current = undefined;
    remaining.current = Math.max(0, remaining.current - (Date.now() - armedAt.current));
  }, []);

  const arm = useCallback(() => {
    if (timer.current !== undefined || held.current.hover || held.current.focus) return;
    armedAt.current = Date.now();
    timer.current = setTimeout(() => onDismiss(item.id), remaining.current);
  }, [item.id, onDismiss]);

  const hold = useCallback(
    (key: 'hover' | 'focus', value: boolean) => {
      held.current[key] = value;
      if (value) clear();
      else arm();
    },
    [arm, clear],
  );

  useEffect(() => {
    arm();
    return clear;
  }, [arm, clear]);

  return (
    <div
      role="status"
      data-kind={item.kind}
      onMouseEnter={() => hold('hover', true)}
      onMouseLeave={() => hold('hover', false)}
      onFocus={() => hold('focus', true)}
      onBlur={() => hold('focus', false)}
      // pointer-events-auto undoes the stack's pointer-events-none, or the
      // buttons could not be clicked. surface2 and a ring set the bubble apart
      // from the card behind it. relative contains the parade's row.
      className="glim-toast pointer-events-auto relative flex items-center gap-2.5 rounded-[var(--radius-control)]
        bg-carbon-surface2 px-4 py-2.5 text-sm text-carbon-text shadow-[var(--elevation)] ring-1 ring-carbon-border"
    >
      {/* The parade replaces the tone dot; both would say the same thing. */}
      {item.parade ? (
        <span className={`flex shrink-0 items-center ${toneClass[item.tone]}`}>
          <Parade />
        </span>
      ) : (
        <span className={`h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${TONE_DOT[item.tone]}`} />
      )}
      <span className={toneClass[item.tone]}>{item.message}</span>
      <span className="flex-1" />
      {item.action && (
        // Dismissed before the work starts, so it cannot be pressed twice. The
        // action reports its own result in a new bubble.
        <Button
          kind="primary"
          className="px-2.5 text-xs"
          onClick={() => {
            const run = item.action?.run;
            onDismiss(item.id);
            void run?.();
          }}
        >
          {item.action.label}
        </Button>
      )}
      {/* secondary, not ghost: on a filled bubble a fill-less button reads as
          decoration. */}
      <Button
        kind="secondary"
        icon={<IconClose width={14} height={14} />}
        aria-label={t('common.dismiss')}
        onClick={() => onDismiss(item.id)}
      />
    </div>
  );
}

/**
 * useQuietMode is the quiet-mode switch's state, in the same uistate bucket
 * ToastProvider reads.
 */
export function useQuietMode(): [boolean, (next: boolean) => void] {
  return useUIState(QUIET_KEY, false);
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastMessage[]>([]);
  const seq = useRef(0);

  // Read through a ref so toast() keeps a stable identity; long-lived effects
  // such as useCompletionToasts's websocket listener hold on to it.
  const [quiet] = useUIState(QUIET_KEY, false);
  const quietRef = useRef(quiet);
  useEffect(() => {
    quietRef.current = quiet;
  }, [quiet]);

  // The same for t, which a system notification needs for its title and which
  // changes identity when a catalogue finishes loading.
  const { t } = useT();
  const tRef = useRef(t);
  useEffect(() => {
    tRef.current = t;
  }, [t]);

  const dismiss = useCallback((id: number) => setItems((s) => s.filter((m) => m.id !== id)), []);

  // The parade's memory: files finished in this burst, and when the last landed.
  const burst = useRef({ count: 0, at: 0 });

  const toast = useCallback(
    (
      message: string,
      tone: ToastTone = 'info',
      kind?: NotificationKind,
      action?: ToastAction,
      target?: EventSubject,
    ) => {
      const k = kind ?? KIND_BY_TONE[tone];

      // Recorded before every return below, so events swallowed by quiet mode
      // or sent to the operating system still reach the event log. Quiet mode
      // means "do not interrupt me", not "I never find out".
      recordEvent({ message, tone, kind: k, target });

      // A bubble with an action survives quiet mode, or the undo it offers
      // would be lost.
      if (quietRef.current && !CRITICAL[k] && !action) return;

      // After the quiet-mode check, so muting also mutes system notifications.
      // A bubble with an action always stays in the app: a plain Notification
      // cannot carry a button. channelFor reads the matrix at call time, which
      // keeps this callback stable.
      const channel = action ? 'app' : channelFor(k);
      if (channel === 'silent') return;
      if (channel === 'system') {
        // The matrix row's label is the title and the message the body. The
        // tag is the kind, which collapses a burst into one notification.
        const ev = NOTIFY_EVENTS.find((e) => e.kind === k);
        showSystem(ev ? tRef.current(ev.label) : message, message, k);
        return;
      }

      // Counted here, after the returns above, so only bubbles actually shown
      // make up a parade, and only finished files count.
      let parade = false;
      if (k === 'download-done' || k === 'extraction-done') {
        const now = Date.now();
        burst.current.count = now - burst.current.at > PARADE_WINDOW_MS ? 1 : burst.current.count + 1;
        burst.current.at = now;
        // Exactly at the threshold, so the row sweeps once per burst.
        parade = burst.current.count === PARADE_AT;
      }

      const id = ++seq.current;
      setItems((s) => [...s, { id, message, tone, kind: k, action, parade }]);
    },
    [],
  );

  return (
    <Ctx.Provider value={{ toast }}>
      {children}
      <div className="fixed bottom-[calc(1.25rem+var(--phone-bar-space))] end-5 z-50 flex flex-col gap-2 pointer-events-none">
        {items.map((m) => (
          <ToastBubble key={m.id} item={m} onDismiss={dismiss} />
        ))}
      </div>
    </Ctx.Provider>
  );
}
