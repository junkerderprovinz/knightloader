import { useEffect, useState } from 'react';
import { connectWS, fetchVolumeUsage, type VolumeUsage } from '../lib/api';
import { fmtDate, fmtGB } from '../lib/format';
import { useT } from '../lib/i18n';
import { InfoBubble } from './ui';

/**
 * useVolumeUsage is the running counter, and the one rule about it is that it
 * owns no timer.
 *
 * The figure only moves when a download FINISHES - that is the moment the
 * server adds a history row, recomputes the period total and broadcasts it - so
 * there is nothing between two of those moments for an interval to discover. It
 * would also be an interval running for a widget nobody can see: app/Layout.tsx
 * renders the shell bar with `hidden` rather than unmounting it, so on Overview,
 * Settings and the Collector this stays alive with its reading off screen, and
 * a poll there is a request a minute for a number in a closed drawer.
 *
 * So: one snapshot on mount, then whatever the hub pushes. The snapshot is not
 * redundant - the socket only ever says what CHANGED, and on an instance where
 * nothing has finished since the counter last restarted that is nothing at all.
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
        /* No figure rather than a guessed one: the row stays out until the
           server has answered, the same refusal DiskSpace makes about a
           free-space readout it cannot get. */
      },
    );
    return () => {
      live = false;
    };
  }, []);

  // connectWS returns its own closer, which is exactly the cleanup this effect
  // owes. 'volume' is the only kind these readings care about, and subscribing
  // narrowly means the socket does not wake this component for every task frame
  // in a running queue.
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
 * The figure at card width: what has finished since the counter restarted, what
 * it is measured against, and when it starts over.
 *
 * It exists twice on purpose - on the Downloads settings card beside the cap
 * itself, and inside the Overview's volume card - because the shell bar's copy
 * is hidden on every page but Downloads (app/Layout.tsx). A counter you can
 * only see on the one page you were not looking at is a counter nobody finds.
 *
 * Each mount opens its own snapshot and its own socket, which is what every
 * other live component in this app already does (see connectWS' own note: there
 * is no shared multiplexer yet). Two of them are cheap and the alternative is a
 * store hoisted above the router for one number.
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
        {/* The whole explanation, in the one place GlimStone puts one. Nothing
            loose under the figure: what it counts, when it moves and what it
            cannot see are three sentences, and three sentences under a number
            is a paragraph people read once. */}
        <InfoBubble tip={t('volume.meterHint')} />
        <span className="flex-1" />
        {/* dir="ltr": the figure and its unit are one token and must not be
            reordered into "BG 2.000 nov BG 1.234" in an Arabic or Hebrew locale,
            the same reason SpeedMeter pins its own reading. */}
        <span
          dir="ltr"
          className={`glim-num text-[13px] font-semibold leading-none ${
            usage.reached ? 'text-statusFail' : 'text-carbon-text'
          }`}
        >
          {capped
            ? t('settings.volume.usedOf', { used: fmtGB(usage.used), cap: fmtGB(usage.cap) })
            : fmtGB(usage.used)}
        </span>
      </div>

      {/* Drawn only when there is something to be a fraction OF. With no cap set
          a track would be a bar that can never fill, which reads as a broken
          gauge rather than as an unlimited one. */}
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
        {/* The server's own instant, formatted in the reader's locale. This one
            IS a moment in time and not a calendar bucket, so fmtDate is right
            here where it would be wrong on a bucket key. */}
        <span className="text-carbon-textMuted">
          {t('settings.volume.resetsOn', { date: fmtDate(usage.periodEnd) })}
        </span>
        {/* Present only while it is true. A line reading "the cap is not
            reached" on every install would be a sentence nobody reads, and the
            state it announces is the one worth a word. */}
        {usage.reached && <span className="text-statusFail">{t('settings.volume.reached')}</span>}
        {/* Said out loud rather than implied by an absent cap: "no cap" is a
            real answer, and a figure with nothing beside it looks like a figure
            whose second half failed to load. */}
        {!capped && <span className="text-carbon-textMuted">{t('settings.volume.noCap')}</span>}
      </div>
    </div>
  );
}

/**
 * VolumeMeter is the same reading at shell-bar size.
 *
 * It renders nothing until the server has answered, and nothing at all while no
 * cap is set. That second refusal is the design: with volumeCap at 0 there is
 * no allowance for the figure to be measured against, and a bare byte count in
 * a bar already carrying a transport row, a curve and two controls would be a
 * number with no question behind it. Somebody who has not set a cap finds the
 * same figure on the Overview card and on the Downloads settings card, which is
 * where they would look for it.
 *
 * Mounted inside ShellStrip's own `local &&` branch, so it inherits that
 * branch's instance guard for free - and it has to. /api/stats is not on the
 * federation forwarder's list (routes_federation.go forwards links, tasks and
 * queue and answers 403 to everything else), so over a peer's list this could
 * only ever show THIS box's figure beside somebody else's downloads, which is
 * the exact confusion that branch exists to prevent.
 */
export function VolumeMeter() {
  const { t } = useT();
  const usage = useVolumeUsage();
  if (!usage || usage.cap <= 0) return null;

  const filled = Math.min(100, Math.round((usage.used / usage.cap) * 100));

  return (
    // w-full, matching SpeedLimitField directly above it: the two share the
    // shell strip's fixed column, so they share one edge rather than each
    // hugging its own text.
    <span className="flex w-full flex-col gap-1">
      <span className="flex items-center gap-1.5 text-[11px] text-carbon-textMuted">
        <span
          dir="ltr"
          className={`glim-num min-w-0 flex-1 truncate ${usage.reached ? 'text-statusFail' : 'text-carbon-text'}`}
        >
          {t('settings.volume.usedOf', { used: fmtGB(usage.used), cap: fmtGB(usage.cap) })}
        </span>
        <InfoBubble tip={t('volume.meterHint')} />
      </span>
      {/* Its own four-pixel track rather than components/ProgressBar, which is
          h-5 because it is the download list's rightmost column and the one
          thing on a row somebody watches. At that weight in here it would read
          louder than the speed limit and the menu button it sits under. */}
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
