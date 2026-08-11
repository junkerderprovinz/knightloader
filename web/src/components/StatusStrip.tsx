import { useEffect, useState, type ReactElement } from 'react';
import { connectWS } from '../lib/api';
import { useT } from '../lib/i18n';
import { IconCaptcha, IconCheck, IconGlobe, IconSearch } from '../lib/icons';

type Translate = ReturnType<typeof useT>['t'];

// Ambient background work KnightLoader does on its own: a page being
// crawled, an availability recheck, the captcha poll loop, an unattended
// auto-confirm pass. Broadcast over the hub as "activity" - see this file's
// own useEffect for the one place it is read. No REST route of its own:
// unlike a captcha challenge this is not durable state worth a GET, only a
// live signal for whatever is happening right now.
//
// Declared here rather than in lib/api.ts, where every other WS payload
// type (Task, CaptchaChallenge, ...) otherwise lives: lib/api.ts is not in
// this wave's file list (build-plan.md section 3's Wave 9 table names only
// this component and Layout.tsx for 9A), and this pair is small and used
// nowhere else. A later pass folding every WS payload type back into one
// place is a reasonable follow-up, not a change to make by reaching into a
// file this wave does not assign anyone.

export type ActivityKind = 'crawl' | 'linkcheck' | 'captcha' | 'autoconfirm';

/**
 * One kind's current counters - app.Activity (internal/app/app_activity.go)
 * verbatim. Active never exceeds Total for a burst kind (crawl, linkcheck,
 * autoconfirm); for captcha the two are always equal - see that struct's own
 * doc comment for why.
 */
export interface ActivitySignal {
  kind: ActivityKind;
  active: number;
  total: number;
}

/**
 * StatusStrip is the app's own ambient-activity tray: whatever KnightLoader
 * is doing on its own right now - crawling a pasted page, rechecking link
 * availability, polling for a captcha, running an unattended auto-confirm
 * pass - shown as real counts, not a spinner with nothing behind it. See
 * build-plan.md section 3's Wave 9 table (9A) and section 8's own Wave 9
 * note: "a typed job with counters, not a free-text status line."
 *
 * Mounted once, beside CaptchaModal and GlobalIntake (app/Layout.tsx), for
 * the identical reason those two are: none of the three have anything to do
 * with which page is open, and a copy mounted per page would remount - and
 * lose whatever it was showing - on every navigation.
 *
 * Renders nothing the moment every kind is idle, the same way CaptchaModal
 * renders nothing with no challenge open. A strip that is a permanent
 * fixture reading "0 active" on every page is exactly the static spinner
 * this exists to not be.
 *
 * Labels route through useT() against the seven `activity.*` keys 9E added
 * to every locale (en.ts plus 41 translations) - minted for this component
 * specifically, per that pass's own report, since nothing existing fit a
 * live ambient-work strip.
 */

// Fixed, not the order kinds happen to arrive in over the wire - a strip
// that reorders itself as bursts start and stop is one a person cannot
// scan at a glance.
const ORDER: ActivityKind[] = ['crawl', 'linkcheck', 'captcha', 'autoconfirm'];

const LABEL_KEY: Record<ActivityKind, 'activity.crawl' | 'activity.linkcheck' | 'activity.captcha' | 'activity.autoconfirm'> = {
  crawl: 'activity.crawl',
  linkcheck: 'activity.linkcheck',
  captcha: 'activity.captcha',
  autoconfirm: 'activity.autoconfirm',
};

function kindIcon(kind: ActivityKind): ReactElement {
  const p = { width: 13, height: 13 };
  switch (kind) {
    case 'crawl':
      return <IconGlobe {...p} />;
    case 'linkcheck':
      return <IconSearch {...p} />;
    case 'captcha':
      return <IconCaptcha {...p} />;
    case 'autoconfirm':
      return <IconCheck {...p} />;
  }
}

// captcha is a live gauge - how many are outstanding right now, not a
// countdown from a burst size - see app.Activity's own doc comment
// (internal/app/app_activity.go). The other three read as "N of M" for the
// whole burst, from the first tick to the last - always that one form, never
// switching to a bare active count while total==active. A burst of 5 that
// opened by showing "5" and then switched to "1 of 5" the moment the first
// one finished read as the number falling, not as the fraction filling in.
function formatCount(t: Translate, kind: ActivityKind, s: ActivitySignal): string {
  if (kind === 'captcha') {
    return t('activity.pending', { n: s.active });
  }
  return t('activity.ofTotal', { n: s.total - s.active, total: s.total });
}

export function StatusStrip() {
  const { t } = useT();
  const [signals, setSignals] = useState<Partial<Record<ActivityKind, ActivitySignal>>>({});

  useEffect(() => {
    // 'activitySnapshot' is not in kinds below and still arrives every time:
    // the server sends it with Hub.SendTo, not Broadcast, which bypasses a
    // connection's own subscription filter entirely (internal/hub/hub.go).
    // 'activity' is the only Broadcast kind this component ever reads.
    return connectWS(
      (type, data) => {
        if (type === 'activitySnapshot') {
          // Sent once per connection (serveWS) - the authoritative current
          // state, replacing whatever this browser last heard before a drop
          // and reconnect. A single "activity" message below only ever
          // patches one kind, which is exactly how a burst that ended while
          // this client was disconnected became a phantom that never cleared.
          const list = (data ?? []) as ActivitySignal[];
          setSignals(Object.fromEntries(list.map((s) => [s.kind, s])));
        } else if (type === 'activity') {
          const s = data as ActivitySignal;
          setSignals((prev) => ({ ...prev, [s.kind]: s }));
        }
      },
      ['activity'],
    );
  }, []);

  const rows = ORDER.map((kind) => signals[kind]).filter((s): s is ActivitySignal => !!s && s.active > 0);
  if (rows.length === 0) return null;

  return (
    <div className="fixed top-20 right-5 z-30">
      <div role="status" aria-live="polite" className="glim-card glim-fade flex min-w-[190px] flex-col gap-2 px-3.5 py-3">
        {rows.map((s) => (
          <div
            key={s.kind}
            className="flex items-center gap-2 text-[11px]"
            title={`${t(LABEL_KEY[s.kind])}: ${t('activity.tooltipHint', { active: s.active, total: s.total })}`}
          >
            <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-accent glim-live" aria-hidden="true" />
            <span className="text-carbon-textMuted">{kindIcon(s.kind)}</span>
            <span className="text-carbon-textMuted">{t(LABEL_KEY[s.kind])}</span>
            <span className="flex-1" />
            <span className="glim-num font-semibold text-carbon-text">{formatCount(t, s.kind, s)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
