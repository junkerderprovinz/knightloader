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

const RETRY_MS = 5000;

/**
 * useQueueControl holds the queue's halted state and its verbs, shared by
 * QueueBar and the downloads commands. /api/queue is forwarded to peers, so
 * `base` may name one.
 */
export function useQueueControl(base: string, instance: string) {
  const [queue, setQ] = useState<QueueState | null>(null);
  // Bumped after a failed load. The shell mounts this once per session, so
  // without a retry a dropped request at boot would hide the controls.
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    let retry = 0;
    fetchQueue(base)
      .then((q) => {
        if (live) setQ(q);
      })
      .catch(() => {
        // The last known state is kept.
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

  // For separate Play and Pause buttons, as JD draws them.
  const setHalted = useCallback(
    async (halted: boolean) => {
      setQ(await setQueue({ halted }, base));
    },
    [base],
  );

  // The hard stop (app.StopAll) interrupts running transfers, unlike halting.
  const stop = useCallback(async () => {
    const r = await stopAll(base);
    setQ(r.queue);
    return r;
  }, [base]);

  return { queue, toggle, setHalted, stop };
}

// The width of the labelled transport buttons, matching the menu button. The
// height comes from `keyControl`; in the glyph-only modes they stay square.
const TRANSPORT_WIDTH = 'w-44';

/**
 * QueueBar holds Play, Pause and the hard Stop in the shell bar. Halting only
 * stops new dispatch and lets running downloads finish. It takes no `base`:
 * the scope comes from lib/instance.tsx, the same value the page reads.
 */
export function QueueBar() {
  const { t } = useT();
  const { instance, base } = useInstanceScope();
  const { queue, setHalted, stop } = useQueueControl(base, instance);
  // Mirrors Button's showText: only 'text' and 'both' draw a caption.
  const labelMode = useNavLabels();
  const transport = labelMode === 'text' || labelMode === 'both' ? TRANSPORT_WIDTH : '';
  // What a hard stop would cost right now (app.StopCost), fetched on each press.
  const [stopCost, setStopCost] = useState<StopCost | null>(null);
  const [stopping, setStopping] = useState(false);
  const dialogs = useDialogMute();

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

  return (
    // A column; the head card grows to fit it.
    <div className="flex w-fit flex-col items-start gap-2">
      {/* `secondary` rather than `ghost` for the inactive buttons, since a
          disabled ghost button has no fill and reads as gone. Stop asks first
          through the cost dialog. */}
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
        // With the dialog muted, the stop happens on the press.
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

      {stopCost && (
        <Modal
          title={t('queue.hardStopConfirmTitle')}
          mute="hardStop"
          onClose={() => (stopping ? undefined : setStopCost(null))}
          footer={
            // Both neutral, so the sentence decides; `secondary` so they stay
            // visible while disabled.
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
 * wheelSteps lets the wheel step a closed <select> one option per notch,
 * clamped at both ends (design rule 14). It is a native listener with
 * `passive: false`, because React registers onWheel passive and preventDefault
 * would not stop the page scrolling. RuleEditor.tsx and SearchField.tsx carry
 * the same listener.
 */
function wheelSteps(el: HTMLSelectElement | null) {
  if (!el) return;
  const onWheel = (e: WheelEvent) => {
    // Only the sign of deltaY counts; trackpads report fractions.
    if (el.disabled || el.options.length < 2 || e.deltaY === 0) return;
    e.preventDefault();
    const next = Math.min(el.options.length - 1, Math.max(0, el.selectedIndex + (e.deltaY > 0 ? 1 : -1)));
    if (next === el.selectedIndex) return;
    el.selectedIndex = next;
    // A real change event, so the element's onChange handles it like a click.
    el.dispatchEvent(new Event('change', { bubbles: true }));
  };
  el.addEventListener('wheel', onWheel, { passive: false });
  return () => el.removeEventListener('wheel', onWheel);
}

/**
 * SpeedLimitField edits the global download limit. It always belongs to this
 * instance, since /api/settings is not forwarded to peers.
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
        // The unit is derived from the stored value, so 5 MiB/s reads as such.
        const split = splitRate(s.speedLimit);
        setLimit(fmtRateValue(split.value));
        setUnit(split.unit);
      })
      .catch(() => setCfg(null));
  }, []);

  // The wheel changes the number only while the field has focus, so scrolling
  // the page past it edits nothing. Attached natively, since React's onWheel is
  // passive and cannot preventDefault.
  useEffect(() => {
    const el = field.current;
    if (!el) return;
    function onWheel(e: WheelEvent) {
      if (document.activeElement !== el) return;
      // A sideways flick is not a step down.
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

  // Saved on blur or Enter, not per keystroke.
  async function commit(next: { value?: string; unit?: RateUnit } = {}) {
    if (!cfg) return;
    const raw = next.value ?? limit;
    const u = next.unit ?? unit;
    const bytes = joinRate(Math.max(0, Number(raw.replace(',', '.')) || 0), u);
    // Renormalised, so 2048 KiB/s settles as 2 MiB/s.
    const settled = splitRate(bytes);
    setLimit(fmtRateValue(settled.value));
    setUnit(settled.unit);
    if (bytes === cfg.speedLimit) return;
    // A PATCH, since a PUT of this snapshot would revert other settings.
    setCfg(await patchSettings({ speedLimit: bytes }));
  }

  return (
    // w-full to share an edge with the menu button above it.
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
        {/* Changing the unit keeps the typed number: 5 then MiB/s is 5 MiB/s.
            Unlike the number field, the picker answers the wheel on hover. */}
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
