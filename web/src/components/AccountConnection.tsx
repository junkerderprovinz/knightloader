// What the accounts page's cards share to draw a connected service as a
// ConnectionRow: the service's mark, its plan and how long it runs as the
// status line, what is left of its allowance as a line under it, and the
// switch that every stored account has beside Test, Edit and Remove.
import { useState, type ComponentProps, type ReactNode } from 'react';
import type { TileMark } from '../lib/catalogs/catalog';
import { fmtDate, fmtGB } from '../lib/format';
import { useT } from '../lib/i18n';
import { IconPower } from '../lib/icons';
import { ConnectionRow, type ConnectionAction } from './ConnectionRow';
import { HosterIcon, hostPaint, type Paint } from './HosterIcon';
import { ProgressBar } from './ProgressBar';

type Translate = ReturnType<typeof useT>['t'];

/**
 * AccountTraffic is an allowance in one of the three shapes services report:
 * unlimited, bytes, or a percentage. A 0 is never read as "unknown" (see
 * app.TrafficState.PercentKnown).
 */
export interface AccountTraffic {
  unlimited?: boolean;
  /** Bytes, when the service quotes bytes. limit 0 means it does not. */
  used?: number;
  limit?: number;
  /** A percentage of an allowance the service does not express in bytes. */
  usedPercent?: number;
  percentKnown?: boolean;
}

/**
 * ServiceConnection is a ConnectionRow for a service known by its host: the
 * catalog's mark where GlimStone has one, else the logo HosterIcon finds for
 * the host, lit in the colours read from it.
 */
export function ServiceConnection({
  catalogMark,
  host,
  ...row
}: Omit<ComponentProps<typeof ConnectionRow>, 'mark' | 'paint'> & { catalogMark?: TileMark; host: string }) {
  const [read, setRead] = useState<Paint>();
  if (catalogMark) return <ConnectionRow mark={catalogMark} {...row} />;
  return <ConnectionRow mark={<HosterIcon host={host} size={40} lit onPaint={setRead} />} paint={read ?? hostPaint(host)} {...row} />;
}

/** ConnectionList holds one card's rows, named after the card. */
export function ConnectionList({ label, children, ...rest }: { label: string; children: ReactNode } & ComponentProps<'div'>) {
  return (
    <div role="group" aria-label={label} className="flex flex-col gap-2" {...rest}>
      {children}
    </div>
  );
}

/**
 * planLine is the status line of an account that works: its plan and how long
 * it runs. "premium" and "free" are translated, and any other plan, such as
 * TorBox's "essential", is the service's own product name.
 */
export function planLine(t: Translate, tier?: string, expiry?: string): string | undefined {
  const plan =
    !tier || tier === 'unknown'
      ? ''
      : tier === 'premium'
        ? t('accounts.tier.premium')
        : tier === 'free'
          ? t('accounts.tier.free')
          : tier.charAt(0).toUpperCase() + tier.slice(1);
  const until = expiry ? t('accounts.runsUntil', { date: fmtDate(expiry) }) : '';
  return [plan, until].filter(Boolean).join(' · ') || undefined;
}

/** switchAction is the row's button that disables a stored account or enables it again. */
export function switchAction(t: Translate, enabled: boolean, onToggle: (enabled: boolean) => void): ConnectionAction {
  return {
    id: 'switch',
    label: enabled ? t('menu.disable') : t('menu.enable'),
    icon: <IconPower />,
    onClick: () => onToggle(!enabled),
  };
}

/**
 * TrafficLine is what is left of an allowance, as a short bar of what is used
 * beside the words, or nothing for a service that reports none. Byte figures
 * use fmtGB, the unit vendors advertise in.
 */
export function TrafficLine({ traffic }: { traffic?: AccountTraffic }) {
  const { t } = useT();
  if (!traffic) return null;
  if (traffic.unlimited) {
    // No bar without a limit. Some services, such as TorBox, still report the
    // total downloaded, which is shown beside the symbol.
    return (
      <span className="glim-num flex items-center gap-2">
        <span>∞</span>
        {(traffic.used ?? 0) > 0 && t('accounts.trafficUsedTotal', { used: fmtGB(traffic.used ?? 0) })}
      </span>
    );
  }
  const limit = traffic.limit ?? 0;
  if (limit > 0) {
    const used = Math.min(traffic.used ?? 0, limit);
    return <Allowance percent={(used / limit) * 100} words={t('accounts.trafficLeftOf', { left: fmtGB(limit - used), total: fmtGB(limit) })} />;
  }
  if (traffic.percentKnown) {
    const usedPct = Math.max(0, Math.min(100, traffic.usedPercent ?? 0));
    return <Allowance percent={usedPct} words={t('accounts.trafficLeftPercent', { n: Math.floor(100 - usedPct) })} />;
  }
  return null;
}

function Allowance({ percent, words }: { percent: number; words: string }) {
  return (
    <span className="flex items-center gap-2">
      <span className="w-24 shrink-0">
        <ProgressBar active level percent={percent} />
      </span>
      <span className="glim-num min-w-0">{words}</span>
    </span>
  );
}
