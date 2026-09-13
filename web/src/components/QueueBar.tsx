import { useCallback, useEffect, useRef, useState } from 'react';
import {
  type QueueState,
  type Settings,
  type StopCost,
  fetchQueue,
  fetchSettings,
  fetchStopCost,
  patchSettings,
  setQueue,
  stopAll,
} from '../lib/api';
import { useDialogMute } from '../lib/dialogmute';
import { useT } from '../lib/i18n';
import { useInstanceScope } from '../lib/instance';
import { useNavLabels } from '../lib/navLabels';
import { Button, Modal } from './ui';
import { fmtBytes, RATE_UNITS, type RateUnit, fmtRateValue, joinRate, splitRate } from '../lib/format';
import { IconPause, IconPlay, IconStop } from '../lib/icons';

// Long enough not to hammer a server that is genuinely down, short enough that
// the controls are back before anyone has decided the app is broken.
const RETRY_MS = 5000;

/**
 * useQueueControl is the master switch's own state and its one verb, lifted
 * out of the bar so the downloads command surface's "stop queue"/"start
 * queue" entries (lib/commands/downloads.ts) call the exact same `toggle`
 * this bar's own button does, instead of a second copy that fetches and
 * halts the queue its own way. QueueBar below is this hook and the three
 * buttons on it, nothing else. The speed limit is SpeedLimitField's own
 * business at the bottom of this file: it reads and writes /api/settings, which
 * is never forwarded to a peer, and a command has no business touching it.
 */
export function useQueueControl(base: string, instance: string) {
  const [queue, setQ] = useState<QueueState | null>(null);
  // Bumped by a failed load to ask again. This used to be mounted once per
  // visit to the download page, so every visit was a fresh attempt; in the
  // shell it mounts once for the session, and without a retry one dropped
  // request at boot would leave the app with no transport control until
  // somebody thought to reload.
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    // Asked for a peer too. This used to skip, because the bar withheld the
    // controls for a peer anyway - and when the switch was given back, the skip
    // was what kept it invisible: `queue` stayed null and the render bailed out
    // above the switch it was meant to draw. Two halves of one decision in two
    // places, and removing only the visible half left a control that existed and
    // never appeared.
    //
    // /api/queue IS forwarded (internal/api/routes_federation.go), so asking is
    // answered. base already carries the scope.
    let live = true;
    let retry = 0;
    fetchQueue(base)
      .then((q) => {
        if (live) setQ(q);
      })
      .catch(() => {
        // The last known state is kept rather than blanked. A bar that
        // disappears on one dropped request is a bar people stop looking for.
        if (live) retry = window.setTimeout(() => setAttempt((n) => n + 1), RETRY_MS);
      });
    return () => {
      live = false;
      clearTimeout(retry);
    };
  }, [base, instance, attempt]);

  const toggle = useCallback(async () => {
    if (!queue) return;
    setQ(await setQueue({ halted: !queue.halted }, base));
  }, [queue, base]);

  // The explicit halves of `toggle`, for the Play/Pause pair (jdp: "ein
  // schöner Play, Pause, Stopp button wie in JD" - JD draws three distinct
  // buttons rather than one that flips, and a Play button that could also
  // BE the pause button depending on state reads as one control doing two
  // jobs instead of two controls each doing one).
  const setHalted = useCallback(
    async (halted: boolean) => {
      setQ(await setQueue({ halted }, base));
    },
    [base],
  );

  // The hard stop (internal/app/app_queue.go's StopAll) - a different verb
  // from the master switch: this one interrupts transfers in flight instead
  // of letting them finish. Exposed here too so QueueBar's Stop button
  // updates the same `queue` state toggle/setHalted already own, rather than
  // going around it and drifting out of sync until the next poll.
  const stop = useCallback(async () => {
    const r = await stopAll(base);
    setQ(r.queue);
    return r;
  }, [base]);

  return { queue, toggle, setHalted, stop };
}

/**
 * QueueBar is the master switch - Play, Pause and the hard Stop - sitting where
 * the work is rather than three clicks away in Settings. It rides in the shell
 * bar (app/Layout.tsx), so it is on every page and outlives navigation. The
 * speed limit used to be part of this component and is SpeedLimitField at the
 * bottom of this file now; nothing here reads /api/settings any more.
 *
 * The switch is deliberately not "pause everything": halting stops the
 * scheduler from handing out new work and leaves running downloads to finish,
 * because aborting a transfer mid-file throws away bytes nobody asked to lose.
 * The running count beside it is what makes that legible.
 *
 * It takes no `base`. A bar the shell can hand an address to is a bar the shell
 * can hand the WRONG address to, and the wrong one here halts a different
 * machine than the list on screen; the scope comes from lib/instance.tsx, which
 * is the same value the page is reading.
 */
/**
 * TRANSPORT_WIDTH is what is LEFT of the transport buttons' own size once the
 * height stopped being this file's to invent.
 *
 * These three were a 48px square, written down here as the app's one sanctioned
 * exception to its button size (jdp, 2026-09-07: "der play, pause und stopp
 * button in der Kopfleiste sollen viel größer sein. diese drei buttons sind
 * größentechnisch eine ausnahme"). GlimStone rule 19 allows exactly two button
 * heights and says there will not be a third, and a height argued for at its own
 * call site - however well argued - is precisely how a house ends up with a
 * ladder somebody has to pick a rung from.
 *
 * The request survives and the exemption does not. These three ARE the key
 * control: the head card exists for them, they are what somebody reaches for
 * without looking, and that is the definition of --btn-h-key (2.5rem). So they
 * take `keyControl` like every other key button in the app, and they are bigger
 * than their neighbours because the ladder's upper rung says so rather than
 * because this one file said so. From outside, a documented exemption and a
 * control that simply ignores the rule look identical, which is the argument
 * 1.13.0 used when it deleted the last approved carve-out in the language
 * instead of moving it somewhere quieter. The glyph follows the box for free:
 * Button sizes its own mark from the height it is on.
 *
 * The WIDTH stays here, because a width is a call site's business. It is the
 * menu button's, which is what jdp asked for when the labels went on ("So breit
 * wie der Menü-Button"), and it only applies while the labelling engine is
 * showing words: a fixed square cannot hold one. Measured on the preview
 * instance, "Wiedergabe" needed 77px inside a square whose overflow is visible,
 * so the word hung out of its own box and across its neighbour - and the words
 * here are German more often than not. In the glyph modes there is no width
 * class at all, because a key control with no caption is a square at its own
 * height and Button already draws it.
 */
const TRANSPORT_WIDTH = 'w-44';

export function QueueBar() {
  const { t } = useT();
  const { instance, base } = useInstanceScope();
  const { queue, setHalted, stop } = useQueueControl(base, instance);
  // Same read the Button makes for itself, made here too because the WIDTH has
  // to change with the label and only the caller owns the className. The
  // condition MIRRORS Button's own `showText` (ui.tsx) exactly - 'glyph' and
  // 'hover' draw no caption, so they keep the square, and any other reading
  // here would widen a button for a word it is not showing.
  const labelMode = useNavLabels();
  const transport = labelMode === 'text' || labelMode === 'both' ? TRANSPORT_WIDTH : '';
  // The hard-stop confirm step: null until the button is pressed, then the
  // cost this exact moment would pay (internal/app/app_queue.go's StopCost -
  // "the warning is half the feature", per its own doc comment). Fetched
  // fresh on each press rather than kept live, because it is only ever read
  // once, right before the confirm dialog opens.
  const [stopCost, setStopCost] = useState<StopCost | null>(null);
  const [stopping, setStopping] = useState(false);
  const dialogs = useDialogMute();

  // No speed-limit state in here any more. This component kept a full second
  // copy of it - the settings fetch, the value, the unit and a commit() - after
  // the limit itself moved out to SpeedLimitField below, so every mount asked
  // /api/settings for a value nothing on screen read. A lever that decides
  // nothing reads as a lever (GlimStone 1.13.0), and this one also cost a
  // request.

  async function confirmStop() {
    setStopping(true);
    try {
      await stop();
      setStopCost(null);
    } finally {
      setStopping(false);
    }
  }

  if (!queue) return null;

  // Nothing here is withheld while a peer is in view. The switch follows the
  // scope, because /api/queue IS forwarded to a peer (see useQueueControl's own
  // note above), and the speed limit is not in this card at all any more - it
  // lives in SpeedLimitField, which says for itself why it stays this machine's.

  return (
    // A COLUMN, not a row (jdp, 2026-09-07: "die drei button untereinander"),
    // and the head card grows to fit it rather than the buttons shrinking to
    // fit the card - his own call when the two pulled against each other ("die
    // karte soll so hoch werden das die drei untereinander platz haben"). The
    // three are key controls now rather than a size of their own; see
    // TRANSPORT_WIDTH for what that changed and why.
    <div className="flex w-fit flex-col items-start gap-2">
      {/* Three distinct transport buttons (jdp: "ein schöner Play, Pause,
          Stopp button wie in JD") rather than one that flips between two
          jobs. Play/Pause are the master switch (SetHalted) - running
          downloads always finish either way, only new dispatch stops. Stop
          is the separate, harder verb (StopAll): it interrupts transfers in
          flight right now, so it always asks first via the cost dialog
          below rather than acting on the first click.

          `secondary`, never `ghost`, for the button that is not the current
          "press this" one: ghost carries no background at all, so a
          disabled ghost button (`disabled:opacity-35` on top of nothing)
          reads as gone rather than as a control that simply isn't the right
          moment for it (jdp: "sollen auch im deaktivierten Zustand als
          Badge erkennbar sein"). Stop stays plain-coloured like its
          siblings too, not tinted fault-red - it is a mode switch here, the
          same weight as Play/Pause, and only the confirm dialog it opens
          carries the real warning. */}
      <Button
        kind={queue.halted ? 'primary' : 'secondary'}
        icon={<IconPlay />}
        labelled
        keyControl
        className={transport}
        onClick={() => void setHalted(false)}
        disabled={!queue.halted}
        title={t('queue.play')}
        aria-label={t('queue.play')}
      />
      <Button
        kind={!queue.halted ? 'primary' : 'secondary'}
        icon={<IconPause />}
        labelled
        keyControl
        className={transport}
        onClick={() => void setHalted(true)}
        disabled={queue.halted}
        title={t('queue.pause')}
        aria-label={t('queue.pause')}
      />
      <Button
        kind="secondary"
        icon={<IconStop />}
        labelled
        keyControl
        className={transport}
        // Silenced, the stop happens on the press. The dialog exists to say
        // what is about to be interrupted, and somebody who ticked "do not
        // show this again" has answered that in advance - see dialogmute.ts.
        onClick={() => {
          if (dialogs.isMuted('hardStop')) {
            void stop();
            return;
          }
          void fetchStopCost(base).then(setStopCost);
        }}
        disabled={queue.running === 0}
        title={t('queue.hardStop')}
        aria-label={t('queue.hardStop')}
      />

      {/* No prose in this card any more (jdp, 2026-09-06: "die infotexte in der
          kopfcard wie zb.: Warteschlange gestoppt. Laufende Downloads werden
          fertig, es startet nichts Neues. sollen weg"). Both sentences that
          used to sit here - the halted note and the peer's "the limit is this
          machine's" note - said in a paragraph what the controls beside them
          already say by their own state: Play lit means halted, and the limit
          is not one of this card's controls at all any more. */}

      {/* The speed limit no longer lives in this row. It moved to the far side
          of the speed curve (jdp, 2026-09-07: "der downloadgraph in der
          kopfzeile soll viel breiter sein. das hamburgermenü und die
          geschwindigkeitsbegrenzung soll rechts davon sein"), so this card now
          reads left to right as: what the queue is doing, what it is doing it
          at, and the two controls that change that. See SpeedLimitField below,
          which ShellStrip renders. */}

      {stopCost && (
        <Modal
          title={t('queue.hardStopConfirmTitle')}
          mute="hardStop"
          onClose={() => (stopping ? undefined : setStopCost(null))}
          footer={
            // Cancel left, proceed right - the one that goes ahead sits at the
            // end of the row, and the pair mirrors with the page under RTL
            // because nothing here reverses or reorders it.
            //
            // Both wear the same neutral kind, and that is the point: neither is
            // recommended, the sentence above them decides. `secondary` rather
            // than `ghost` for the same reason the transport buttons carry it -
            // a ghost button that is also disabled reads as gone, and both of
            // these go disabled while the stop is running.
            <>
              <span className="flex-1" />
              <Button kind="secondary" onClick={() => setStopCost(null)} disabled={stopping}>
                {t('queue.hardStopConfirmCancel')}
              </Button>
              <Button kind="secondary" disabled={stopping} onClick={() => void confirmStop()}>
                {stopping ? t('settings.system.acting') : t('queue.hardStopConfirmProceed')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">
            {t('queue.hardStopConfirmBody', {
              n: stopCost.running,
              detail:
                stopCost.losing.length > 0
                  ? t('queue.hardStopConfirmLoss', { bytes: fmtBytes(stopCost.bytes) })
                  : t('queue.hardStopConfirmSafe'),
            })}
          </p>
          {stopCost.unknown > 0 && (
            <p className="mt-2 text-xs text-carbon-textMuted">{t('queue.hardStopConfirmUnknown', { n: stopCost.unknown })}</p>
          )}
        </Modal>
      )}
    </div>
  );
}

/**
 * wheelSteps is rule 14's wheel clause on a native <select>: a CLOSED select
 * steps one option per notch and fires a real `change`, without the platform's
 * own list opening at all. The platform only wires the wheel up once that list
 * is already open, which costs a click on a value somebody reaches for
 * constantly - and the wheel belongs to the PICKER, not to the element the
 * platform happens to draw, so it has to be here rather than left to the widget.
 *
 * Clamped at both ends instead of wrapping: one notch too many must not land a
 * value from the other end of the list.
 *
 * A ref callback with its own cleanup (React 19) and `{ passive: false }`,
 * never onWheel: React registers onWheel passive at its root, so preventDefault
 * inside such a handler does nothing but log a warning, and the page would
 * scroll away under the pointer while the value changed.
 *
 * The same eight lines sit in components/RuleEditor.tsx and
 * components/SearchField.tsx, the app's two other native selects. GlimStone
 * ships one copy as reference/selectScroll.ts and this app's home for it would
 * be lib/selectScroll.ts, which does not exist yet; three copies of a listener
 * is the honest price of not inventing that file from inside one component.
 */
function wheelSteps(el: HTMLSelectElement | null) {
  if (!el) return;
  const onWheel = (e: WheelEvent) => {
    // A horizontal wheel says nothing about this control, and a trackpad
    // reports fractional deltas - so read the sign of deltaY and nothing else.
    if (el.disabled || el.options.length < 2 || e.deltaY === 0) return;
    // This handler IS the scroll while the pointer sits on the control.
    e.preventDefault();
    const next = Math.min(el.options.length - 1, Math.max(0, el.selectedIndex + (e.deltaY > 0 ? 1 : -1)));
    if (next === el.selectedIndex) return;
    el.selectedIndex = next;
    // A real change event rather than a state write, so the onChange already on
    // the element picks this up exactly as it would a click on an <option>.
    el.dispatchEvent(new Event('change', { bubbles: true }));
  };
  el.addEventListener('wheel', onWheel, { passive: false });
  return () => el.removeEventListener('wheel', onWheel);
}

/**
 * SpeedLimitField is the global download limit, as its own control.
 *
 * It used to sit at the right-hand end of the transport row, pushed there by
 * an ml-auto. Since 2026-09-07 the speed curve owns that space instead and
 * this sits past it, together with the settings hamburger, so the head card
 * reads left to right: what the queue is doing, what it is doing it at, and
 * the two things that change that.
 *
 * It owns its own settings state rather than taking it as a prop, and it is now
 * the ONLY place in this file that reads /api/settings: QueueBar kept a second
 * copy of the same fetch, value, unit and commit for a while after the control
 * moved out here, asking the server on every mount for something nothing on
 * screen read.
 *
 * What the two answer is genuinely different, which is why they are two
 * components: the limit is a setting of THIS instance whatever page is showing
 * (there is no /api/settings on a peer), while the queue state above is scoped
 * and IS forwarded. They were only ever in one component because they were next
 * to each other on screen.
 */
export function SpeedLimitField() {
  const { t } = useT();
  const [cfg, setCfg] = useState<Settings | null>(null);
  // Held as text so a half-typed "1." survives the keystroke that follows it.
  const [limit, setLimit] = useState('');
  const [unit, setUnit] = useState<RateUnit>('KiB/s');
  const field = useRef<HTMLInputElement>(null);

  useEffect(() => {
    fetchSettings()
      .then((s) => {
        setCfg(s);
        // The unit follows the stored value rather than being remembered
        // separately: somebody who set 5 MiB/s should not come back to "5120"
        // in a KiB field and wonder whether it took.
        const split = splitRate(s.speedLimit);
        setLimit(fmtRateValue(split.value));
        setUnit(split.unit);
      })
      .catch(() => setCfg(null));
  }, []);

  /**
   * The wheel changes the number (jdp, 2026-09-07: "man soll im eingabefeld die
   * zahl per scrollen ändern können"), but ONLY while the field has focus.
   *
   * That condition is the whole design. A wheel handler that fires on hover
   * turns a scroll down the page into an edit of whatever the pointer happened
   * to pass over, and the page stops scrolling where the pointer rests. Focus
   * first means the gesture is deliberate: click into the field, then spin.
   *
   * Attached natively rather than through React's onWheel, which is registered
   * passive at the root - preventDefault there is ignored with a console
   * warning, so the page would scroll AND the number would change.
   */
  useEffect(() => {
    const el = field.current;
    if (!el) return;
    function onWheel(e: WheelEvent) {
      if (document.activeElement !== el) return;
      // A horizontal wheel is not an adjustment of this number. Without the
      // test a deltaY of 0 read as "downwards" and quietly stepped the limit
      // DOWN on a sideways flick; a trackpad's fractional deltas are already
      // handled, because only the sign is read.
      if (e.deltaY === 0) return;
      e.preventDefault();
      const step = e.shiftKey ? 10 : 1;
      setLimit((cur) => {
        const n = Number(cur.replace(',', '.')) || 0;
        return fmtRateValue(Math.max(0, n + (e.deltaY < 0 ? step : -step)));
      });
    }
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, []);

  // Saved when the field is left or Enter is pressed, not on every keystroke:
  // saving per character would send a request for "5", "51", "512".
  async function commit(next: { value?: string; unit?: RateUnit } = {}) {
    if (!cfg) return;
    const raw = next.value ?? limit;
    const u = next.unit ?? unit;
    const bytes = joinRate(Math.max(0, Number(raw.replace(',', '.')) || 0), u);
    // Re-derive the unit from what was actually stored, so typing 2048 KiB/s
    // settles as "2 MiB/s" instead of leaving the field in a form the app would
    // never have chosen itself.
    const settled = splitRate(bytes);
    setLimit(fmtRateValue(settled.value));
    setUnit(settled.unit);
    if (bytes === cfg.speedLimit) return;
    // PATCH, not the whole document - see patchSettings' own doc comment: a PUT
    // built from this component's snapshot would put every other setting back
    // to what it last saw.
    setCfg(await patchSettings({ speedLimit: bytes }));
  }

  return (
    // w-full, not shrink-0: this sits in ShellStrip's fixed-width column under
    // the menu button and takes that column's whole width, so the two controls
    // share one edge instead of each hugging its own text.
    <label className="flex w-full items-center gap-1.5 text-[11px] text-carbon-textMuted">
      <span className="shrink-0">{t('queue.limit')}</span>
      <span className="flex min-w-0 flex-1 items-center gap-1">
        <input
          type="text"
          inputMode="decimal"
          dir="ltr"
          value={limit}
          placeholder="∞"
          aria-label={t('queue.limit')}
          onChange={(e) => setLimit(e.target.value)}
          onBlur={() => void commit()}
          onKeyDown={(e) => {
            if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
          }}
          ref={field}
          title={t('queue.limitWheelHint')}
          className="glim-num min-w-0 flex-1 rounded-[var(--radius-control)] bg-carbon-surface2 px-2 py-1 text-right text-xs
            text-carbon-text outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
        />
        {/* The number is read in whichever unit is picked - type 5, choose
            MiB/s, get 5 MiB/s. Converting instead would make that impossible,
            because switching the unit would rewrite the number just typed.

            The wheel steps it, like the number beside it and like every other
            picker in the app (rule 14, see wheelSteps above). The two answer the
            wheel on different terms and that is not an inconsistency: a number
            field needs focus first because a page full of them would otherwise
            edit itself under a scrolling pointer, while a picker with three
            options answers on hover, which is what the rule asks for. */}
        <select
          ref={wheelSteps}
          value={unit}
          aria-label={t('queue.limitUnit')}
          onChange={(e) => {
            const u = e.target.value as RateUnit;
            setUnit(u);
            void commit({ unit: u });
          }}
          className="glim-select shrink-0 appearance-none rounded-[var(--radius-control)] bg-carbon-surface2 ps-1.5 pe-6 py-1 text-xs text-carbon-text
            outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
        >
          {RATE_UNITS.map((u) => (
            <option key={u.label} value={u.label}>
              {u.label}
            </option>
          ))}
        </select>
      </span>
    </label>
  );
}
