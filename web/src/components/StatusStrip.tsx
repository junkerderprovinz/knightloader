import { useCallback, useEffect, useState, type ReactElement } from 'react';
import { abortActivity, connectWS } from '../lib/api';
import { fmtCountdown, goTimeMs, useCountdown } from '../lib/countdown';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconArchive, IconCaptcha, IconCheck, IconClose, IconGlobe, IconSearch } from '../lib/icons';
import { Button, InfoBubble } from './ui';

type Translate = ReturnType<typeof useT>['t'];

// Background work the app does on its own, broadcast over the hub as
// "activity". It is a live signal only, with no REST route.

export type ActivityKind = 'crawl' | 'linkcheck' | 'captcha' | 'autoconfirm' | 'container';

/** ActivitySignal mirrors app.Activity in internal/app/app_activity.go. */
export interface ActivitySignal {
  kind: ActivityKind;
  active: number;
  total: number;
  /**
   * How many active runs registered a stop handle, which decides whether the
   * row offers a stop button. Absent from older servers, meaning nothing to stop.
   */
  cancellable?: number;
  /**
   * When the soonest countdown of this kind runs out, as a Go timestamp.
   * Absent while nothing counts down, and from older servers.
   */
  deadline?: string;
}

// A fixed order, so rows do not jump as bursts start and stop.
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

// Captcha and container counts are gauges of what is outstanding. The burst
// kinds always read "N of M", so the figure fills up rather than jumping from
// a bare count to a fraction.
function formatCount(t: Translate, kind: ActivityKind, s: ActivitySignal): string {
  if (kind === 'captcha' || kind === 'container') {
    return t('activity.pending', { n: s.active });
  }
  return t('activity.ofTotal', { n: s.total - s.active, total: s.total });
}

// The kinds whose runs can wait before they act, with the words a row uses
// while one is counting down.
const COUNTDOWN_KEYS: Partial<Record<ActivityKind, { tip: TranslationKey; stop: TranslationKey }>> = {
  autoconfirm: { tip: 'activity.autoconfirmCountdown', stop: 'activity.autoconfirmStop' },
};

/** countdownOf is the row's countdown, or null while it is not counting down. */
function countdownOf(s: ActivitySignal): { due: number; tip: TranslationKey; stop: TranslationKey } | null {
  const keys = COUNTDOWN_KEYS[s.kind];
  const due = goTimeMs(s.deadline);
  return keys && due !== null ? { due, ...keys } : null;
}

/**
 * TimeLeft is the only part of the strip that ticks. role="timer" keeps the
 * strip's live region from reading out every second. A countdown that has run
 * out reads 0s until the server retires the row.
 */
function TimeLeft({ due }: { due: number }) {
  const secs = useCountdown(due);
  return <span role="timer">{fmtCountdown(secs ?? 0)}</span>;
}

/**
 * StatusStrip shows the app's background work as live counts, mounted once in
 * app/Layout.tsx, and renders nothing while every kind is idle.
 */
export function StatusStrip() {
  const { t } = useT();
  const [signals, setSignals] = useState<Partial<Record<ActivityKind, ActivitySignal>>>({});

  // The server broadcasts new counters once the run unwinds, so the row itself
  // is the feedback. A stop that finds nothing left was pressed on a finished run.
  const stop = useCallback((kind: ActivityKind) => {
    void abortActivity(kind).catch(() => {});
  }, []);

  useEffect(() => {
    // 'activitySnapshot' arrives without a subscription because the server
    // sends it with Hub.SendTo, which bypasses the filter.
    return connectWS(
      (type, data) => {
        if (type === 'activitySnapshot') {
          // Sent once per connection and replaces everything, so a burst that
          // ended during a disconnect does not linger.
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
    <div className="fixed top-20 end-5 z-30">
      <div role="status" aria-live="polite" className="glim-card glim-fade flex min-w-[190px] flex-col gap-2 px-3.5 py-3">
        {rows.map((s) => {
          const countdown = countdownOf(s);
          return (
            <div key={s.kind} className="flex items-center gap-2 text-[11px]">
              <span className="h-1.5 w-1.5 shrink-0 rounded-[var(--radius-pill)] bg-accent glim-live" aria-hidden="true" />
              <span className="text-carbon-textMuted">{kindIcon(s.kind)}</span>
              <span className="text-carbon-textMuted">{t(LABEL_KEY[s.kind])}</span>
              <span className="flex-1" />
              <span className="glim-num font-semibold text-carbon-text">
                {countdown ? <TimeLeft due={countdown.due} /> : formatCount(t, s.kind, s)}
              </span>
              <InfoBubble
                tip={countdown ? t(countdown.tip) : t('activity.tooltipHint', { active: s.active, total: s.total })}
              />
              {(s.cancellable ?? 0) > 0 && (
                <Button
                  kind="ghost"
                  className="px-1"
                  title={t(countdown ? countdown.stop : 'activity.stop')}
                  icon={<IconClose width={12} height={12} />}
                  onClick={() => stop(s.kind)}
                />
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
