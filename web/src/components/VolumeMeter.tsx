import { useEffect, useState } from 'react';
import { connectWS, fetchVolumeUsage, type VolumeUsage } from '../lib/api';
import { fmtDate, fmtGB } from '../lib/format';
import { useT } from '../lib/i18n';
import { InfoBubble } from './ui';

/**
 * useVolumeUsage reads the volume counter once and then follows the hub. It
 * polls nothing, since the figure only moves when a download finishes and the
 * server broadcasts it.
 */
export function useVolumeUsage(): VolumeUsage | null {
  const [usage, setUsage] = useState<VolumeUsage | null>(null);

  useEffect(() => {
    let live = true;
    void fetchVolumeUsage().then(
      (u) => {
        if (live) setUsage(u);
      },
      () => {
        // No figure until the server answers.
      },
    );
    return () => {
      live = false;
    };
  }, []);

  useEffect(
    () =>
      connectWS((type, data) => {
        if (type === 'volume') setUsage(data as VolumeUsage);
      }, ['volume']),
    [],
  );

  return usage;
}

/**
 * VolumeUsageRow shows the counter at card width: used volume, the cap and the
 * reset date. The shell bar's copy shows only on the Downloads page, so the
 * settings card and the Overview carry this one.
 */
export function VolumeUsageRow() {
  const { t } = useT();
  const usage = useVolumeUsage();
  if (!usage) return null;

  const capped = usage.cap > 0;
  const filled = capped ? Math.min(100, Math.round((usage.used / usage.cap) * 100)) : 0;

  return (
    <div className="glim-well flex flex-col gap-2 px-4 py-3">
      <div className="flex items-center gap-2">
        <span className="text-[11px] text-carbon-textMuted">{t('settings.volume.used')}</span>
        <InfoBubble tip={t('volume.meterHint')} />
        <span className="flex-1" />
        <span
          className={`glim-num text-[12px] font-semibold leading-none ${
            usage.reached ? 'text-statusFail' : 'text-carbon-text'
          }`}
        >
          {capped
            ? t('settings.volume.usedOf', { used: fmtGB(usage.used), cap: fmtGB(usage.cap) })
            : fmtGB(usage.used)}
        </span>
      </div>

      {/* Without a cap a track could never fill. */}
      {capped && (
        <div
          className="h-1.5 w-full overflow-hidden rounded-[var(--radius-control)] bg-carbon-surface3/70"
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={filled}
          aria-label={t('volume.meter')}
        >
          <div
            className="h-full rounded-[var(--radius-control)] transition-[width] duration-500 ease-out"
            style={{
              width: `${filled}%`,
              background: usage.reached ? 'var(--status-fail-solid)' : 'var(--accent)',
            }}
          />
        </div>
      )}

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px]">
        {/* periodEnd is an instant, not a calendar bucket, so fmtDate fits. */}
        <span className="text-carbon-textMuted">
          {t('settings.volume.resetsOn', { date: fmtDate(usage.periodEnd) })}
        </span>
        {usage.reached && <span className="text-statusFail">{t('settings.volume.reached')}</span>}
        {!capped && <span className="text-carbon-textMuted">{t('settings.volume.noCap')}</span>}
      </div>
    </div>
  );
}

/**
 * VolumeMeter is the same reading at shell-bar size, shown only once a cap is
 * set. The shell bar mounts it under its buttons for the local instance only,
 * because the federation proxy does not forward /api/stats.
 */
export function VolumeMeter() {
  const { t } = useT();
  const usage = useVolumeUsage();
  if (!usage || usage.cap <= 0) return null;

  const filled = Math.min(100, Math.round((usage.used / usage.cap) * 100));

  return (
    // w-full to share both edges with the row of buttons above it.
    <span className="flex w-full flex-col gap-1">
      <span className="flex items-center gap-1.5 text-[11px] text-carbon-textMuted">
        <span
          className={`glim-num min-w-0 flex-1 truncate ${usage.reached ? 'text-statusFail' : 'text-carbon-text'}`}
        >
          {t('settings.volume.usedOf', { used: fmtGB(usage.used), cap: fmtGB(usage.cap) })}
        </span>
        <InfoBubble tip={t('volume.meterHint')} />
      </span>
      {/* A thin track of its own; ProgressBar's h-5 would outweigh the controls above. */}
      <span
        className="block h-1 w-full overflow-hidden rounded-[var(--radius-control)] bg-carbon-surface3/70"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={filled}
        aria-label={t('volume.meter')}
      >
        <span
          className="block h-full rounded-[var(--radius-control)] transition-[width] duration-500 ease-out"
          style={{
            width: `${filled}%`,
            background: usage.reached ? 'var(--status-fail-solid)' : 'var(--accent)',
          }}
        />
      </span>
    </span>
  );
}
