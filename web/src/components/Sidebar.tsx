import { NavLink } from 'react-router-dom';
import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import { LOGO_SCOPE_ID, logoInline } from '../lib/logoInline';
import { hueVars, rainbowAt } from '../lib/appearance';
import { useRainbow } from '../lib/useRainbow';
import { setHidden, useHidden } from '../lib/sidebarPrefs';
import { asNavLabelMode, setNavLabels, useNavLabels, type NavLabelMode } from '../lib/navLabels';
import { useTooltip } from './ui';
import { useT } from '../lib/i18n';
import { fetchAuth, fetchSettings, logout } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { EventBell } from './EventBell';
import {
  IconDashboard,
  IconCollector,
  IconDownloads,
  IconInstances,
  IconAccounts,
  IconSettings,
  IconSignOut,
} from '../lib/icons';

// The active item is FILLED with the accent. It used to be marked by a 3px rail
// on a raised surface; jdp wants no vertical lines anywhere, and the fill is
// also the only treatment that survives the square corner setting, where a rail
// reads as a stray border rather than as a mark.
//
// text-[15px], matching BombVault's own nav row exactly (jdp: "Die texte in
// der sidebar sind zu klein. exakt gleiche Schriftgröße wie in BV") - this
// was 14px, one size below BombVault's navBase.
// No `gap` here, deliberately: the display modes that centre the glyph need it
// to be zero, and two competing gap utilities on one element resolve by
// stylesheet order rather than by which one was written last - a coin flip, not
// an override. Each caller adds the gap it wants.
/**
 * useDrawAndStrike is the sidebar mark's easter egg: press and hold and the
 * blade draws out of the rail, let go and it swings.
 *
 * It is the counterpart to BombVault's own, which shatters its logo into 36
 * pieces with a fire cloud. That fits a bomb; a sword's own gesture is drawing
 * and striking, and it keeps the same rhythm - tension while held, discharge on
 * release.
 *
 * A SHORT press is still a click and still navigates home. That is what the
 * suppress flag below is for: the browser fires click after pointerup either
 * way, and an egg that ate the navigation would be a broken logo rather than a
 * surprise.
 *
 * The whole thing is CSS (see .kl-egg in index.css), so it follows the motion
 * setting and disappears under prefers-reduced-motion without knowing that it
 * does. This hook only decides WHICH of the three states is on the element.
 */
function useDrawAndStrike(): {
  state: 'idle' | 'draw' | 'strike';
  onPointerDown: () => void;
  onPointerUp: () => void;
  onPointerCancel: () => void;
  onClick: (e: React.MouseEvent) => void;
} {
  const [state, setState] = useState<'idle' | 'draw' | 'strike'>('idle');
  const hold = useRef<number | undefined>(undefined);
  const done = useRef<number | undefined>(undefined);
  const suppress = useRef(false);

  useEffect(
    () => () => {
      window.clearTimeout(hold.current);
      window.clearTimeout(done.current);
    },
    [],
  );

  /**
   * How long after the release the blade actually lands, in milliseconds.
   *
   * It has to match the impact keyframe in .kl-egg's strike animation, which
   * sits at 16% of a 540ms swing. Before this existed the rail started
   * shuddering the instant the pointer came up, so the shudder arrived BEFORE
   * the blow and read as the rail flinching in anticipation.
   */
  const IMPACT_MS = 86;

  /** Runs the shudder down the rail, one entry after the next, from the impact. */
  function shove() {
    const rail = document.querySelector('[data-nav-rail]');
    if (!rail) return;
    [...rail.children].forEach((el, i) => {
      window.setTimeout(() => {
        el.classList.add('kl-egg-struck');
        window.setTimeout(() => el.classList.remove('kl-egg-struck'), 400);
      }, IMPACT_MS + i * 45);
    });
  }

  return {
    state,
    onPointerDown: () => {
      suppress.current = false;
      window.clearTimeout(hold.current);
      // 320ms: long enough that an ordinary click never reaches it, short
      // enough that somebody who meant to hold does not wonder whether it
      // registered.
      hold.current = window.setTimeout(() => {
        suppress.current = true;
        setState('draw');
      }, 320);
    },
    onPointerUp: () => {
      window.clearTimeout(hold.current);
      setState((cur) => {
        if (cur !== 'draw') return cur;
        window.clearTimeout(done.current);
        done.current = window.setTimeout(() => setState('idle'), 700);
        shove();
        return 'strike';
      });
    },
    onPointerCancel: () => {
      window.clearTimeout(hold.current);
      setState((cur) => (cur === 'draw' ? 'idle' : cur));
    },
    onClick: (e) => {
      // Only the click that ENDED a hold is swallowed; every other one is the
      // ordinary "take me home".
      if (!suppress.current) return;
      suppress.current = false;
      e.preventDefault();
    },
  };
}

// navBase/navInactive/NavLabel are exported rather than copied, and that is not
// tidiness: a rail row that lives in another file (EventBell.tsx, the one row
// here that opens a panel instead of navigating) needs the identical class
// strings, and a duplicated Tailwind string is the drift that stops matching
// the rail the first time somebody edits one of the two copies. The import
// EventBell makes back into this file is a cycle on paper only - it reads these
// during render, long after both modules have finished evaluating.
// glim-nav-row traegt die Glyphengroesse des Hauses, 20px. Sie steht HIER statt
// an jeder Aufrufstelle, weil navBase ohnehin die eine Zeichenkette ist, die
// jede Schienenzeile teilt - auch die in EventBell.tsx, die sie importiert
// statt sie abzuschreiben.
export const navBase =
  'glim-nav-row relative flex items-center rounded-[var(--radius-control)] px-3 py-2.5 text-[15px] font-medium transition duration-150 select-none';
const navActive = 'glim-active bg-accent text-accentContrast';
// A QUIET HOVER MOVES, IT DOES NOT ONLY RECOLOUR: 2px of horizontal nudge,
// motion-safe-gated. On a rail this visually quiet a colour change alone
// barely reads as "this is reachable", and the nudge was simply missing - a
// search for `translate` in index.css finds the bubble, the shake, the page
// entrance, the toast, the panel, the window and the easter egg, and nothing
// at all for the rail. It sits on this string rather than at the three call
// sites because the bell and the sign-out row IMPORT this string instead of
// copying it, so one edit reaches every row in the rail.
export const navInactive =
  'text-[var(--sidebar-text)] hover:bg-carbon-hover hover:text-carbon-text motion-safe:hover:translate-x-0.5';

// In rainbow mode the icon carries the item's own hue, so the rail and the
// glyph agree and the nav reads as a set rather than as one gold item and five
// grey ones. Without the mode the rule below never matches.
// Exportiert wie navBase und navInactive daneben, und aus demselben Grund: die
// Glocke ist eine Zeile dieser Schiene, die in einer anderen Datei liegt, und
// eine abgeschriebene Klassenkette ist die Drift, die beim ersten Bearbeiten
// einer der beiden Kopien anfaengt.
export const navHued = 'glim-hue glim-hue-icon';

/**
 * The label, in whichever of the four states the display mode asks for.
 *
 * `hover` renders it and collapses it to nothing rather than leaving it out,
 * which is the entire mechanism (jdp, 2026-08-27: "bei button in sidebar
 * rutscht der glyph nach links und text erscheint"): the row is centred, so a
 * label of zero width leaves the glyph in the middle, and a label that grows
 * back pushes the glyph left into exactly the position it holds under `both`.
 * Nothing measures anything, and the row's own box never changes size.
 *
 * The gap between glyph and label lives INSIDE this span rather than on the
 * row as a `gap`, and it appears only on hover. Both halves of that are
 * measured rather than chosen: a row `gap` survives its neighbour collapsing
 * to zero, and so does padding - `max-width: 0` on a border-box element still
 * cannot squeeze 12px of padding below 12px, which left the glyph 7px off
 * centre at rest when this was a plain `ps-3`. Growing the padding with the
 * label is the only version where the resting glyph is actually centred.
 *
 * Logical `ps`, not `pl` - the app has right-to-left locales, and in those the
 * glyph slides the other way.
 */
export function NavLabel({ label, mode }: { label: string; mode: NavLabelMode }) {
  if (mode === 'glyph') return null;
  if (mode !== 'hover') return <span className="flex-1">{label}</span>;
  return (
    <span
      className="max-w-0 overflow-hidden whitespace-nowrap opacity-0 transition-all duration-200
        group-hover:ps-3 group-hover:max-w-40 group-hover:opacity-100
        group-focus-visible:ps-3 group-focus-visible:max-w-40 group-focus-visible:opacity-100"
    >
      {label}
    </span>
  );
}

function Item({
  to,
  label,
  icon,
  end,
  badge,
  hue,
  mode,
}: {
  to: string;
  label: string;
  icon: React.ReactNode;
  end?: boolean;
  badge?: number;
  hue: number;
  mode: NavLabelMode;
}) {
  // Centred whenever the label is not permanently present - which covers
  // glyph-only (there is nothing else in the row) and hover (where the
  // centring is what does the work). `group` is what NavLabel's own
  // group-hover rules hang off, and inert in the other three modes.
  const centred = mode === 'glyph' || mode === 'hover';
  // GLYPH-ONLY GETS THE BUBBLE, HOVER MODE DOES NOT - and it is the house
  // bubble either way.
  //
  // Two separate things were wrong in one attribute. It was gated on
  // `centred`, which is true in hover mode as well: there the pointer arrives,
  // the words slide out of the row, and the browser drew its own box saying
  // the same word on top of them. The words themselves are the reveal in that
  // mode and a bubble over them is the same information twice. And it was the
  // NATIVE attribute, which is the other mechanism - the OS's box, in the OS's
  // font, at the pointer instead of at the trigger, and untouched by every
  // rule this app's own bubble follows. Button and IconBadge made this same
  // move already; the rail is one of the call sites on raw elements that was
  // never swept after them.
  const tip = useTooltip<HTMLAnchorElement>(mode === 'glyph' ? label : undefined);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <NavLink
        to={to}
        end={end}
        style={hueVars(rainbowAt(hue)) as CSSProperties}
        // The accessible name comes from the visible text in three of the four
        // modes. Glyph-only has no visible text at all, so it says the name
        // itself rather than announcing as an unlabelled link.
        aria-label={mode === 'glyph' ? label : undefined}
        {...(mode === 'glyph' ? tipHoverProps : {})}
        className={({ isActive }) =>
          `${navHued} ${navBase} group ${centred ? 'justify-center' : 'gap-3'} ${isActive ? navActive : navInactive}`
        }
      >
        {mode !== 'text' && icon}
        <NavLabel label={label} mode={mode} />
        {/* On the filled active item the badge sits on the accent, so it borrows
            the ink colour instead of the surface tint it uses when idle.

            In both centred modes it moves to the corner of the glyph rather
            than disappearing: a queue that is running is worth more than the two
            characters it costs, and it is the one thing in this rail somebody
            watches without reading. In hover mode that is also the only way the
            glyph is genuinely centred at rest - left in the flow, an inline
            badge sitting beside it would push it off centre by its own width,
            on exactly the four items that ever carry one. navBase is already
            `relative`, so this needs no positioning context of its own. */}
        {badge ? (
          <span
            className={`glim-num rounded-[var(--radius-pill)] bg-carbon-surface3/60 px-1.5 py-0.5 text-[11px] font-semibold leading-none text-carbon-textSub [.glim-active_&]:bg-black/15 [.glim-active_&]:text-current
              ${centred ? 'absolute end-1 top-1' : ''}`}
          >
            {badge}
          </span>
        ) : null}
      </NavLink>
      {tip.node}
    </>
  );
}

export function Sidebar() {
  const { t } = useT();
  const tasks = useTasks('');
  // Subscribed, not read: the nav re-renders when the palette or the mode
  // changes, so editing a swatch in Settings is visible in the rail at once.
  useRainbow();

  const [locked, setLocked] = useState(false);

  useEffect(() => {
    // Signing out only makes sense on an instance that can be signed in to.
    fetchAuth()
      .then((a) => setLocked(a.enabled))
      .catch(() => {});
  }, []);

  // Live, not refetched on navigation: this is a persistent layout element
  // that never remounts, and each settings tab pushes into the same store the
  // instant its toggle changes (see sidebarPrefs.ts), so the item
  // appears/disappears while the user is still sitting on that tab, not only
  // on the next navigation (jdp, 2026-08-23: "Kontentab in der sidebar wird
  // nicht live ein und ausgeblendet"). Fetched once here purely to seed the
  // store with whatever the server last saved, for the first paint after a
  // reload / before either tab has ever been visited this session.
  const hideAccounts = useHidden('accounts');
  const hideInstances = useHidden('instances');
  // Seeded in the same breath and for the same reason - one settings read,
  // three things the rail cannot draw itself without. See lib/navLabels.ts.
  const mode = useNavLabels();
  const egg = useDrawAndStrike();
  useEffect(() => {
    fetchSettings()
      .then((s) => {
        setHidden('accounts', s.hideAccountsFromSidebar);
        setHidden('instances', s.hideInstancesFromSidebar);
        setNavLabels(asNavLabelMode(s.navLabels));
      })
      .catch(() => {});
  }, []);

  const { collected, active } = useMemo(() => {
    let collected = 0,
      active = 0;
    for (const t of Object.values(tasks)) {
      // Held links are not in the collector's list, so they must not be in its
      // badge either: a filter that is working would otherwise put a permanent
      // number in the sidebar pointing at links that are not there.
      if (t.status === 'collected') {
        if (!t.skipped) collected++;
      }
      else if (t.status === 'running' || t.status === 'queued' || t.status === 'extracting') active++;
    }
    return { collected, active };
  }, [tasks]);

  // Glyph-only is the one mode that narrows the rail: with no label ever drawn
  // there is nothing to be wide for. Hover mode keeps the full width on
  // purpose - its promise is that nothing moves when the pointer arrives, and
  // a rail that widened to fit the label it reveals would break that on the
  // first mouseover.
  const narrow = mode === 'glyph';

  // THE POSITION COMES FROM A COUNTER, IN RENDER ORDER, NEVER FROM A LITERAL.
  //
  // The six destinations carried hue={0}..hue={5} written out by hand, and two
  // of them are conditional: hide "Instanzen" in the settings and position 3
  // goes unspent, so the rail's rainbow has a hole in the middle while Konten
  // and Einstellungen sit on 4 and 5 regardless. A counter incremented once
  // per destination AS IT ACTUALLY RENDERS has no such gap - a destination
  // that is hidden this render consumes no slot, so hiding or showing one
  // never disturbs the colour of any other. This is the whole of the original
  // objection to a rail rainbow ("a destination list is user-configured, a
  // static index can never hold a stable position") answered: the objection
  // was to the static index, not to the position.
  //
  // Reset on every render because it is declared here rather than kept in a
  // ref: the count has to describe THIS render's tree, not the last one's.
  let hue = 0;
  const nextHue = () => hue++;

  // The sign-out row is a rail row like any other and follows the same rule as
  // Item's own tooltip above: the house bubble, and only in the one mode that
  // shows no words at all. Declared out here because the row itself is
  // conditional on `locked` and a hook cannot be.
  const signOutTip = useTooltip<HTMLButtonElement>(mode === 'glyph' ? t('auth.signOut') : undefined);
  const { role: _signOutRole, tabIndex: _signOutTabIndex, ...signOutTipProps } = signOutTip.triggerProps;
  return (
    // EINE KARTE, KEINE WAND (GlimStone 1.8.0). Der Radius ist der der Karten,
    // der Abstand kommt vom Rahmen in app/Layout.tsx. KEIN Schatten, und das ist
    // ausdruecklich: in einem Haus, dessen Karten keinen haben, tauscht eine
    // erhoehte Schiene neben flachen Karten nur eine Ungereimtheit gegen eine
    // andere.
    // Ihr eigenes Flaechen-Token bleibt, was es war. Was sich aendert, ist die
    // FORM, nicht die Farbe: die Schiene ist Navigation, und dass sie sich vom
    // Inhalt abhebt, den sie navigiert, ist der Sinn des eigenen Tokens.
    // overflow-hidden, weil die Marke oben sonst ueber die runde Ecke hinausragt.
    //
    // THE NARROW WIDTH IS 85px, AND IT BELONGS TO THE HOUSE. It used to be a
    // calculation each app made from its own brand mark plus its row padding,
    // which produced 96px in one app and 64px in another - and this rail was
    // the 64, written as `w-16`. Two rails of different widths doing the same
    // job is the thing a shared language exists to prevent, so the number is
    // now a number: the mark fits the rail, not the rail the mark, and an app
    // whose mark does not fit shrinks the mark.
    <aside
      className={`flex h-full shrink-0 flex-col overflow-hidden rounded-[var(--radius-card)]
        bg-carbon-sidebar ${narrow ? 'w-[85px]' : 'w-56'}`}
    >
      {/* Centered and stacked - jdp's own call for KnightLoader specifically,
          overriding the horizontal BV-matched row this briefly became ("Das
          Logo in der Sidebar wieder größer und Text unter das Logo"): a
          bigger mark reads better centered above its name than squeezed
          into a compact side-by-side row sized for BV's own smaller 64px
          icon.

          The narrow rail drops the wordmark and shrinks the mark to fit. It
          is the one place the rail's own header has to know about the display
          mode: a 112px logo in a 64px column is not a smaller logo, it is a
          cropped one, and the name beside it would have nowhere to go. */}
      <NavLink
        to="/"
        end
        className={`flex flex-col items-center gap-2 hover:opacity-90 transition-opacity ${narrow ? 'px-2 py-4' : 'px-4 py-6'}`}
        onPointerDown={egg.onPointerDown}
        onPointerUp={egg.onPointerUp}
        onPointerLeave={egg.onPointerCancel}
        onPointerCancel={egg.onPointerCancel}
        onClick={egg.onClick}
      >
        {/* The easter egg wraps the mark and nothing else: a short click still
            navigates home, a long press draws the blade. See useDrawAndStrike. */}
        {/* Inline rather than <img>, and only here: CSS cannot reach inside an
            image, and this egg moves the SWORD while the shield stands still.
            The other three places that show the mark keep <img>, which caches
            and decodes off the main thread. See lib/logoInline for the one
            trap inlining brings with it. */}
        <span
          className={`kl-egg shrink-0 ${narrow ? 'h-10' : 'h-28'}`}
          id={LOGO_SCOPE_ID}
          data-egg={egg.state}
          dangerouslySetInnerHTML={{ __html: logoInline }}
        />
        {!narrow && <span className="text-carbon-text font-bold text-xl tracking-tight">KnightLoader</span>}
      </NavLink>

      <nav data-nav-rail className={`flex flex-col gap-1 flex-1 ${narrow ? 'p-2' : 'p-3'}`}>
        {/* Downloads above the collector: the download list is what this app is
            open for, and the collector is the room links pass through on their
            way into it. JDownloader puts its download tab first for the same
            reason, and somebody arriving from it reaches for the first entry. */}
        <Item to="/" end hue={nextHue()} mode={mode} label={t('nav.overview')} icon={<IconDashboard />} />
        <Item to="/downloads" hue={nextHue()} mode={mode} label={t('nav.downloads')} icon={<IconDownloads />} badge={active} />
        <Item to="/collector" hue={nextHue()} mode={mode} label={t('nav.collector')} icon={<IconCollector />} badge={collected} />
        {!hideInstances && <Item to="/instances" hue={nextHue()} mode={mode} label={t('nav.instances')} icon={<IconInstances />} />}
        {!hideAccounts && <Item to="/accounts" hue={nextHue()} mode={mode} label={t('nav.accounts')} icon={<IconAccounts />} />}
      </nav>

      {/* Sprache and Hell/Dunkel used to live here too, mirrored from the
          Aussehen settings tab - one control, one home now, not two copies
          of the same switch (jdp: "Sprach und hell dunkel ist in der
          sidebar immer noch vorhanden"). Both still live on the Aussehen
          tab (pages/settings/Look.tsx). */}
      <div className={`flex flex-col gap-1 ${narrow ? 'p-2' : 'p-3'}`}>
        {/* The bell sits in the bottom cluster rather than in the nav rail
            above, because it is not a place: the five rows above are routes,
            and a sixth that opens a panel over them would read as a page that
            never loads. Above sign-out and settings, which are the two things
            somebody reaches for when they have finished looking rather than
            when they are looking. */}
        <EventBell hue={nextHue()} />
        {/* Sign out sits ABOVE Settings (jdp, 2026-09-07: "in der sidebar soll
            der abmeldebutton über dem einstellungs button sein"). It is also
            the only sign-out left in the app now - the copy on the password
            card is gone - so it wants the position a sign-out is looked for
            in, not the last row under the settings link. */}
        {locked && (
          <>
            <button
              aria-label={mode === 'glyph' ? t('auth.signOut') : undefined}
              {...(mode === 'glyph' ? signOutTipProps : {})}
              onClick={async () => {
                try {
                  await logout();
                  location.reload();
                } catch {
                  // logout() now throws on a non-2xx response too, not only a
                  // network failure (api.ts) - caught here so that stays an
                  // inert click rather than an unhandled rejection; there is
                  // no error banner in the sidebar to show anything richer.
                }
              }}
              // Sign out is not an Item (it navigates nowhere), but it is a row
              // in the same rail and follows the same four modes - one of them
              // leaving a labelled button among centred glyphs would read as
              // something the mode had missed rather than as an exception.
              //
              // `text-start` because a <button> centres its own text and an <a>
              // does not, so this row sat a little further in than the five
              // links above it in every mode that shows a label. Only visible
              // once text-only mode put a label here with no glyph beside it to
              // disguise it, but it was always wrong.
              // navHued wie jede andere Zeile: Abmelden navigiert nirgendwohin,
              // ist aber eine Zeile dieser Schiene, und ohne die Farbposition
              // stand hier ein graues Symbol zwischen farbigen.
              className={`${navHued} ${navBase} ${navInactive} group w-full text-start ${mode === 'glyph' || mode === 'hover' ? 'justify-center' : 'gap-3'}`}
              // nextHue() direkt hier, in der Reihenfolge, in der die Zeile
              // gezeichnet wird. Genau einmal aufgerufen, also genau eine Farbe;
              // und weil der ganze Block an `locked` haengt, verbraucht eine
              // nicht gezeichnete Abmelden-Zeile auch keine Position und
              // verschiebt nichts darunter.
              style={hueVars(rainbowAt(nextHue())) as CSSProperties}
            >
              {mode !== 'text' && <IconSignOut />}
              <NavLabel label={t('auth.signOut')} mode={mode} />
            </button>
            {signOutTip.node}
          </>
        )}
        <Item to="/settings" hue={nextHue()} mode={mode} label={t('nav.settings')} icon={<IconSettings />} />
      </div>
    </aside>
  );
}
