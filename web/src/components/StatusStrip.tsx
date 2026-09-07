import { useCallback, useEffect, useState, type ReactElement } from 'react';
import { abortActivity, connectWS } from '../lib/api';
import { useT } from '../lib/i18n';
import { IconArchive, IconCaptcha, IconCheck, IconClose, IconGlobe, IconSearch } from '../lib/icons';
import { Button, InfoBubble } from './ui';

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

export type ActivityKind = 'crawl' | 'linkcheck' | 'captcha' | 'autoconfirm' | 'container';

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
  /**
   * How many of the active runs registered a stop handle, and therefore
   * whether this row gets a stop button at all.
   *
   * A count and not a flag because that is what the server derives it from -
   * the very map its abort route walks - so the button can never be offered
   * for work that has nothing to call off. Not every kind can have one: a
   * captcha poll is a timer nobody waits on and an auto-confirm pass is over
   * before a button could be pressed, while a page crawl is minutes of
   * somebody else's server not answering.
   *
   * Optional on the type because a server older than the field simply does not
   * send it, and an absent count has to read as "nothing to stop" rather than
   * as a button that answers 400.
   */
  cancellable?: number;
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
const ORDER: ActivityKind[] = ['crawl', 'linkcheck', 'captcha', 'autoconfirm', 'container'];

const LABEL_KEY: Record<
  ActivityKind,
  'activity.crawl' | 'activity.linkcheck' | 'activity.captcha' | 'activity.autoconfirm' | 'activity.container'
> = {
  crawl: 'activity.crawl',
  linkcheck: 'activity.linkcheck',
  captcha: 'activity.captcha',
  autoconfirm: 'activity.autoconfirm',
  container: 'activity.container',
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
    case 'container':
      return <IconArchive {...p} />;
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
  // container is a gauge for the same reason captcha is: "how many were
  // handed over and not yet collected" has no batch size to be a fraction
  // of. It reuses activity.pending rather than minting a second key for the
  // same sentence in 42 locales.
  if (kind === 'captcha' || kind === 'container') {
    return t('activity.pending', { n: s.active });
  }
  return t('activity.ofTotal', { n: s.total - s.active, total: s.total });
}

export function StatusStrip() {
  const { t } = useT();
  const [signals, setSignals] = useState<Partial<Record<ActivityKind, ActivitySignal>>>({});

  // Nothing is done with the answer, deliberately, and nothing is caught into
  // a toast either. The row IS the feedback: the server broadcasts the new
  // counters the moment the run unwinds, so a successful stop makes the row
  // disappear on its own, and a stop that found nothing left to cancel was
  // pressed on a run that had already finished - which is the same outcome the
  // person wanted and not a failure to report.
  const stop = useCallback((kind: ActivityKind) => {
    void abortActivity(kind).catch(() => {
      /* the row stays until the server says otherwise, which is the truth */
    });
  }, []);

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
          <div key={s.kind} className="flex items-center gap-2 text-[11px]">
            <span className="h-1.5 w-1.5 shrink-0 rounded-[var(--radius-pill)] bg-accent glim-live" aria-hidden="true" />
            <span className="text-carbon-textMuted">{kindIcon(s.kind)}</span>
            <span className="text-carbon-textMuted">{t(LABEL_KEY[s.kind])}</span>
            <span className="flex-1" />
            <span className="glim-num font-semibold text-carbon-text">{formatCount(t, s.kind, s)}</span>
            {/* The row's own label and count are already visible beside this -
                the bubble adds the one thing they don't say: active vs. total
                for the whole run, not just this instant's count. */}
            <InfoBubble tip={t('activity.tooltipHint', { active: s.active, total: s.total })} />
            {/* Shown only while the server says there is something to call off,
                which for a three-level crawl of a slow site is the difference
                between waiting five minutes and not. A button on a row that
                cannot be stopped would be one that does nothing when pressed. */}
            {(s.cancellable ?? 0) > 0 && (
              <Button
                kind="ghost"
                className="px-1"
                title={t('activity.stop')}
                icon={<IconClose width={12} height={12} />}
                onClick={() => stop(s.kind)}
              />
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
