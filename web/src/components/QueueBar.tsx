import { useEffect, useState } from 'react';
import { type QueueState, type Settings, fetchQueue, fetchSettings, saveSettings, setQueue } from '../lib/api';
import { useT } from '../lib/i18n';
import { Button } from './ui';
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
  // Held separately from cfg so typing a limit does not fight the field.
  const [limit, setLimit] = useState('');

  useEffect(() => {
    fetchQueue(base).then(setQ).catch(() => setQ(null));
    fetchSettings()
      .then((s) => {
        setCfg(s);
        setLimit(s.speedLimit ? String(Math.round(s.speedLimit / 1024)) : '');
      })
      .catch(() => setCfg(null));
  }, [base]);

  async function toggle() {
    if (!queue) return;
    setQ(await setQueue({ halted: !queue.halted }, base));
  }

  // The limit is saved when the field is left or Enter is pressed, not on every
  // keystroke: saving per character would send a request for "5", "51", "512".
  async function commitLimit() {
    if (!cfg) return;
    const kib = Math.max(0, Number(limit) || 0);
    const bytes = kib * 1024;
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
        <input
          type="number"
          min={0}
          step={256}
          dir="ltr"
          value={limit}
          placeholder="∞"
          onChange={(e) => setLimit(e.target.value)}
          onBlur={commitLimit}
          onKeyDown={(e) => {
            if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
          }}
          className="glim-num w-20 rounded-[var(--radius-control)] bg-carbon-surface2 px-2 py-1 text-right text-xs
            text-carbon-text outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
        />
        KiB/s
      </label>
    </div>
  );
}
