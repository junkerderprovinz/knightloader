// The table shared by the debrid and hoster account cards. Both get the same
// columns, and a column one card cannot fill shows a dash.
import { useState, type ReactNode } from 'react';
import { useT } from '../lib/i18n';
import { fmtDate, fmtGB } from '../lib/format';
import { IconBadge, InfoBubble, Toggle, useTooltip } from './ui';
import { ProgressBar } from './ProgressBar';
import { HosterIcon } from './HosterIcon';
import { ContextMenu, anchorBelow, useContextMenu, type MenuGroup } from './ContextMenu';
import { IconSettings } from '../lib/icons';

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

export interface AccountRow {
  key: string;
  /** Whatever HosterIcon can reduce to a hostname: a bare host or a full URL. */
  iconHost: string;
  label: string;
  /** How KnightLoader reaches the service when it is not direct, e.g. "through JDownloader". */
  via?: string;
  enabled: boolean;
  /** The status badge, drawn by the card, since the two have different states. */
  status: ReactNode;
  /** "premium", "free", or anything the service calls its own plan. */
  tier?: string;
  /** RFC3339, or empty for an account with nothing to expire. */
  expiry?: string;
  traffic?: AccountTraffic;
  onToggle: (enabled: boolean) => void;
  /**
   * The switch for picking up what is added on the service's own website.
   * Absent for an account whose service cannot list its downloads.
   */
  importing?: { on: boolean; onChange: (on: boolean) => void };
  /** Extra entries for this row's own menu, on top of Edit and Remove. */
  menu?: MenuGroup[];
  onEdit: () => void;
  onRemove?: () => void;
}

/**
 * TierCell translates "premium" and "free" and prints any other plan name, such
 * as TorBox's "essential", as the service's own product name, capitalised.
 */
function TierCell({ tier }: { tier?: string }) {
  const { t } = useT();
  if (!tier || tier === 'unknown') return <span className="text-carbon-textMuted">-</span>;
  if (tier === 'premium') return <span className="text-carbon-text">{t('accounts.tier.premium')}</span>;
  if (tier === 'free') return <span className="text-carbon-textMuted">{t('accounts.tier.free')}</span>;
  return <span className="text-carbon-text">{tier.charAt(0).toUpperCase() + tier.slice(1)}</span>;
}

/**
 * TrafficCell draws the allowance as a bar of what is used beside a caption of
 * what is left, on one line, as JDownloader's account manager does. Byte
 * figures use fmtGB, the unit vendors advertise in.
 */
function TrafficCell({ traffic }: { traffic?: AccountTraffic }) {
  const { t } = useT();
  if (!traffic) return <span className="text-carbon-textMuted">-</span>;

  if (traffic.unlimited) {
    // No bar without a limit. Some services, such as TorBox, still report the
    // total downloaded, which is shown beside the symbol.
    return (
      <span className="flex items-center gap-2">
        <span className="glim-num text-carbon-textSub">∞</span>
        {(traffic.used ?? 0) > 0 && (
          <span className="glim-num whitespace-nowrap text-[11px] text-carbon-textMuted">
            {t('accounts.trafficUsedTotal', { used: fmtGB(traffic.used ?? 0) })}
          </span>
        )}
      </span>
    );
  }

  const limit = traffic.limit ?? 0;
  if (limit > 0) {
    const used = Math.min(traffic.used ?? 0, limit);
    return (
      <span className="flex items-center gap-2">
        {/* The bar takes the leftover room so the figures align. */}
        <span className="min-w-0 flex-1">
          <ProgressBar active percent={(used / limit) * 100} />
        </span>
        <span className="glim-num shrink-0 whitespace-nowrap text-[11px] text-carbon-textMuted">
          {t('accounts.trafficLeftOf', { left: fmtGB(limit - used), total: fmtGB(limit) })}
        </span>
      </span>
    );
  }

  if (traffic.percentKnown) {
    const usedPct = Math.max(0, Math.min(100, traffic.usedPercent ?? 0));
    return (
      <span className="flex items-center gap-2">
        <span className="min-w-0 flex-1">
          <ProgressBar active percent={usedPct} />
        </span>
        <span className="glim-num shrink-0 whitespace-nowrap text-[11px] text-carbon-textMuted">
          {t('accounts.trafficLeftPercent', { n: Math.floor(100 - usedPct) })}
        </span>
      </span>
    );
  }
  // No quota reported, so no bar; an empty track would claim a limit of zero.
  return <UnknownTraffic />;
}

/** UnknownTraffic is the dash for an account that reports no quota, with the
 *  reason in a tooltip. A component of its own because the tooltip is a hook. */
function UnknownTraffic() {
  const { t } = useT();
  const tip = useTooltip<HTMLSpanElement>(t('accounts.trafficUnknown'));
  return (
    <>
      {/* A dash names nothing, so the reason is the accessible name too. */}
      <span className="text-carbon-textMuted" {...tip.triggerProps} aria-label={t('accounts.trafficUnknown')}>
        -
      </span>
      {tip.node}
    </>
  );
}

/**
 * AccountTable draws one card's accounts. `importColumn` adds the switch for
 * the import from each service's website, which only debrid services have.
 */
export function AccountTable({
  rows,
  label,
  importColumn = false,
}: {
  rows: AccountRow[];
  label: string;
  importColumn?: boolean;
}) {
  const { t } = useT();
  const menu = useContextMenu();
  // By key, because polling replaces the row objects; held here so only one
  // menu is open at a time.
  const [menuKey, setMenuKey] = useState<string | null>(null);
  const open = rows.find((r) => r.key === menuKey);

  return (
    // relative keeps the sr-only header cell inside this scroller. Placed
    // against the page, it would widen the whole page on a phone.
    <div className="glim-well relative overflow-x-auto p-0">
      <table
        className={`w-full border-collapse text-sm ${importColumn ? 'min-w-[50rem]' : 'min-w-[46rem]'}`}
        aria-label={label}
      >
        <thead>
          <tr className="text-start text-xs text-carbon-textMuted">
            <th className="w-12 px-4 py-3 text-start font-medium">{t('accounts.col.enabled')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.service')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.status')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.tier')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.expiry')}</th>
            <th className="w-52 px-2 py-3 text-start font-medium">{t('accounts.col.traffic')}</th>
            {importColumn && (
              <th className="w-24 ps-5 pe-2 py-3 text-start font-medium">
                <span className="inline-flex items-center">
                  {t('accounts.col.import')}
                  <InfoBubble tip={t('accounts.importHint')} />
                </span>
              </th>
            )}
            <th className="w-10 px-2 py-3">
              <span className="sr-only">{t('accounts.rowActions')}</span>
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-carbon-border/40">
          {rows.map((row, i) => (
            <tr key={row.key} className="transition-colors hover:bg-carbon-hover">
              <td className="px-4 py-3">
                <Toggle
                  checked={row.enabled}
                  onChange={row.onToggle}
                  label={t('accounts.enableAccount', { account: row.label })}
                  hideLabel
                />
              </td>
              <td className="px-2 py-3 font-medium text-carbon-text">
                <span className="inline-flex items-center gap-2">
                  <HosterIcon host={row.iconHost} />
                  <span className="flex min-w-0 flex-col">
                    {row.label}
                    {row.via && <span className="text-[11px] font-normal text-carbon-textMuted">{row.via}</span>}
                  </span>
                </span>
              </td>
              <td className="px-2 py-3">{row.status}</td>
              <td className="px-2 py-3">
                <TierCell tier={row.tier} />
              </td>
              <td className="glim-num px-2 py-3 text-carbon-textSub">{fmtDate(row.expiry) || '-'}</td>
              <td className="px-2 py-3 text-carbon-textSub">
                <TrafficCell traffic={row.traffic} />
              </td>
              {importColumn && (
                <td className="ps-5 pe-2 py-3">
                  {row.importing ? (
                    <Toggle
                      checked={row.importing.on}
                      onChange={row.importing.onChange}
                      label={t('accounts.importAccount', { account: row.label })}
                      hideLabel
                    />
                  ) : (
                    <span className="text-carbon-textMuted">-</span>
                  )}
                </td>
              )}
              <td className="px-2 py-3 text-end">
                <IconBadge
                  hue={i}
                  icon={<IconSettings width={16} height={16} />}
                  title={t('accounts.rowActions')}
                  aria-label={t('accounts.rowActions')}
                  onClick={(e) => {
                    setMenuKey(row.key);
                    menu.openAt(anchorBelow(e.currentTarget));
                  }}
                />
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {menu.anchor && open && (
        <ContextMenu
          anchor={menu.anchor}
          label={t('accounts.rowActions')}
          onClose={() => {
            menu.close();
            setMenuKey(null);
          }}
          groups={[
            ...(open.menu ?? []),
            {
              id: 'edit',
              items: [
                {
                  id: 'edit',
                  label: t('accounts.edit'),
                  onSelect: open.onEdit,
                },
              ],
            },
            {
              id: 'remove',
              items: open.onRemove
                ? [
                    {
                      id: 'remove',
                      label: t('accounts.remove'),
                      onSelect: open.onRemove,
                    },
                  ]
                : [],
            },
          ]}
        />
      )}
    </div>
  );
}
