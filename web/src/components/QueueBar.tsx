import { useEffect, useState } from 'react';
import { type QueueState, type Settings, fetchQueue, fetchSettings, saveSettings, setQueue } from '../lib/api';
import { useT } from '../lib/i18n';
import { Button } from './ui';
import { RATE_UNITS, type RateUnit, fmtRateValue, joinRate, splitRate } from '../lib/format';
import { IconPause, IconPlay } from '../lib/icons';

/**
 * QueueBar is the master switch plus the speed limit, sitting where the work is
 * rather than three clicks away in Settings.
 *
 * The switch is deliberately not "pause everything": halting stops the
 * scheduler from handing out new work and leaves running downloads to finish,
 * because aborting a transfer mid-file throws away bytes nobody asked to lose.
 * The running count beside it is what makes that legible.
 */
export function QueueBar({ base = '/api' }: { base?: string }) {
  const { t } = useT();
  const [queue, setQ] = useState<QueueState | null>(null);
  const [cfg, setCfg] = useState<Settings | null>(null);
  // Held separately from cfg so typing a limit does not fight the field, and
  // held as text so a half-typed "1." survives the keystroke that follows it.
  const [limit, setLimit] = useState('');
  const [unit, setUnit] = useState<RateUnit>('KiB/s');

  useEffect(() => {
    fetchQueue(base).then(setQ).catch(() => setQ(null));
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
  }, [base]);

  async function toggle() {
    if (!queue) return;
    setQ(await setQueue({ halted: !queue.halted }, base));
  }

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
    setCfg(await saveSettings({ ...cfg, speedLimit: bytes }));
  }

  if (!queue) return null;

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button
        kind={queue.halted ? 'primary' : 'secondary'}
        icon={queue.halted ? <IconPlay width={16} height={16} /> : <IconPause width={16} height={16} />}
        onClick={toggle}
        className="px-2.5 text-xs"
      >
        {queue.halted ? t('queue.start') : t('queue.stop')}
      </Button>

      {queue.halted && (
        <span className="text-statusInfo text-[11px]">
          {t('queue.halted')}
        </span>
      )}

      <span className="flex-1" />

      <label className="flex items-center gap-2 text-[11px] text-carbon-textMuted">
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
            onBlur={() => commit()}
            onKeyDown={(e) => {
              if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
            }}
            className="glim-num w-16 rounded-[var(--radius-control)] bg-carbon-surface2 px-2 py-1 text-right text-xs
              text-carbon-text outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
          />
          {/* The number is read in whichever unit is picked — type 5, choose
              MiB/s, get 5 MiB/s. Converting instead would make that impossible,
              because switching the unit would rewrite the number the user just
              typed. After committing, the field settles into the largest unit
              that keeps the number whole, so 2048 KiB/s comes back as 2 MiB/s. */}
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
    </div>
  );
}
