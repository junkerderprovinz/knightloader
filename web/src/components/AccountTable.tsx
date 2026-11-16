// The table shared by the debrid and hoster account cards. Both get the same
// columns, and a column one card cannot fill shows a dash.
import { useState, type CSSProperties, type ReactNode } from 'react';
import { useT } from '../lib/i18n';
import { fmtDate, fmtGB } from '../lib/format';
import { IconBadge, useTooltip } from './ui';
import { ProgressBar } from './ProgressBar';
import { HosterIcon } from './HosterIcon';
import { ContextMenu, anchorFromEvent, useContextMenu } from './ContextMenu';
import { IconPencil, IconPower, IconTrash } from '../lib/icons';

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
  enabled: boolean;
  /** The status badge, drawn by the card, since the two have different states. */
  status: ReactNode;
  /** "premium", "free", or anything the service calls its own plan. */
  tier?: string;
  /** RFC3339, or empty for an account with nothing to expire. */
  expiry?: string;
  traffic?: AccountTraffic;
  /**
   * Absent for an account that is in use whenever it is stored, such as a
   * site's sign-in. The card's gear (AccountSwitches) and the row's own
   * button switch it; the row only says when it is disabled.
   */
  onToggle?: (enabled: boolean) => void;
  /** The row's own options, shown before the switch, Edit and Remove. */
  actions?: AccountAction[];
  onEdit: () => void;
  onRemove?: () => void;
}

/** AccountAction is one option of a row, drawn as a button at the row's end. */
export interface AccountAction {
  id: string;
  label: string;
  icon: ReactNode;
  disabled?: boolean;
  onSelect: () => void;
}

/**
 * rowActions is every option of a row in the order all cards share: the
 * row's own, then the switch, Edit and Remove. Remove is last and looks like
 * the others, since the dialog it opens does the warning.
 */
function rowActions(row: AccountRow, t: ReturnType<typeof useT>['t']): AccountAction[] {
  const glyph = { width: 16, height: 16 };
  return [
    ...(row.actions ?? []),
    ...(row.onToggle
      ? [
          {
            id: 'switch',
            label: row.enabled ? t('menu.disable') : t('menu.enable'),
            icon: <IconPower {...glyph} />,
            onSelect: () => row.onToggle?.(!row.enabled),
          },
        ]
      : []),
    { id: 'edit', label: t('accounts.edit'), icon: <IconPencil {...glyph} />, onSelect: row.onEdit },
    ...(row.onRemove ? [{ id: 'remove', label: t('accounts.remove'), icon: <IconTrash {...glyph} />, onSelect: row.onRemove }] : []),
  ];
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
          <span className="glim-num whitespace-nowrap text-meta text-carbon-textMuted">
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
          <ProgressBar active level percent={(used / limit) * 100} />
        </span>
        <span className="glim-num shrink-0 whitespace-nowrap text-meta text-carbon-textMuted">
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
          <ProgressBar active level percent={usedPct} />
        </span>
        <span className="glim-num shrink-0 whitespace-nowrap text-meta text-carbon-textMuted">
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

/** Clipped is a name cut short to fit its column, shown whole in a tooltip while it is cut. */
function Clipped({ text }: { text: string }) {
  const [cut, setCut] = useState(false);
  const tip = useTooltip<HTMLSpanElement>(cut ? text : null);
  const { ref, onMouseEnter, onMouseLeave } = tip.triggerProps;
  return (
    <>
      <span
        ref={ref}
        className="block truncate"
        aria-describedby={cut ? tip.triggerProps['aria-describedby'] : undefined}
        onMouseEnter={() => {
          const el = ref.current;
          const now = !!el && el.scrollWidth > el.clientWidth;
          setCut(now);
          if (now) onMouseEnter();
        }}
        onMouseLeave={onMouseLeave}
      >
        {text}
      </span>
      {cut && tip.node}
    </>
  );
}

/** DisabledStatus stands in for the status of an account that is switched off. */
function DisabledStatus() {
  const { t } = useT();
  return (
    <span className="inline-flex items-center gap-1.5 text-meta font-medium text-carbon-textMuted">
      <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-carbon-textMuted" />
      {t('accounts.disabled')}
    </span>
  );
}

/** AccountTable draws one card's accounts. */
export function AccountTable({ rows, label }: { rows: AccountRow[]; label: string }) {
  const { t } = useT();
  const menu = useContextMenu();
  // By key, because polling replaces the row objects; held here so only one
  // menu is open at a time.
  const [menuKey, setMenuKey] = useState<string | null>(null);
  const open = rows.find((r) => r.key === menuKey);

  const most = Math.max(0, ...rows.map((r) => rowActions(r, t).length));
  // The touch layout's column for the buttons, wide enough for the row with the most.
  const room = { '--row-actions': `calc(${most} * var(--btn-h) + ${Math.max(most - 1, 0) * 0.25 + 0.5}rem)` } as CSSProperties;

  return (
    // relative keeps the sr-only header cell inside this scroller. Placed
    // against the page, it would widen the whole page on a phone.
    <div data-new="account-rows" className="kl-accounts glim-well relative overflow-x-auto p-0" style={room}>
      <table className="w-full table-fixed border-collapse text-sm" aria-label={label}>
        <thead>
          <tr className="text-start text-xs text-carbon-textMuted">
            <th className="truncate px-4 py-3 text-start font-medium">{t('accounts.col.service')}</th>
            <th className="kl-col-status w-36 truncate px-2 py-3 text-start font-medium">{t('accounts.col.status')}</th>
            <th className="kl-col-plan w-20 truncate px-2 py-3 text-start font-medium">{t('accounts.col.tier')}</th>
            <th className="kl-col-expiry w-36 truncate px-2 py-3 text-start font-medium">{t('accounts.col.expiry')}</th>
            <th className="kl-col-traffic w-44 truncate px-2 py-3 text-start font-medium">{t('accounts.col.traffic')}</th>
            <th className="kl-col-actions">
              <span className="sr-only">{t('accounts.rowActions')}</span>
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-carbon-border/40">
          {rows.map((row) => {
            const off = !!row.onToggle && !row.enabled;
            const status = off ? <DisabledStatus /> : row.status;
            return (
              // The whole row opens its account, and a right click its menu
              // (GlimStone, "Lists"). A press on one of the row's own controls
              // is that control's alone.
              // The row sets its own height and the cells add none, so a status
              // with a bubble beside it keeps every card's rows level.
              <tr
                key={row.key}
                className="glim-row h-11 cursor-pointer transition-colors hover:bg-carbon-surface3 focus-within:bg-carbon-surface3"
                onClick={(e) => {
                  if (!(e.target as Element).closest('button, a, [role="note"]')) row.onEdit();
                }}
                onContextMenu={(e) => {
                  e.preventDefault();
                  setMenuKey(row.key);
                  menu.openAt(anchorFromEvent(e));
                }}
              >
                <td className={`overflow-hidden px-4 py-0 font-medium ${off ? 'text-carbon-textMuted' : 'text-carbon-text'}`}>
                  <span className="flex min-w-0 items-center gap-2">
                    {/* Faded with the name, so a disabled account reads as resting at a glance. */}
                    <span className={`shrink-0 ${off ? 'opacity-50 grayscale' : ''}`}>
                      <HosterIcon host={row.iconHost} />
                    </span>
                    <span className="min-w-0">
                      <Clipped text={row.label} />
                      <span className="kl-status-under font-normal">{status}</span>
                    </span>
                  </span>
                </td>
                <td className="kl-col-status overflow-hidden px-2 py-0">{status}</td>
                <td className="kl-col-plan truncate px-2 py-0">
                  <TierCell tier={row.tier} />
                </td>
                <td className="kl-col-expiry glim-num truncate px-2 py-0 text-carbon-textSub">{fmtDate(row.expiry) || '-'}</td>
                <td className="kl-col-traffic overflow-hidden px-2 py-0 text-carbon-textSub">
                  <TrafficCell traffic={row.traffic} />
                </td>
                <td className="kl-col-actions py-0">
                  <span className="glim-row-actions">
                    {rowActions(row, t).map((a, j) => (
                      <IconBadge
                        key={a.id}
                        quiet
                        hue={j}
                        icon={a.icon}
                        title={a.label}
                        disabled={a.disabled}
                        onClick={a.onSelect}
                      />
                    ))}
                  </span>
                </td>
              </tr>
            );
          })}
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
          groups={[{ id: 'actions', items: rowActions(open, t) }]}
        />
      )}
    </div>
  );
}
