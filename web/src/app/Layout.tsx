import { useEffect, useRef } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { Sidebar } from '../components/Sidebar';
import { QueueBar } from '../components/QueueBar';
import { QuickSettings } from '../components/QuickSettings';
import { SpeedMeter } from '../components/SpeedGraph';
import { VolumeMeter } from '../components/VolumeMeter';
import { CaptchaModal } from '../components/CaptchaModal';
import { CommandDispatcher } from '../components/CommandDispatcher';
import { CommandPalette } from '../components/CommandPalette';
import { GlobalIntake } from '../components/GlobalIntake';
import { IdleActionBanner } from '../components/IdleActionBanner';
import { OnboardingWizard } from '../components/OnboardingWizard';
import { StatusStrip } from '../components/StatusStrip';
import { InfoBubble } from '../components/ui';
import { connectWS, fetchDeploymentInfo, fetchSettings, fetchUpdateCheck, installUpdate, type Task } from '../lib/api';
import {
  applyAccent,
  applyRainbow,
  applyShape,
  cacheAppearance,
  rainbowFromSettings,
  readCachedDisco,
} from '../lib/appearance';
import { applyDisco } from '../lib/disco';
import { InstanceProvider, useInstanceScope } from '../lib/instance';
import { useToast } from '../lib/toast';
import { useExtractionToasts } from '../lib/useExtractionToasts';
import { useTasks } from '../lib/useTasks';
import { useT } from '../lib/i18n';

// Global completion toasts: watch the local task stream and notify when a
// download finishes or fails (transition only, so the initial snapshot is quiet).
function useCompletionToasts() {
  const { toast } = useToast();
  const { t } = useT();
  const prev = useRef<Record<string, string>>({});
  useEffect(() => {
    // The snapshot is a direct send and arrives without being in kinds.
    return connectWS(
      (type, data) => {
        if (type === 'snapshot') {
          prev.current = Object.fromEntries((data ?? []).map((t: Task) => [t.id, t.status]));
        } else if (type === 'task') {
          const before = prev.current[data.id];
          prev.current[data.id] = data.status;
          if (before && before !== data.status) {
            const name = data.name || t('nav.downloads');
            // The task target lets the event log jump to the row. The stream
            // has no base, so the id is always one on this instance.
            if (data.status === 'done')
              toast(t('downloads.finished', { name }), 'ok', 'download-done', undefined, { kind: 'task', id: data.id });
            else if (data.status === 'error')
              toast(t('downloads.failed', { name }), 'fail', 'download-failed', undefined, { kind: 'task', id: data.id });
          }
        } else if (type === 'removed') {
          delete prev.current[data.id];
        }
      },
      ['task', 'removed'],
    );
  }, [toast, t]);
}

// Shape and accent are server settings, so they follow the instance. They are
// applied here at the top so every page gets them, not just the settings page.
// Motion and disco are stored in the browser and applyCachedAppearance has
// already applied them.
function useAppearance() {
  useEffect(() => {
    let live = true;
    fetchSettings()
      .then((s) => {
        if (!live) return;
        const rainbow = rainbowFromSettings(s);
        applyShape(s.shape);
        applyAccent(s.accent);
        applyRainbow(rainbow);
        // A running walk takes up the server's palette where it is.
        applyDisco(readCachedDisco());
        // Cached so the next load paints the chosen look immediately instead of
        // flashing the default while this request is in flight.
        cacheAppearance(s.shape, s.accent, rainbow);
      })
      .catch(() => {
        // The cached look from the last successful load stays.
      });
    return () => {
      live = false;
    };
  }, []);
}

/**
 * ShellSpeed is the shell bar's speed curve, summed over the running tasks of
 * the shell's scope. Its own component, so a websocket frame redraws the curve
 * and not the whole bar.
 */
function ShellSpeed({ instance }: { instance: string }) {
  const tasks = useTasks(instance);
  let speed = 0;
  for (const id in tasks) {
    if (tasks[id].status === 'running') speed += tasks[id].speed;
  }
  // The history is seeded only for the local instance, since /api/stats/speed
  // is not forwarded; a peer's curve starts live.
  return <SpeedMeter value={speed} instance={instance} />;
}

/**
 * The download page's head card with the transport controls and the speed
 * curve. It sits above the keyed page div, which remounts on navigation, so
 * the controls keep their state; outside Downloads it is hidden rather than
 * unmounted for the same reason.
 *
 * A bottom margin and no top one, so it starts on the sidebar's line.
 *
 * It is as tall as its content: the row of squares, or the curve with its two
 * lines of figures, whichever is taller. The curve takes no height of its own
 * (SpeedGraph.tsx, check-stretched-svg-height.mjs), so it cannot grow the card.
 * In a narrow window the curve wraps under the squares.
 */
function ShellBar({ visible }: { visible: boolean }) {
  const { t } = useT();
  const { instance } = useInstanceScope();
  // The settings and volume routes are not forwarded to peers.
  const local = instance === '';
  return (
    <div
      role="region"
      aria-label={t('shell.bar')}
      className={`glim-card mb-6 flex-wrap items-center gap-x-6 gap-y-4 p-5 sm:mx-6 md:mx-8 md:mb-8
        ${visible ? 'flex' : 'hidden'}`}
    >
      {/* Named only when it is not this machine, so it stands out when it
          matters. */}
      {instance && (
        <span className="flex items-center gap-2">
          <span className="glim-eyebrow">{t('shell.scope')}</span>
          {/* dir="auto": the peer's name may be in a right-to-left script. */}
          <span className="text-xs font-medium text-carbon-text" dir="auto">
            {instance}
          </span>
          <InfoBubble tip={t('shell.scopeHint')} />
        </span>
      )}

      <div className="flex flex-col gap-2">
        {/* The squares wrap rather than run out of a very narrow card. */}
        <div className="flex flex-wrap items-center gap-2">
          <QueueBar />
          {local && <QuickSettings />}
        </div>
        {local && <VolumeMeter />}
      </div>

      {/* The curve takes the rest of the row, up to the card's end edge. */}
      <ShellSpeed instance={instance} />
    </div>
  );
}

// Once per app load on the desktop build: if update checks are on, toast a
// newer release, so it reaches people who never open Settings. Silent when
// current. With autoUpdateInstall on, it installs directly.
function useAutoUpdateToast() {
  const { toast } = useToast();
  const { t } = useT();
  useEffect(() => {
    let live = true;
    void (async () => {
      const [deployment, settings] = await Promise.all([fetchDeploymentInfo(), fetchSettings()]);
      if (!live || deployment.deployment !== 'desktop' || !settings.autoUpdateCheck) return;
      const check = await fetchUpdateCheck().catch(() => null);
      if (!live || !check?.checked || !check.available || !check.latest) return;
      if (!settings.autoUpdateInstall) {
        toast(t('settings.look.updatesAvailable', { version: check.latest }), 'info');
        return;
      }
      toast(t('settings.look.updatesAutoInstalling', { version: check.latest }), 'info');
      try {
        await installUpdate();
      } catch {
        if (live) toast(t('settings.look.updatesInstallFailed', { error: t('settings.look.updatesFailed') }), 'fail');
      }
    })();
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per mount
  }, []);
}

export function Layout() {
  const location = useLocation();
  useCompletionToasts();
  // Here rather than per page, so unpacking is watched on every page.
  useExtractionToasts();
  useAutoUpdateToast();
  useAppearance();
  // Keyed on the section, not the path: the key replays the enter animation
  // by remounting, which would throw away a section's state on every click
  // between its own sub-pages, such as the settings tabs.
  const section = location.pathname.split('/')[1] ?? '';
  // Settings owns its frame: its tab rail is a full-height column flush with
  // the sidebar, which the padded, page-scrolling wrapper below cannot give.
  // pages/Settings.tsx scrolls its own content column.
  const ownsFrame = section === 'settings';
  return (
    // Wraps the bar and the outlet, so both agree on the instance.
    <InstanceProvider>
      {/* The frame holds the spacing (GlimStone 1.8.0: the rail is a card, 1rem
          all round), so the gap between rail and content and the gap around
          both are one number. The content column therefore only has
          horizontal padding. On a phone the rail is a bar under the page, and
          the dynamic height keeps it above the browser's own toolbar. */}
      <div className="flex h-dvh flex-col gap-4 overflow-hidden bg-carbon-background p-4 md:flex-row">
        <Sidebar />
        <main className={`flex-1 min-w-0 ${ownsFrame ? 'overflow-hidden' : 'overflow-y-auto'}`}>
          <ShellBar visible={section === 'downloads'} />
          {/* flex flex-col min-h-full lets a page fill the height with
              flex-grow (Collector.tsx), which a percentage height would not do
              reliably through an auto-height parent.

              No vertical padding, so the first and last cards line up with
              the sidebar; the frame's 1rem keeps them off the window edge.
              glim-column-top gives the room a section badge's notch needs to
              the first card that has one (index.css). */}
          <div
            key={section}
            className={
              ownsFrame
                ? 'glim-page-enter flex h-full w-full min-h-0 flex-col'
                : 'glim-page-enter glim-column-top flex w-full min-h-full flex-col sm:px-6 md:px-8'
            }
          >
            <Outlet />
          </div>
        </main>
      </div>
      {/* Mounted once, outside the keyed page div, so navigation does not
          remount them: the captcha dialog, the paste and drop listeners, the
          activity strip, the end-of-queue banner, the first-run tour, the
          command palette and the keyboard dispatcher. */}
      <CaptchaModal />
      <GlobalIntake />
      <StatusStrip />
      <IdleActionBanner />
      <OnboardingWizard />
      <CommandPalette />
      {/* Handles every command shortcut except the palette's own mod+k, which
          CommandPalette listens for itself. */}
      <CommandDispatcher />
    </InstanceProvider>
  );
}
