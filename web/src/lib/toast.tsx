// The notification centre: a typed event core under the same corner-bubble UI
// this app has always had, plus one global quiet-mode switch (build-plan.md
// section 8's Wave 9 note on 9B).
//
// The per-event grid this comment used to defer now exists, in lib/notify.ts,
// and it lands INSIDE toast() below rather than beside it: this function is the
// one place every notification in the app passes through, so routing a kind to
// the operating system instead of to a bubble is a branch here and not a second
// dispatcher somewhere else. Nothing about the vocabulary changed for it - the
// kinds below and KIND_BY_TONE are what the matrix is keyed on.
//
// KIND is what makes an event typed instead of a free-text message. Every
// caller may pass one from the real vocabulary below; the ~50 existing call
// sites across the app that only ever pass (message, tone) still get a real
// kind for free, derived from tone (KIND_BY_TONE) - so quiet mode has
// something true to filter on everywhere today, not only at the few call
// sites anyone bothers to type explicitly.
//
// The session event log (lib/eventLog.ts) hangs off the same fact, one line
// further in: toast() is the only funnel, so the ring behind the sidebar's bell
// is fed from here rather than from a second subscription that could disagree
// with the bubbles. It is recorded ABOVE every early return in toast(), for the
// reason spelled out at that line.
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
import { recordEvent, type EventSubject } from './eventLog';
import { IconClose } from './icons';
import { useT } from './i18n';
import { NOTIFY_EVENTS, channelFor, showSystem } from './notify';
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

/**
 * One thing a bubble can offer besides being read.
 *
 * It exists for the press somebody wants back. An action that only ever
 * destroys - removing rows, clearing a list - is finished before the bubble
 * appears, so the bubble is the only place left where taking it back is one
 * click rather than a re-import; QuickAdd already proves the shape works, it
 * simply had nowhere shared to live.
 *
 * `holdMs` is the caller's, because the caller is the only one who knows how
 * long its offer is good for: the removal's own token expires on the server
 * (app.UndoWindow), and a bubble that goes away before that token does throws
 * away an undo that would have worked. Left out, a bubble lives the ordinary
 * DURATION_MS.
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
}

interface ToastAPI {
  /**
   * `target` is the fifth POSITIONAL parameter rather than the fourth turned
   * into an options bag, and that is a deliberate refusal to be tidy: there are
   * a hundred and three call sites in this app, all but two of which have
   * nothing to point at, and rewriting every one of them to gain a named
   * argument at two would be a diff nobody could review for a change nobody
   * asked for.
   *
   * It says which row the event is ABOUT, so the event log can offer a jump to
   * it. Left out everywhere it would be a guess: a captcha, a benched account
   * and a plain "saved" have no row, and inventing one for them would put a
   * button in the panel that lands nowhere.
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
// Exported so the event panel's rows carry the same three dots the bubbles do.
// A second, near-identical map over there would be the version that drifts the
// first time one of these three tokens is retuned - and the whole promise of
// the log is that it shows the same facts the bubble showed.
export const TONE_DOT: Record<ToastTone, string> = {
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

// The PENDING/useNx fallback table that used to stand here is gone. It existed
// because notifications.quiet* had not landed in en.ts yet and t() could still
// come back with nothing; they landed, so the `?? PENDING[key]` branch and the
// cast that only ever served it were both unreachable. Deleted rather than left
// as a pattern, which is how the next person ends up adding a second one beside
// it for the next set of keys.

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
  const remaining = useRef(item.action?.holdMs ?? DURATION_MS);
  const armedAt = useRef(0);
  // The initial value is explicit and the type carries undefined: React 19's
  // useRef no longer has an overload that takes no argument at all.
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
      // pointer-events-auto is load-bearing: the stack around this bubble
      // (ToastProvider's own overlay, below) is pointer-events-none so empty
      // space near it never blocks a click on whatever is behind it, and
      // that property is inherited. Without restoring it here, the dismiss
      // button underneath is unclickable no matter what its own styling
      // says - the second defect this file fixes.
      // surface2 with a ring rather than the flat surface: a bubble the same
      // colour as the card behind it is a bubble somebody misses (jdp,
      // 2026-09-07: "sich besser vom hintergund abheben"). The ring is what
      // carries it on a light theme, where a shadow alone barely reads.
      className="glim-toast pointer-events-auto flex items-center gap-2.5 rounded-[var(--radius-control)]
        bg-carbon-surface2 px-4 py-2.5 text-sm text-carbon-text shadow-[var(--elevation)] ring-1 ring-carbon-border"
    >
      <span className={`h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${TONE_DOT[item.tone]}`} />
      <span className={toneClass[item.tone]}>{item.message}</span>
      <span className="flex-1" />
      {item.action && (
        // Dismissed before the work is started, not after it: the bubble was
        // offering exactly one press, and leaving it up while the request is in
        // flight invites a second one. What the action itself reports - it
        // worked, it was too late - arrives as its own bubble.
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
          decoration rather than as the way out of it. */}
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
 * QuietModeToggle is the one global switch this wave ships (the per-event
 * settings grid the plan also names is deferred - see this file's module
 * doc comment). Self-contained and exported for a settings page to render
 * as an ordinary row: it originally rendered as a small panel pinned to the
 * bottom-right corner of every page, permanently, over whatever content was
 * there - a plain toggle inflated into a floating fixture nobody asked for.
 * Reading QUIET_KEY through the same shared uistate bucket ToastProvider
 * reads keeps the two in sync with no prop wiring between them.
 *
 * Description + switch, no caption of its own (jdp: "Stiller Modus in
 * eigene Card") - now the one control in its own single-purpose card, whose
 * own title already says "Stiller Modus"; repeating that as this row's
 * caption too would be the exact "heading says what the switch beneath it
 * says" duplication the Rainbow master switch above already avoids, and
 * BombVault's own Settings.tsx documents this exact case by name ("that IS
 * the pattern for a single-purpose Card whose TITLE is the decision"). The
 * label survives as the switch's accessible name either way.
 */
export function QuietModeToggle() {
  const { t } = useT();
  const [quiet, setQuiet] = useUIState(QUIET_KEY, false);
  return (
    <div className="flex items-center justify-between gap-4">
      {/* The row's NAME plus a bubble, not the explanation as the label
          (GlimStone 1.4.0: every explanatory text is a bubble). This row used
          to carry two lines of prose where a label belongs - true, read once,
          and costing that vertical space forever. It was normal body text
          rather than a small caption for a good reason at the time (jdp:
          "bitte normal formatieren, wie die Schrift von zb Regenbogen-Modus"),
          and that reason survives: the label IS normal body text now. The
          sentence moved behind the "(i)", where it is available to exactly the
          person who wants it. */}
      <span className="flex items-center gap-1.5 text-sm text-carbon-text">
        {t('notifications.quiet')}
        {/* The bubble's own sentence changed with the event log: quiet mode
            used to mean a swallowed bubble was gone, and it does not any more,
            so the text that describes the switch has to say where the swallowed
            ones went. See lib/eventLog.ts's note on recording ABOVE the
            quiet-mode return, which is what makes that sentence true. */}
        <InfoBubble tip={t('notifications.quietHint')} />
      </span>
      <Toggle hideLabel checked={quiet} onChange={setQuiet} label={t('notifications.quiet')} />
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

  // The same mirror, for the same reason, around t: a system notification needs
  // a TITLE, and the title is the event's own row label out of the matrix. t
  // changes identity whenever the chosen language's catalogue finishes loading,
  // and putting it in toast()'s dependencies would recreate toast() at that
  // moment - resubscribing every long-lived effect holding onto it, exactly the
  // failure the quiet mirror above exists to avoid.
  const { t } = useT();
  const tRef = useRef(t);
  useEffect(() => {
    tRef.current = t;
  }, [t]);

  const dismiss = useCallback((id: number) => setItems((s) => s.filter((m) => m.id !== id)), []);

  const toast = useCallback(
    (
      message: string,
      tone: ToastTone = 'info',
      kind?: NotificationKind,
      action?: ToastAction,
      target?: EventSubject,
    ) => {
      const k = kind ?? KIND_BY_TONE[tone];

      // ABOVE every return below, and the placement is the whole interlock.
      //
      // Three of the branches that follow end this function without a bubble:
      // quiet mode swallowing a non-critical event, the matrix routing a kind
      // to 'silent', and the matrix routing it to the operating system. Record
      // beside setItems at the bottom, in the place it naturally wants to go,
      // and the log holds only what was already on screen - so quiet mode, the
      // one state somebody opens a bell in to find out what they missed, is
      // exactly the state that empties it.
      //
      // Recorded before the channel switch for the same reason: an event the
      // operating system raised is still an event this window knows about, and
      // a notification somebody dismissed on a phone lock screen is precisely
      // the one they come back to the panel to re-read.
      //
      // What this changes about quiet mode: it stops meaning "you never find
      // out" and starts meaning "it does not interrupt you". The switch's own
      // hint (notifications.quietHint) says so.
      recordEvent({ message, tone, kind: k, target });

      // A bubble carrying an action survives quiet mode whatever its kind says,
      // and that follows from the rule CRITICAL already encodes rather than
      // bending it: quiet mode swallows what is "already sitting in the queue,
      // the account strip or the list that caused it", and an offer that expires
      // is sitting nowhere. Swallow the removal bubble and the undo behind it is
      // gone with it - the switch would be quietly turning a reversible action
      // into an irreversible one, which is not what anybody reads "hide success
      // notifications" as.
      if (quietRef.current && !CRITICAL[k] && !action) return;

      // The matrix comes AFTER the quiet-mode return above, and the order is the
      // whole thing: quiet mode has to gate the system channel too. Switching
      // the channel first would let every non-critical event escape the mute
      // somebody just set, through a louder channel than the one they muted.
      //
      // A bubble carrying an action is never routed anywhere else, whatever its
      // row says. A non-persistent Notification cannot carry a button at all
      // (actions exist only through ServiceWorkerRegistration.showNotification,
      // and public/sw.js deliberately handles nothing), so sending one to the
      // operating system would drop the undo offer on the floor while its token
      // is already expiring server-side - the matrix would be quietly turning a
      // reversible action into an irreversible one, which is the same thing the
      // quiet-mode escape one line above exists to prevent.
      //
      // channelFor reads the matrix out of the uistate bucket at call time
      // rather than through a hook, so this callback keeps its empty dependency
      // list and its stable identity. See lib/notify.ts.
      const channel = action ? 'app' : channelFor(k);
      if (channel === 'silent') return;
      if (channel === 'system') {
        // The row's own label is the title and the bubble's text is the body,
        // so a notification reads the same as the bubble it replaced plus the
        // heading a bubble never needed. The tag is the kind, which is what
        // collapses a burst of two hundred finished downloads into one.
        const ev = NOTIFY_EVENTS.find((e) => e.kind === k);
        showSystem(ev ? tRef.current(ev.label) : message, message, k);
        return;
      }

      const id = ++seq.current;
      setItems((s) => [...s, { id, message, tone, kind: k, action }]);
    },
    [],
  );

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
