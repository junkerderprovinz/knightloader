/**
 * AccountStrip is the shell bar's compact read of tier, traffic and expiry
 * for every configured, enabled debrid account - see docs/build-plan.md 6B.
 *
 * It NEVER calls TestAccount or any per-service API directly: everything
 * here comes from GET /api/accounts, which app.AccountStates() answers
 * purely from the account-health ticker's own cache
 * (internal/app/app_accounts.go) - never a call made while answering the
 * request. TestAccount is a live network call with a 15s timeout per
 * service; reading that here would fire up to three third-party calls on
 * every page load and let one slow debrid host stall this bar on every
 * route, not only the Accounts page. The ticker refreshes the cache on its
 * own schedule - this only ever reads what it last found.
 *
 * Mounted into app/Layout.tsx's shell-bar widget slot beside ShellStrip, not
 * onto the Accounts page: the whole point is that it stays visible while
 * navigating.
 */
import { useEffect, useState } from 'react';
import { type Account, type CatalogueService, fetchAccountCatalogue, fetchAccounts } from '../lib/api';
import { fmtBytes, fmtDate } from '../lib/format';
import { useT } from '../lib/i18n';
import { InfoBubble } from './ui';
import { HosterIcon } from './HosterIcon';

// Mirrors Accounts.tsx's own HEALTH_POLL_MS: both read the same server-side
// cache, so there is no reason for this to look more often than that page
// does - the underlying numbers move on the ticker's own much slower
// schedule (internal/app/app_accounts.go: accountHealthInterval), not this one.
const POLL_MS = 30000;

// Inside a week reads as "renews soon" - a colour worth noticing without
// being an alarm. Nothing further out than this is marked at all.
const EXPIRY_SOON_MS = 7 * 24 * 60 * 60 * 1000;

export function AccountStrip() {
  const { t } = useT();
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const [catalogue, setCatalogue] = useState<CatalogueService[]>([]);

  useEffect(() => {
    let live = true;
    const load = () => {
      Promise.all([fetchAccounts(), fetchAccountCatalogue()]).then(
        ([a, c]) => {
          if (!live) return;
          setAccounts(a);
          setCatalogue(c);
        },
        () => {
          // The bar simply keeps showing the last good read - a dropped poll
          // is not a reason to blank out a figure that was correct thirty
          // seconds ago.
        },
      );
    };
    load();
    const timer = window.setInterval(load, POLL_MS);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, []);

  if (!accounts) return null;
  const byId = new Map(catalogue.map((s) => [s.id, s]));
  // Only debrid rows that are actually in effect. An account switched off is
  // not backing any download right now, and showing its last-known traffic
  // beside the ones that are would read as one combined picture when it is
  // not - the same reasoning Accounts.tsx's own sections split on group for.
  const rows = accounts.filter((a) => a.enabled && byId.get(a.service)?.group === 'debrid');
  if (rows.length === 0) return null;

  return (
    <div className="flex items-center gap-3" role="group" aria-label={t('accountStrip.label')}>
      {rows.map((a) => (
        <AccountChip
          key={a.id}
          account={a}
          label={byId.get(a.service)?.label ?? a.service}
          site={byId.get(a.service)?.whereUrl ?? ''}
        />
      ))}
    </div>
  );
}

function AccountChip({ account, label, site }: { account: Account; label: string; site: string }) {
  const { t } = useT();
  const traffic = account.traffic;
  // Unlimited is checked FIRST, always - see app.TrafficState's own doc
  // comment. pct stays null rather than 0 for every other case (never
  // fetched, or a confirmed reading with nothing to meter), so nothing below
  // ever prints a percentage this account does not actually have.
  const pct = !traffic.unlimited && traffic.limit > 0 ? Math.min(100, Math.round((traffic.used / traffic.limit) * 100)) : null;

  const expiryMs = account.expiry ? new Date(account.expiry).getTime() : NaN;
  const expiresSoon = Number.isFinite(expiryMs) && expiryMs > Date.now() && expiryMs - Date.now() < EXPIRY_SOON_MS;

  return (
    <span className="flex items-baseline gap-1.5">
      {/* A dot, not a repeated date - the InfoBubble below already carries
          the exact one, and a second copy of it printed in the bar would be
          furniture on every page for a fact that changes once every few
          months. */}
      {expiresSoon && (
        <span className="h-1.5 w-1.5 shrink-0 rounded-[var(--radius-pill)] bg-statusWarnSolid" aria-hidden="true" />
      )}
      {/* The logo where the name used to be (jdp, 2026-09-05: "in der kopfcard
          im downloadtab soll der Hostername weg"). The name is not lost: it
          leads the bubble beside it now, so the one place that spells it out
          is the one a person opens to ask what this chip is about. A chip
          showing only a traffic figure would have been anonymous. */}
      <HosterIcon host={site} size={14} />
      <span className="sr-only">{label}</span>
      {traffic.unlimited ? (
        // The unlimited symbol this app already uses unlocalized elsewhere
        // (QueueBar.tsx's speed-limit placeholder) - never a percentage, and
        // never the word "Unlimited" needing a 39th translation for one
        // character that already reads the same in every one of them.
        <span className="glim-num text-[11px] font-semibold text-carbon-text">∞</span>
      ) : pct !== null ? (
        <span className="glim-num text-[11px] font-semibold text-carbon-text">{pct}%</span>
      ) : (
        <span className="text-[11px] text-carbon-textMuted">—</span>
      )}
      <InfoBubble tip={`${label} · ${chipTip(t, account)}`} />
    </span>
  );
}

// chipTip is the one place this widget explains itself - GlimStone puts
// explanations behind the (i), never as grey prose beside the label (see
// web/src/index.css's own design-language block). It is the only thing that
// tells "not checked yet" apart from "checked, nothing to meter" - the
// compact chip renders both as the same dash on purpose, because the bar has
// no room for the distinction and the tooltip does.
function chipTip(t: ReturnType<typeof useT>['t'], account: Account): string {
  if (account.tier === 'unknown') {
    return t('accountStrip.uncheckedHint');
  }
  const parts = [tierLabel(account.tier)];
  if (account.traffic.unlimited) {
    parts.push(t('accountStrip.unlimitedHint'));
  } else if (account.traffic.limit > 0) {
    parts.push(t('accountStrip.trafficHint', { used: fmtBytes(account.traffic.used), limit: fmtBytes(account.traffic.limit) }));
  }
  const expiry = fmtDate(account.expiry);
  if (expiry) {
    parts.push(t('accountStrip.expiryHint', { date: expiry }));
  }
  return parts.join(' · ');
}

// tierLabel is a plain capitalised read of the service's own vocabulary
// ("premium", "essential", "pro"...) rather than a translated string: these
// are provider-chosen plan names, the same reason AllDebrid, Real-Debrid and
// TorBox are not translated either, and the set is open-ended - a future
// service can add a tier this file has never heard of and it still reads
// sensibly.
function tierLabel(tier: string): string {
  return tier.length > 0 ? tier[0].toUpperCase() + tier.slice(1) : tier;
}
