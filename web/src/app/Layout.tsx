import { useEffect, useRef } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { Sidebar } from '../components/Sidebar';
import { QueueBar } from '../components/QueueBar';
import { ShellStrip } from '../components/QuickSettings';
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
  applyMotion,
  applyRainbow,
  applyShape,
  cacheAppearance,
  rainbowFromSettings,
  readCachedMotionIntensity,
} from '../lib/appearance';
import { InstanceProvider, useInstanceScope } from '../lib/instance';
import { useToast } from '../lib/toast';
import { useExtractionToasts } from '../lib/useExtractionToasts';
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
            // The fifth argument is the row this notification is ABOUT, and
            // these two are the only call sites in the app that hold a task id
            // at the moment they toast. It is what puts a "show me the row"
            // press on the matching line in the event log (lib/eventLog.ts);
            // every other logged event has no row to point at and simply
            // carries no jump, which is the honest state rather than a gap to
            // paper over.
            //
            // Always a LOCAL id: this watcher subscribes with no base, so the
            // stream is this machine's own however the page is scoped. See
            // EventBell's jump, which navigates without the ?instance= for
            // exactly that reason.
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
    applyMotion(readCachedMotionIntensity());

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
 * QueueBar's queue-control state and ShellStrip's own fetch loops, remounting
 * and refetching them the moment somebody navigates back.
 *
 * A real card, not a bar pinned to the window edge (jdp: "Die Statuszeile
 * soll nicht oben am Fenster kleben sondern eine schöne eigene Card sein") —
 * it scrolls with the page like every other card, sitting where a page's own
 * hero content usually starts, with the identical horizontal inset the page
 * below it uses so the two visually line up as one column.
 *
 * It carries a BOTTOM margin and no top one, and that is not a style choice:
 * on Downloads this is the topmost card of the column, and the topmost card is
 * meant to start where the sidebar starts (jdp: "in allen tabs und fenstern
 * sollen die obersten und untersten cards/fenster mit der sidebar
 * abschliessen"). The column's own top inset is down to the 12px the section
 * badge needs (see its className below), so an mt here would put this card
 * back where the complaint found it. What the old mt-6 md:mt-8 was really
 * doing - holding this card apart from the page's first one - is the mb now.
 * Under `hidden` the margin goes with the element, so every other page still
 * starts at the top of its own column.
 *
 * WHAT DECIDES HOW TALL THIS CARD IS, because for a while nothing did and it
 * got read as a second page (jdp, 2026-09-13: "die kopfcard im downloadtab ist
 * immer noch viel zu hoch!!").
 *
 * The transport column decides, and only it: three key buttons at 2.5rem with
 * 8px between them is 136px, and jdp asked for exactly that shape and that
 * consequence (2026-09-07, "die drei button untereinander" and "die karte soll
 * so hoch werden das die drei untereinander platz haben"). With p-5 around it
 * the card is 176px, and that is the floor - the way to a flatter card from
 * here is the three buttons, not the padding.
 *
 * What used to decide it was the WIDTH. The speed curve is an svg with a
 * viewBox and no height, so its 148:40 ratio turned every pixel of width into a
 * quarter of a pixel of height, and the row grew to fit it: measured on jdp's
 * own instance at 196px in a 1280 window, 229 at 1400 and 369 at 1920, with the
 * transport column stuck at 136 the whole time. Widening the curve (2026-09-07,
 * "der downloadgraph in der kopfzeile soll viel breiter sein") is what made the
 * card taller, which is why a round that went looking for a wrapped label found
 * nothing to fix. SpeedGraph.tsx's SpeedMeter carries the fix and
 * web/check-stretched-svg-height.mjs is the gate for it.
 *
 * So anything mounted in here has a budget: 136px, or it becomes the tallest
 * thing in the row and the card grows to it. The widget column is inside that
 * today (menu button 32, limit 24, volume meter 25, disk line 20, gaps 24) and
 * it is the piece to watch, because it is the one that wraps.
 */
function ShellBar({ visible }: { visible: boolean }) {
  const { t } = useT();
  const { instance } = useInstanceScope();
  return (
    <div
      role="region"
      aria-label={t('shell.bar')}
      /* items-stretch, not items-center: the speed curve at the trailing edge
         is meant to be as tall as the card itself (jdp, 2026-09-06), and a
         centred row would give it only the height of its own content. It is
         also what carries the card's height down to the curve now that nothing
         on the way claims a height of its own - see ShellStrip. */
      className={`glim-card mx-6 mb-6 items-stretch gap-x-4 gap-y-2 p-5 md:mx-8 md:mb-8
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

      {/* WIDGET SLOT. Whatever has to be visible on every page goes here and
          takes the rest of the row: the speed curve (4C), the limit and the
          settings hamburger, and what waves 6 and 9 add. Left as a place to
          render into rather than a stub, because a guess at their markup is
          something they would have to delete first. Read the scope above with
          useInstanceScope(): nothing in this bar may assume '/api'.

          No flex-1 spacer before it any more: the spacer used to push this to
          the trailing edge and, in doing so, took every spare pixel the curve
          could have had (jdp, 2026-09-07: "der downloadgraph in der kopfzeile
          soll viel breiter sein"). The slot itself grows instead, so the width
          lands where it is wanted. */}
      <ShellStrip />
      {/* No account chip here any more (jdp, 2026-09-06: "Der Hinweis zum
          premium accoount soll da ganz weg"). Tier, traffic and expiry live on
          the Accounts page, which is where somebody goes to act on them; in the
          head card they were a second copy nobody had asked a question of. */}
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
  // Beside the download watcher above and for the identical reason: unpacking
  // finishes whether or not the Downloads page happens to be open, and the
  // per-page useExtractJobs stops watching the moment somebody navigates away.
  // See lib/useExtractionToasts.ts.
  useExtractionToasts();
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
  // Settings owns its own frame, and is the only page that does.
  //
  // Its tab rail is a full-height column flush against the sidebar (jdp,
  // 2026-08-27: "Alle Einstellungstabs werden rechts von der Sidebar vertikal
  // in kacheln angezeigt... von ganz oben bis ganz nach unten"), and neither
  // half of that is reachable from inside the padded, page-scrolling wrapper
  // below: the padding is what stops it being flush, and a wrapper that grows
  // with its content is what stops `h-full` meaning the window. So this one
  // section gets the frame without them and scrolls its own content column
  // instead - see pages/Settings.tsx, which is where that column lives.
  const ownsFrame = section === 'settings';
  return (
    // The provider wraps the bar AND the outlet, because the whole point is that
    // the two agree on which instance is being looked at.
    <InstanceProvider>
      {/* DER ABSTAND GEHOERT DEM RAHMEN, NICHT DER SCHIENE, und die Zahl ist die
          des Hauses. GlimStone 1.8.0: "The rail is a CARD, not a wall" - sie
          traegt denselben Radius und denselben Grundabstand wie jede andere
          Karte, der Grund zeigt sich also auf allen vier Seiten und sie klebt
          nicht am Fensterrand. Die Sprache nennt 1rem ausdruecklich und
          begruendet warum: die Regel ist einmal nur mit "eine Zahl"
          ausgeliefert worden, zwei Anwendungen haben sich je eine eigene
          ausgesucht, und nebeneinander war genau das der ganze Eindruck.
          Ein pro Anwendung gewaehlter Abstand ist kein Abstand, sondern ein
          Zufall.
          Der Abstand steht HIER statt als Rand an der Schiene, damit die Luecke
          zwischen Schiene und Inhalt und die Luecke um beide herum eine einzige
          Zahl sind.
          Und weil der Rahmen den Abstand nach OBEN und UNTEN schon gibt, hat die
          Inhaltsspalte darunter fast keinen eigenen mehr: px-6 pt-3 md:px-8.
          Waagerecht ist das Polster echte Arbeit (es haelt die Karte von der
          Schiene daneben weg), senkrecht war es der zweite Abstand ueber dem
          ersten, und genau der hat die oberste Karte tiefer anfangen lassen als
          die Seitenleiste. Die 1rem des Rahmens sind unten die Luft am Ende des
          Scrollens: naeher als die Seitenleiste kommt keine Karte an den
          Fensterrand, weil die Spalte im Rahmen steckt und nicht daneben. Oben
          steht gar kein Polster mehr; die Kerbe des Abschnittsbadges holt sich
          ihre 12px an der Karte, die eine hat - die Begruendung steht an der
          Spalte selbst und in index.css. */}
      <div className="flex h-screen gap-4 overflow-hidden bg-carbon-background p-4">
        <Sidebar />
        <main className={`flex-1 min-w-0 ${ownsFrame ? 'overflow-hidden' : 'overflow-y-auto'}`}>
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
              down is what makes a child reliably fill it.

              px-6 md:px-8, and NO vertical inset at all. It used to be
              p-6 md:p-8 on top of the frame's own p-4, and that second inset is
              what made the first card start 24-32px lower than the sidebar
              beside it (jdp: "in allen tabs und fenstern sollen die obersten
              und untersten cards/fenster mit der sidebar abschliessen").

              The bottom goes to zero outright. Note where this padding sits: on
              the SCROLLED element, not on `main`, so its bottom half was
              genuine air at the end of a long list rather than a fixed frame -
              that is the thing being given up, and what replaces it is the
              frame's own 1rem, which no card can ever scroll into because the
              column is INSIDE the frame. The foot of a list therefore stops
              exactly where the sidebar stops and still never touches the window
              edge; a column too short to scroll ends where its content ends, as
              it always did.

              The top goes to zero too, and the thing that used to stand in the
              way is handled where it belongs. SectionTitle's badge is a notch
              drawn half OUTSIDE its card's top edge (top-0 plus
              -translate-y-1/2, ui.tsx), so a 22px badge reaches 11px above the
              card; `main` is the scroll container and clips at the frame's
              inner edge, which sliced that half off - measured live at
              1400x900: card top 16, badge 5 to 27, column clipping at 16. A
              12px inset on the COLUMN fixes that card and pushes down every
              page whose first card has no badge at all (Dashboard's hero,
              Collector's toolbar, the instance tiles), which is the same
              defect jdp reported, one notch smaller. So `glim-column-top`
              hands those 12px to the first card WITH a badge and to nothing
              else (index.css) - a page with no notch to protect starts on the
              sidebar's line. The bar above is outside this column and keeps
              the top line exactly. */}
          <div
            key={section}
            className={
              ownsFrame
                ? 'glim-page-enter flex h-full w-full min-h-0 flex-col'
                : 'glim-page-enter glim-column-top flex w-full min-h-full flex-col px-6 md:px-8'
            }
          >
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
