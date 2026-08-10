// The notification centre: a typed event core under the same corner-bubble UI
// this app has always had, plus one global quiet-mode switch (build-plan.md
// section 8's Wave 9 note on 9B; the per-event settings grid it also
// describes is deferred to the later sweep named in the "one straight swap"
// note near the plan's "honest total" section - this file is scope-narrowed
// to the typed core and the global toggle only).
//
// KIND is what makes an event typed instead of a free-text message. Every
// caller may pass one from the real vocabulary below; the ~50 existing call
// sites across the app that only ever pass (message, tone) still get a real
// kind for free, derived from tone (KIND_BY_TONE) - so quiet mode has
// something true to filter on everywhere today, not only at the few call
// sites anyone bothers to type explicitly.
//
// Quiet mode does not suppress by tone. CRITICAL draws the line at "does
// somebody lose something by never seeing this bubble" - a captcha nobody
// answers blocks that download forever, a benched account silently stops
// being routed to, and a failure is the one outcome nobody asked for; those
// keep surfacing. A completion, a resolution or a plain "saved" is already
// sitting in the queue, the account strip or the list that caused it, so
// nothing is lost by swallowing the bubble.
import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { Button, InfoBubble, Toggle } from '../components/ui';
import { IconClose } from './icons';
import { useT, type TranslationKey } from './i18n';
import { useUIState } from './uistate';

export type ToastTone = 'ok' | 'fail' | 'info';

// Grounded in grep, not invented: download completion (Layout.tsx's
// useCompletionToasts), extraction (Archives.tsx's unpack/abort), captcha
// (CaptchaModal.tsx's resolve/timeout/network paths, and the challenge
// itself - internal/captcha, Wave 7), account health (BenchedUntil / the
// HealthOK return trip - internal/accounts/health.go, Wave 6) and a generic
// pair for every plain save/remove/load call site that only ever carried a
// tone. captcha-needs-answer and account-benched have no toast() call site
// yet - CaptchaModal and the account strip already say so on their own
// surfaces - but the source event is real, so the kind exists for whichever
// call site reaches for it next rather than that caller inventing its own.
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

interface ToastMessage {
  id: number;
  message: string;
  tone: ToastTone;
  kind: NotificationKind;
}

interface ToastAPI {
  toast: (message: string, tone?: ToastTone, kind?: NotificationKind) => void;
}

const Ctx = createContext<ToastAPI>({ toast: () => {} });

export const useToast = () => useContext(Ctx);

const toneClass: Record<ToastTone, string> = {
  ok: 'text-statusOk',
  fail: 'text-statusFail',
  info: 'text-statusInfo',
};
const dot: Record<ToastTone, string> = {
  ok: 'bg-statusOkSolid',
  fail: 'bg-statusFailSolid',
  info: 'bg-statusInfoSolid',
};

// What an untyped call becomes. fail always lands on a critical kind (see
// CRITICAL) and ok/info never do, which is the same split a free-text call
// site was already expressing through tone alone - typing it just makes
// quiet mode able to act on it.
const KIND_BY_TONE: Record<ToastTone, NotificationKind> = {
  ok: 'action-done',
  fail: 'action-failed',
  info: 'info',
};

// The whole design of quiet mode is this table. true survives it, false is
// swallowed at the moment toast() is called. See the module doc comment for
// the rule it encodes.
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

// Long enough to read a short line, short enough not to pile up when several
// fire at once. Unchanged by this file - the defect fixed here is the total
// absence of a pause, not the number itself.
const DURATION_MS = 4000;

const QUIET_KEY = 'notifications.quiet';

/**
 * The two strings this file needs, keyed by where they are going.
 *
 * Same arrangement as Connections.tsx (Wave 2's 2E) and Captcha.tsx (Wave
 * 7B): the locale files are one writer's lane per wave (9E, phase 3 of this
 * one), and the lookup asks the real catalogue first, so the day these keys
 * land in en.ts this table stops being consulted and can be deleted without
 * touching anything else here.
 */
const PENDING = {
  'notifications.quiet': 'Quiet mode',
  'notifications.quietHint':
    'Hides success and info notifications. A failure, a captcha waiting on you, or a benched account still shows.',
} as const;

type PendingKey = keyof typeof PENDING;

function useNx() {
  const { t } = useT();
  return useCallback(
    (key: PendingKey) => {
      // The cast is the whole point: these keys are not in the union yet. It
      // is narrow - only keys in PENDING can be passed - and it goes with
      // the table.
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      return translated ?? PENDING[key];
    },
    [t],
  );
}

/**
 * One bubble, owning its own auto-dismiss timer so pausing one on hover
 * never touches its neighbours' clocks.
 *
 * The defect this replaces was a single hardcoded setTimeout with nothing
 * watching the pointer: a bubble carrying a control (the dismiss button
 * below, and eventually more) could vanish out from under someone reading
 * it. hold() tracks hover and focus as two independent flags rather than
 * one - tabbing onto the dismiss button while the pointer is still over the
 * bubble must not resume the clock the moment either one alone changes, only
 * once both let go. Pausing keeps the REMAINING time rather than resetting
 * to the full duration, so glancing away and back does not buy an unlimited
 * extension one hover at a time.
 */
function ToastBubble({ item, onDismiss }: { item: ToastMessage; onDismiss: (id: number) => void }) {
  const { t } = useT();
  const remaining = useRef(DURATION_MS);
  const armedAt = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout>>();
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
      // pointer-events-auto is load-bearing: the stack around this bubble
      // (ToastProvider's own overlay, below) is pointer-events-none so empty
      // space near it never blocks a click on whatever is behind it, and
      // that property is inherited. Without restoring it here, the dismiss
      // button underneath is unclickable no matter what its own styling
      // says - the second defect this file fixes.
      className="glim-toast pointer-events-auto flex items-center gap-2.5 rounded-[var(--radius-control)] bg-carbon-surface px-4 py-2.5 text-sm text-carbon-text shadow-[var(--elevation)]"
    >
      <span className={`h-2 w-2 shrink-0 rounded-full ${dot[item.tone]}`} />
      <span className={toneClass[item.tone]}>{item.message}</span>
      <span className="flex-1" />
      <Button
        kind="ghost"
        icon={<IconClose width={14} height={14} />}
        aria-label={t('common.dismiss')}
        onClick={() => onDismiss(item.id)}
      />
    </div>
  );
}

/**
 * QuietModeToggle is the one global switch this wave ships (the per-event
 * settings grid the plan also names is deferred - see this file's module
 * doc comment). Self-contained and exported for a settings page to render
 * as an ordinary row: it originally rendered as a small panel pinned to the
 * bottom-right corner of every page, permanently, over whatever content was
 * there - a plain toggle inflated into a floating fixture nobody asked for.
 * Reading QUIET_KEY through the same shared uistate bucket ToastProvider
 * reads keeps the two in sync with no prop wiring between them.
 */
export function QuietModeToggle() {
  const nx = useNx();
  const [quiet, setQuiet] = useUIState(QUIET_KEY, false);
  return (
    <div className="flex items-center gap-1">
      <Toggle checked={quiet} onChange={setQuiet} label={nx('notifications.quiet')} />
      <InfoBubble tip={nx('notifications.quietHint')} />
    </div>
  );
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastMessage[]>([]);
  const seq = useRef(0);

  // Persisted the way every other UI-only preference is (lib/uistate.ts):
  // local first, written through, never rolled back by a slow server. A ref
  // mirrors it so toast() - called from arbitrary event handlers all over
  // the app, some of them long-lived effects - always reads the current
  // value without itself being recreated every time the switch flips, which
  // would otherwise resubscribe every effect holding onto the toast()
  // identity it returned (useCompletionToasts's WS listener among them).
  // Only the value is needed here - QuietModeToggle owns the setter, from
  // wherever a settings page mounts it, via the same shared bucket.
  const [quiet] = useUIState(QUIET_KEY, false);
  const quietRef = useRef(quiet);
  useEffect(() => {
    quietRef.current = quiet;
  }, [quiet]);

  const dismiss = useCallback((id: number) => setItems((s) => s.filter((m) => m.id !== id)), []);

  const toast = useCallback((message: string, tone: ToastTone = 'info', kind?: NotificationKind) => {
    const k = kind ?? KIND_BY_TONE[tone];
    if (quietRef.current && !CRITICAL[k]) return;
    const id = ++seq.current;
    setItems((s) => [...s, { id, message, tone, kind: k }]);
  }, []);

  return (
    <Ctx.Provider value={{ toast }}>
      {children}
      <div className="fixed bottom-5 right-5 z-50 flex flex-col gap-2 pointer-events-none">
        {items.map((m) => (
          <ToastBubble key={m.id} item={m} onDismiss={dismiss} />
        ))}
      </div>
    </Ctx.Provider>
  );
}
