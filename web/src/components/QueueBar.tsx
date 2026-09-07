import { useCallback, useEffect, useState } from 'react';
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
 * halts the queue its own way. QueueBar below is this hook plus the speed
 * limit, which stays local: the limit reads and writes /api/settings, never
 * forwarded to a peer, and a command has no business touching it.
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
 * QueueBar is the master switch plus the speed limit, sitting where the work is
 * rather than three clicks away in Settings. It rides in the shell bar
 * (app/Layout.tsx), so it is on every page and outlives navigation.
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
 * TRANSPORT_SIZE is the one deliberate exception to the app's button size
 * (jdp, 2026-09-07: "der play, pause und stopp button in der Kopfleiste sollen
 * viel größer sein. diese drei buttons sind größentechnisch eine ausnahme").
 *
 * Every other control here is 32px square because a page of same-sized things
 * is a page you can scan. These three are not part of that page: they are the
 * one control that starts and stops everything the app does, they sit alone in
 * the head card, and they are what somebody reaches for without looking. A
 * transport control is the one place where "bigger than its neighbours" is the
 * information.
 */
const TRANSPORT_SIZE = 'h-12 w-12 p-0';

export function QueueBar() {
  const { t } = useT();
  const { instance, base } = useInstanceScope();
  const { queue, setHalted, stop } = useQueueControl(base, instance);
  const [cfg, setCfg] = useState<Settings | null>(null);
  // The hard-stop confirm step: null until the button is pressed, then the
  // cost this exact moment would pay (internal/app/app_queue.go's StopCost -
  // "the warning is half the feature", per its own doc comment). Fetched
  // fresh on each press rather than kept live, because it is only ever read
  // once, right before the confirm dialog opens.
  const [stopCost, setStopCost] = useState<StopCost | null>(null);
  const [stopping, setStopping] = useState(false);
  const dialogs = useDialogMute();
  // Held separately from cfg so typing a limit does not fight the field, and
  // held as text so a half-typed "1." survives the keystroke that follows it.
  const [limit, setLimit] = useState('');
  const [unit, setUnit] = useState<RateUnit>('KiB/s');

  // Loaded once, and deliberately not per scope change: the speed limit is a
  // setting of THIS instance whatever the page is showing, because /api/settings
  // is not forwarded to a peer either.
  useEffect(() => {
    fetchSettings()
      .then((s) => {
        setCfg(s);
        // The unit follows the stored value rather than being remembered
        // separately: somebody who set 5 MiB/s should not come back to
        // "5120" in a KiB field and wonder whether it took.
        const { value, unit } = splitRate(s.speedLimit);
        setLimit(fmtRateValue(value));
        setUnit(unit);
      })
      .catch(() => setCfg(null));
  }, []);

  // The limit is saved when the field is left or Enter is pressed, not on every
  // keystroke: saving per character would send a request for "5", "51", "512".
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
    // PATCH, not the whole document: this bar only ever knows about
    // speedLimit, and cfg is a snapshot that can already be behind whatever
    // the Settings page (or another tab) saved since it was fetched - a PUT
    // built from `{...cfg, speedLimit: bytes}` would silently put every one
    // of those other fields back to what this bar last saw. See
    // patchSettings' own doc comment (lib/api.ts).
    setCfg(await patchSettings({ speedLimit: bytes }));
  }

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

  // A peer is in view, and the two controls do NOT land in the same place. The
  // switch follows the scope, because /api/queue is forwarded to a peer; the
  // speed limit cannot, because it lives in /api/settings and settings stay on
  // the machine that configures them.
  //
  // Both used to be withheld, with one sentence explaining why. That was the
  // safe reading of a real trap - a button obeying the peer beside a field
  // obeying this box, with nothing on screen saying so - but it withheld a
  // control the server was perfectly willing to forward, so stopping a peer's
  // queue was impossible from the very bar built to make stopping possible
  // anywhere. The switch stays and the limit goes, with the limit's absence
  // explained rather than silent. The shell's scope tag has already named the
  // peer, so neither line repeats it.
  const peer = Boolean(instance);

  return (
    <div className="flex flex-wrap items-center gap-2">
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
        icon={<IconPlay width={22} height={22} />}
        className={TRANSPORT_SIZE}
        onClick={() => void setHalted(false)}
        disabled={!queue.halted}
        title={t('queue.play')}
        aria-label={t('queue.play')}
      />
      <Button
        kind={!queue.halted ? 'primary' : 'secondary'}
        icon={<IconPause width={22} height={22} />}
        className={TRANSPORT_SIZE}
        onClick={() => void setHalted(true)}
        disabled={queue.halted}
        title={t('queue.pause')}
        aria-label={t('queue.pause')}
      />
      <Button
        kind="secondary"
        icon={<IconStop width={22} height={22} />}
        className={TRANSPORT_SIZE}
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
          field is simply absent while a peer is in view. */}

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
            <>
              <span className="flex-1" />
              <Button kind="ghost" onClick={() => setStopCost(null)} disabled={stopping}>
                {t('queue.hardStopConfirmCancel')}
              </Button>
              <Button kind="danger" disabled={stopping} onClick={() => void confirmStop()}>
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
 * SpeedLimitField is the global download limit, as its own control.
 *
 * It used to sit at the right-hand end of the transport row, pushed there by
 * an ml-auto. Since 2026-09-07 the speed curve owns that space instead and
 * this sits past it, together with the settings hamburger, so the head card
 * reads left to right: what the queue is doing, what it is doing it at, and
 * the two things that change that.
 *
 * It owns its own settings state rather than taking it as a prop. That is not
 * duplication of QueueBar's own fetch: the limit is a setting of THIS instance
 * whatever page is showing (there is no /api/settings on a peer), while
 * QueueBar's queue state is scoped, so the two genuinely answer different
 * questions and were only ever in one component because they were next to each
 * other on screen.
 */
export function SpeedLimitField() {
  const { t } = useT();
  const [cfg, setCfg] = useState<Settings | null>(null);
  // Held as text so a half-typed "1." survives the keystroke that follows it.
  const [limit, setLimit] = useState('');
  const [unit, setUnit] = useState<RateUnit>('KiB/s');

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
    <label className="flex shrink-0 items-center gap-2 self-center text-[11px] text-carbon-textMuted">
      {t('queue.limit')}
      <span className="flex items-center gap-1">
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
          className="glim-num w-16 rounded-[var(--radius-control)] bg-carbon-surface2 px-2 py-1 text-right text-xs
            text-carbon-text outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
        />
        {/* The number is read in whichever unit is picked - type 5, choose
            MiB/s, get 5 MiB/s. Converting instead would make that impossible,
            because switching the unit would rewrite the number just typed. */}
        <select
          value={unit}
          aria-label={t('queue.limitUnit')}
          onChange={(e) => {
            const u = e.target.value as RateUnit;
            setUnit(u);
            void commit({ unit: u });
          }}
          className="rounded-[var(--radius-control)] bg-carbon-surface2 px-1.5 py-1 text-xs text-carbon-text
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
