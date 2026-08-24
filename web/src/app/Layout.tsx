import { useEffect, useRef } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { Sidebar } from '../components/Sidebar';
import { QueueBar } from '../components/QueueBar';
import { ShellStrip } from '../components/QuickSettings';
import { AccountStrip } from '../components/AccountStrip';
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
  applyMotionIntensity,
  applyRainbow,
  applyShape,
  cacheAppearance,
  rainbowFromSettings,
  readCachedMotionIntensity,
} from '../lib/appearance';
import { InstanceProvider, useInstanceScope } from '../lib/instance';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';

// Global completion toasts: watch the local task stream and notify when a
// download finishes or fails (transition only, so the initial snapshot is quiet).
function useCompletionToasts() {
  const { toast } = useToast();
  const { t } = useT();
  const prev = useRef<Record<string, string>>({});
  useEffect(() => {
    // 'snapshot' is not in kinds below and still arrives every time - see
    // lib/useTasks.ts's identical note on why (Hub.SendTo bypasses a
    // connection's own subscription filter).
    return connectWS(
      (type, data) => {
        if (type === 'snapshot') {
          prev.current = Object.fromEntries((data ?? []).map((t: Task) => [t.id, t.status]));
        } else if (type === 'task') {
          const before = prev.current[data.id];
          prev.current[data.id] = data.status;
          if (before && before !== data.status) {
            const name = data.name || t('nav.downloads');
            if (data.status === 'done') toast(t('downloads.finished', { name }), 'ok', 'download-done');
            else if (data.status === 'error') toast(t('downloads.failed', { name }), 'fail', 'download-failed');
          }
        } else if (type === 'removed') {
          delete prev.current[data.id];
        }
      },
      ['task', 'removed'],
    );
  }, [toast, t]);
}

// The shape and accent live in the server settings, so they follow the instance
// rather than the browser. They are fetched once here, at the top of the app:
// applying them only where the settings page is mounted would leave every other
// page on the default look, which is exactly what a "square corners" setting
// must not do.
function useAppearance() {
  useEffect(() => {
    // Motion intensity is a client-only preference (see lib/appearance.ts's
    // own "Motion intensity" section) — it has nothing to fetch, so it is
    // applied synchronously here rather than nested inside fetchSettings()'s
    // callback below. This is what makes the axis live from first paint
    // everywhere Layout mounts, not only once the Look settings row itself
    // mounts.
    applyMotionIntensity(readCachedMotionIntensity());

    let live = true;
    fetchSettings()
      .then((s) => {
        if (!live) return;
        const rainbow = rainbowFromSettings(s);
        applyShape(s.shape);
        applyAccent(s.accent);
        applyRainbow(rainbow);
        // Cached so the next load paints the chosen look immediately instead of
        // flashing the default while this request is in flight.
        cacheAppearance(s.shape, s.accent, rainbow);
      })
      .catch(() => {
        // An unreachable API is not a reason to restyle the app; the cached
        // look from the last successful load stays.
      });
    return () => {
      live = false;
    };
  }, []);
}

/**
 * The strip that outlives navigation.
 *
 * It sits ABOVE the keyed page div, and that is the entire reason it exists as
 * its own thing: the keyed div throws its subtree away on every navigation to
 * replay the enter animation, so a transport control rendered inside a page
 * remounts, refetches and forgets itself every time somebody touches the
 * sidebar. Anything that has to keep running while the page changes goes here.
 *
 * It is chrome, not content: the sidebar's shade continues across the top, so
 * the band reads as part of the frame and the page scrolls under it without a
 * separator line.
 *
 * Visually hidden outside the Downloads page (jdp: "die Statuszeile die ganz
 * oben ist soll nur im Downloadfenster sichtbar sein") — hidden with `hidden`,
 * not left unmounted, for the exact reason this component exists at all:
 * unmounting on every navigation away from Downloads would throw away
 * QueueBar's queue-control state and ShellStrip/AccountStrip's own fetch
 * loops, remounting and refetching them the moment somebody navigates back.
 *
 * A real card, not a bar pinned to the window edge (jdp: "Die Statuszeile
 * soll nicht oben am Fenster kleben sondern eine schöne eigene Card sein") —
 * it scrolls with the page like every other card, sitting where a page's own
 * hero content usually starts, with the identical horizontal inset the page
 * below it uses so the two visually line up as one column.
 */
function ShellBar({ visible }: { visible: boolean }) {
  const { t } = useT();
  const { instance } = useInstanceScope();
  return (
    <div
      role="region"
      aria-label={t('shell.bar')}
      className={`glim-card mx-6 mt-6 flex-wrap items-center gap-x-4 gap-y-2 p-5 md:mx-8 md:mt-8
        ${visible ? 'flex' : 'hidden'}`}
    >
      {/* Named only when it is not this machine. A tag on every screen would be
          furniture nobody reads, and then the one time it matters (the queue
          controls pointing somewhere else) it would not be noticed either. */}
      {instance && (
        <span className="flex items-center gap-2">
          <span className="glim-eyebrow">{t('shell.scope')}</span>
          {/* dir="auto", not "ltr": the name is whatever the operator typed
              when the peer was registered, and it can be written in a
              right-to-left script as easily as in a Latin one. */}
          <span className="text-xs font-medium text-carbon-text" dir="auto">
            {instance}
          </span>
          <InfoBubble tip={t('shell.scopeHint')} />
        </span>
      )}

      <QueueBar />

      <span className="flex-1" />
      {/* WIDGET SLOT. Whatever has to be visible on every page goes after this
          spacer and lands at the trailing edge: the overview strip and the
          speed meter (4C), and what waves 6 and 9 add. Left as a place to
          render into rather than a stub, because a guess at their markup is
          something they would have to delete first. Read the scope above with
          useInstanceScope(): nothing in this bar may assume '/api'. */}
      <ShellStrip />
      {/* Wave 6 (6B): tier/traffic/expiry for every enabled debrid account,
          reading a cached snapshot only - see AccountStrip's own doc comment
          for why it never calls a live per-service check from here. */}
      <AccountStrip />
    </div>
  );
}

// Once per app load (jdp, 2026-08-24: the toggle on the Allgemein tab should
// mean "automatic", not "only while that tab happens to be open"): if the
// build is desktop and the setting is on, check for a newer release and
// toast it - the same GET /api/system/update-check the Allgemein tab's own
// manual button uses, just fired here too so it reaches someone who never
// opens Settings at all. Silent when off, on the container build, or when
// already current - a toast on every launch for "you're up to date" would
// train people to dismiss it without reading.
//
// autoUpdateInstall (jdp, 2026-08-24: "kannst du bei updates auch ein
// toggle machen für updates automatisch installieren?") rides the exact
// same check result: meaningless without autoUpdateCheck also being on (see
// its own doc comment on the settings field), so there is nothing to
// install here unless the block above already found something available.
// Installing calls POST /api/system/update-install directly rather than
// going through Look.tsx's own UpdateCard state - this hook fires whether
// or not that page is even mounted, the identical reasoning that already
// justifies toasting from here instead of only checking when the tab is
// open.
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
  useAutoUpdateToast();
  useAppearance();
  // Keyed on the SECTION, not the whole path.
  //
  // The key is what re-triggers the enter animation, and the way it does that is
  // by remounting everything below it. That is right between top-level pages and
  // wrong inside one: a section with a route per sub-page — settings has
  // thirteen — keeps state above its own outlet, and remounting on every click
  // in its rail throws that state away with nothing said. An edited settings
  // page reverting because somebody looked at another tab of it is the exact
  // failure the sub-page split exists to avoid.
  const section = location.pathname.split('/')[1] ?? '';
  return (
    // The provider wraps the bar AND the outlet, because the whole point is that
    // the two agree on which instance is being looked at.
    <InstanceProvider>
      <div className="flex h-screen overflow-hidden bg-carbon-background">
        <Sidebar />
        <main className="flex-1 overflow-y-auto min-w-0">
          <ShellBar visible={section === 'downloads'} />
          {/* flex flex-col min-h-full: a no-op for every page that just
              renders its own natural content height - a flex container's
              lone child with no flex-grow of its own still sizes to its own
              content, identical to plain block layout. What this adds is a
              way for a page to opt INTO filling the available height via
              flex-grow instead of a percentage `height: 100%`, which does
              not reliably resolve through an auto-height ancestor even with
              min-height set on it (browser-inconsistent CSS territory,
              deliberately avoided rather than relied on). Needed for
              Collector.tsx's own flex-1 (jdp, 2026-08-24: "Das
              hauptlinkfenster soll immer ... bis ganz nach unten im fenster
              gehen") - main's own height is definite here (a flex row's own
              align-items:stretch under h-screen, this file's own outer div),
              so min-h-full itself resolves correctly; flex-grow from there
              down is what makes a child reliably fill it. */}
          <div key={section} className="glim-page-enter flex w-full min-h-full flex-col p-6 md:p-8">
            <Outlet />
          </div>
        </main>
      </div>
      {/* Sibling to the scrollable shell rather than nested inside it, the
          same reason ToastProvider's own overlay (lib/toast.tsx) sits beside
          its children instead of inside them: a fixed-position overlay reads
          from the viewport, and nesting it under an animated, keyed page div
          would remount it - and drop whatever it was showing - on every
          navigation. Captcha state has nothing to do with which page is
          open, so it is mounted exactly once here, reachable from every
          route (build-plan.md section 8's Wave 7 note). */}
      <CaptchaModal />
      {/* Same reasoning, same reason it renders null and sits here rather
          than inside a page: a document-level paste listener and a
          window-level drop target are not "this page's" behaviour, and
          mounting either inside the keyed div above would attach and
          detach them on every navigation (build-plan.md section 8's Wave 8
          note, 8B). */}
      <GlobalIntake />
      {/* Same reasoning again: ambient background activity has nothing to
          do with which page is open, so it is mounted exactly once here
          rather than inside the keyed page div (build-plan.md section 3's
          Wave 9 table, 9A). */}
      <StatusStrip />
      {/* Same reasoning once more: the end-of-queue countdown
          (internal/idleaction, Wave 10's 10B) is server-side state that
          survives navigation and reload on its own - this is only the
          courtesy notice and the Cancel button, and both have nothing to do
          with which page happens to be open when the queue goes idle. */}
      <IdleActionBanner />
      {/* Same reasoning once more: the first-run tour is gated on a single
          flag, not on which route is open, so it belongs beside the other
          overlays rather than inside the keyed page div - see
          OnboardingWizard.tsx's own doc comment. */}
      <OnboardingWizard />
      {/* Same reasoning once more: mod+k (or a command's own "open the
          palette" entry, lib/commands/global.ts) has nothing to do with
          which route is open when it fires, so this is mounted exactly
          once here too - see CommandPalette.tsx's own doc comment. */}
      <CommandPalette />
      {/* The keyboard half of the command registry (lib/commands/types.ts) -
          every OTHER command's defaultShortcut (mod+a, and whatever a later
          wave adds) is matched here; CommandPalette.tsx recognises its own
          mod+k independently (see its own doc comment on why), so the two
          do not depend on mount order. Renders nothing; see
          CommandDispatcher.tsx's own doc comment for why it lives beside
          the other always-mounted, render-nothing pieces above rather than
          inside a page. */}
      <CommandDispatcher />
    </InstanceProvider>
  );
}
