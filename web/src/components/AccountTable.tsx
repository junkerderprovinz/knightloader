// One table for both account cards.
//
// It exists because the two cards drifted apart until they no longer looked
// like the same kind of thing (jdp, 2026-09-07: "bei beiden Cards (Debrid,
// hoster) sollen die spalten gleich sein"). The debrid table had a label column
// nobody used and a traffic column; the hoster table had neither, and its rows
// carried no plan, no expiry and no allowance at all - not because a hoster
// account has none, but because nobody had asked JDownloader for them.
//
// So the columns are decided HERE, once, and each card hands over rows in this
// shape. A column that only one of them can fill is still drawn for both, with
// a dash: two tables of different widths side by side read as two unrelated
// features, and the dash is the honest answer to "what does it say here".
import { useState, type ReactNode } from 'react';
import { useT } from '../lib/i18n';
import { fmtBytes, fmtDate } from '../lib/format';
import { IconBadge, Toggle } from './ui';
import { ProgressBar } from './ProgressBar';
import { HosterIcon } from './HosterIcon';
import { ContextMenu, anchorBelow, useContextMenu, type MenuGroup } from './ContextMenu';
import { IconSettings } from '../lib/icons';

/**
 * What one account's allowance looks like, in the three shapes the services
 * actually report it in. Nothing is derived from a missing field: every reader
 * below checks which of the three is present rather than treating 0 as an
 * answer - a fresh account really has used 0%, and that is not the same as a
 * service that said nothing (see app.TrafficState.PercentKnown).
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
  /** The status badge, drawn by the card - the two have genuinely different states. */
  status: ReactNode;
  /** "premium", "free", or anything the service calls its own plan. */
  tier?: string;
  /** RFC3339, or empty for an account with nothing to expire. */
  expiry?: string;
  traffic?: AccountTraffic;
  onToggle: (enabled: boolean) => void;
  /** Extra entries for this row's own menu, on top of Edit and Remove. */
  menu?: MenuGroup[];
  onEdit: () => void;
  onRemove?: () => void;
}

/** tierLabel keeps "premium" and "free" translated and anything else verbatim:
 *  a service's own plan name (TorBox's "essential") is a product name, not a
 *  word to translate, and inventing a category for it would be worse than
 *  printing what it calls itself. */
function TierCell({ tier }: { tier?: string }) {
  const { t } = useT();
  if (!tier || tier === 'unknown') return <span className="text-carbon-textMuted">—</span>;
  if (tier === 'premium') {
    return <span className="glim-eyebrow bg-statusOkBg text-statusOk">{t('accounts.tier.premium')}</span>;
  }
  if (tier === 'free') {
    return <span className="glim-eyebrow bg-carbon-surface3 text-carbon-textSub">{t('accounts.tier.free')}</span>;
  }
  return <span className="glim-eyebrow bg-carbon-surface3 text-carbon-textSub">{tier}</span>;
}

/**
 * The allowance as a bar (jdp, 2026-09-07: "Das verbliebene Volume soll als
 * progressbar angezeigt werden. in JD funktioniert das auch").
 *
 * The bar shows what is USED and the caption what is LEFT, which is the pairing
 * JDownloader's own account manager uses: a bar that fills as you spend, and a
 * number that answers "how much have I got".
 */
function TrafficCell({ traffic }: { traffic?: AccountTraffic }) {
  const { t } = useT();
  if (!traffic) return <span className="text-carbon-textMuted">—</span>;

  if (traffic.unlimited) {
    // No bar: a bar needs a full, and there is none. The symbol is the whole
    // statement, and it is the same one the speed-limit field uses for "off".
    return <span className="glim-num text-carbon-textSub">∞</span>;
  }

  const limit = traffic.limit ?? 0;
  if (limit > 0) {
    const used = Math.min(traffic.used ?? 0, limit);
    return (
      <span className="flex flex-col gap-1">
        <ProgressBar active percent={(used / limit) * 100} />
        <span className="glim-num text-[11px] text-carbon-textMuted">
          {t('accounts.trafficLeftOf', { left: fmtBytes(limit - used), total: fmtBytes(limit) })}
        </span>
      </span>
    );
  }

  if (traffic.percentKnown) {
    const usedPct = Math.max(0, Math.min(100, traffic.usedPercent ?? 0));
    return (
      <span className="flex flex-col gap-1">
        <ProgressBar active percent={usedPct} />
        <span className="glim-num text-[11px] text-carbon-textMuted">
          {t('accounts.trafficLeftPercent', { n: Math.floor(100 - usedPct) })}
        </span>
      </span>
    );
  }
  return <span className="text-carbon-textMuted">—</span>;
}

export function AccountTable({ rows, label }: { rows: AccountRow[]; label: string }) {
  const { t } = useT();
  const menu = useContextMenu();
  // The row the open menu belongs to, kept by KEY rather than by object: the
  // list is re-fetched on a poll, so the object identity a click captured is
  // gone a few seconds later while the row itself is still there. Held here
  // rather than inside each row so only one menu can be open at a time, which
  // is what a menu anchored to a table has to mean.
  const [menuKey, setMenuKey] = useState<string | null>(null);
  const open = rows.find((r) => r.key === menuKey);

  return (
    <div className="glim-well overflow-x-auto p-0">
      <table className="w-full min-w-[46rem] border-collapse text-sm" aria-label={label}>
        <thead>
          <tr className="text-start text-xs text-carbon-textMuted">
            <th className="w-12 px-4 py-3 text-start font-medium">{t('accounts.col.enabled')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.service')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.status')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.tier')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.expiry')}</th>
            <th className="w-52 px-2 py-3 text-start font-medium">{t('accounts.col.traffic')}</th>
            <th className="w-10 px-2 py-3">
              <span className="sr-only">{t('accounts.rowActions')}</span>
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-carbon-border/40">
          {rows.map((row, i) => (
            <tr key={row.key} className="group transition-colors hover:bg-carbon-hover">
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
                  {row.label}
                </span>
              </td>
              <td className="px-2 py-3">{row.status}</td>
              <td className="px-2 py-3">
                <TierCell tier={row.tier} />
              </td>
              {/* fmtDate, not the raw field: the server sends RFC 3339, and a
                  cell reading 2026-10-06T00:28:59Z is a timestamp somebody has
                  to decode rather than a date they can read. */}
              <td className="glim-num px-2 py-3 text-carbon-textSub">{fmtDate(row.expiry) || '—'}</td>
              <td className="px-2 py-3 text-carbon-textSub">
                <TrafficCell traffic={row.traffic} />
              </td>
              <td className="px-2 py-3 text-end">
                <IconBadge
                  hue={i}
                  className="opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100"
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
              id: 'danger',
              items: open.onRemove
                ? [
                    {
                      id: 'remove',
                      label: t('accounts.remove'),
                      danger: true,
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
