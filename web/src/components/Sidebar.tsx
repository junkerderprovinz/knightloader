import { NavLink } from 'react-router-dom';
import { useEffect, useMemo, useState, type CSSProperties } from 'react';
import logoUrl from '../assets/logo.svg';
import { hueVars, rainbowAt } from '../lib/appearance';
import { useRainbow } from '../lib/useRainbow';
import { setHidden, useHidden } from '../lib/sidebarPrefs';
import { asNavLabelMode, setNavLabels, useNavLabels, type NavLabelMode } from '../lib/navLabels';
import { useT } from '../lib/i18n';
import { fetchAuth, fetchSettings, logout } from '../lib/api';
import { useTasks } from '../lib/useTasks';
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
const navBase =
  'relative flex items-center rounded-[var(--radius-control)] px-3 py-2.5 text-[15px] font-medium transition duration-150 select-none';
const navActive = 'glim-active bg-accent text-accentContrast';
const navInactive = 'text-[var(--sidebar-text)] hover:bg-carbon-hover hover:text-carbon-text';

// In rainbow mode the icon carries the item's own hue, so the rail and the
// glyph agree and the nav reads as a set rather than as one gold item and five
// grey ones. Without the mode the rule below never matches.
const navHued = 'glim-hue glim-hue-icon';

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
function NavLabel({ label, mode }: { label: string; mode: NavLabelMode }) {
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
  return (
    <NavLink
      to={to}
      end={end}
      style={hueVars(rainbowAt(hue)) as CSSProperties}
      // The accessible name comes from the visible text in three of the four
      // modes. Glyph-only has no visible text at all, so it says the name
      // itself rather than announcing as an unlabelled link.
      title={centred ? label : undefined}
      aria-label={mode === 'glyph' ? label : undefined}
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
  return (
    <aside className={`flex flex-col shrink-0 h-full bg-carbon-sidebar ${narrow ? 'w-16' : 'w-56'}`}>
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
      >
        <img src={logoUrl} alt="" aria-hidden className={`w-auto shrink-0 ${narrow ? 'h-10' : 'h-28'}`} />
        {!narrow && <span className="text-carbon-text font-bold text-xl tracking-tight">KnightLoader</span>}
      </NavLink>

      <nav className={`flex flex-col gap-1 flex-1 ${narrow ? 'p-2' : 'p-3'}`}>
        {/* Downloads above the collector: the download list is what this app is
            open for, and the collector is the room links pass through on their
            way into it. JDownloader puts its download tab first for the same
            reason, and somebody arriving from it reaches for the first entry. */}
        <Item to="/" end hue={0} mode={mode} label={t('nav.overview')} icon={<IconDashboard />} />
        <Item to="/downloads" hue={1} mode={mode} label={t('nav.downloads')} icon={<IconDownloads />} badge={active} />
        <Item to="/collector" hue={2} mode={mode} label={t('nav.collector')} icon={<IconCollector />} badge={collected} />
        {!hideInstances && <Item to="/instances" hue={3} mode={mode} label={t('nav.instances')} icon={<IconInstances />} />}
        {!hideAccounts && <Item to="/accounts" hue={4} mode={mode} label={t('nav.accounts')} icon={<IconAccounts />} />}
      </nav>

      {/* Sprache and Hell/Dunkel used to live here too, mirrored from the
          Aussehen settings tab - one control, one home now, not two copies
          of the same switch (jdp: "Sprach und hell dunkel ist in der
          sidebar immer noch vorhanden"). Both still live on the Aussehen
          tab (pages/settings/Look.tsx). */}
      <div className={`flex flex-col gap-1 ${narrow ? 'p-2' : 'p-3'}`}>
        <Item to="/settings" hue={5} mode={mode} label={t('nav.settings')} icon={<IconSettings />} />
        {locked && (
          <button
            title={mode === 'glyph' || mode === 'hover' ? t('auth.signOut') : undefined}
            aria-label={mode === 'glyph' ? t('auth.signOut') : undefined}
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
            className={`${navBase} ${navInactive} group w-full text-start ${mode === 'glyph' || mode === 'hover' ? 'justify-center' : 'gap-3'}`}
          >
            {mode !== 'text' && <IconSignOut />}
            <NavLabel label={t('auth.signOut')} mode={mode} />
          </button>
        )}
      </div>
    </aside>
  );
}
