import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Navigate, Route, Routes, useMatch, useNavigate, useParams } from 'react-router-dom';
import { ApiError, type Settings, fetchHealth, fetchSettings, patchSettings } from '../lib/api';
import { useResource } from '../lib/useResource';
import { readUIState, useUIState } from '../lib/uistate';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { ErrorCard, InfoBubble, LoadingCard, PageHeader } from '../components/ui';
import { Tabs } from '../components/Tabs';
import { SettingsProvider, type FeatureAccess, type SettingsDraft } from './settings/context';
import { fetchFeatures, setFeature, type FeaturePage, type FeatureState } from './settings/features';
import { same } from './settings/paths';
import { FALLBACK_PAGE, hasContent, pageIcon, renderSettingsPage } from './settings/registry';
import { label, useTx } from './settings/tx';

/**
 * Absolute, so that relative resolution inside a splat route is never a
 * question anyone has to answer twice.
 */
const pagePath = (id: string) => `/settings/${id}`;

/**
 * The settings shell: the rail, the draft and the save bar. Every actual
 * control lives in pages/settings/, one file per sub-page.
 *
 * It used to be one long scroll, and waves 3 to 11 each add a section to it —
 * which is the contention that turned app.go into eight files. Sub-pages are the
 * same answer: a wave adds a file and one line in registry.tsx instead of
 * editing a page four other people are also editing.
 *
 * Two things the split has to get right, and both are here rather than in the
 * pages:
 *
 *   - the DRAFT survives a rail move. Owned by a page it would be discarded on
 *     every click, so changing the speed limit and then the accent would lose
 *     the first silently. One draft, one save, thirteen routes.
 *   - a page that is remembered and no longer exists falls back to General
 *     rather than rendering an empty frame, and the fallback happens only after
 *     the page list has arrived — redirecting before then would throw away the
 *     remembered page on every reload.
 */
export function SettingsPage() {
  const { t } = useT();
  const { tx } = useTx();

  const { data: saved, setData: setSaved, loading, failed, reload } = useResource<Settings>(fetchSettings);
  const {
    data: features,
    setData: setFeatures,
    loading: featuresLoading,
    failed: featuresFailed,
    reload: reloadFeatures,
  } = useResource<FeatureState>(fetchFeatures);

  // The edited copy. Seeded from the server's answer and reseeded whenever a
  // save or a module switch produces a new one.
  const [draft, setDraft] = useState<Settings | null>(null);
  useEffect(() => {
    if (saved) setDraft(saved);
  }, [saved]);

  const [saving, setSaving] = useState(false);
  const { toast } = useToast();

  const dirty = useMemo(
    () => draft !== null && saved !== null && JSON.stringify(draft) !== JSON.stringify(saved),
    [draft, saved],
  );

  const patch = useCallback((fields: Partial<Settings>) => {
    // Spread, never rebuild: the server sends more fields than the Settings type
    // names — the rule sets, the connections, the timetable — and PUT replaces
    // the document wholesale. A patch that dropped them would delete somebody's
    // Packagizer with no error anywhere.
    setDraft((d) => (d ? { ...d, ...fields } : d));
  }, []);

  const replace = useCallback((next: Settings) => setDraft(next), []);

  const patchNow = useCallback(async (fields: Partial<Settings>) => {
    setDraft((d) => (d ? { ...d, ...fields } : d));
    const applied = await patchSettings(fields);
    const keys = Object.keys(fields) as Array<keyof Settings>;
    // Fold back only the fields this call actually sent - not the whole
    // document `applied` carries - so an edit still sitting unsaved on a
    // different page (Rules, a resolver's API key, ...) is never quietly
    // replaced by whatever this tab last loaded from the server.
    const appliedDoc = applied as unknown as Record<string, unknown>;
    setSaved((s) => {
      if (!s) return s;
      const next = s as unknown as Record<string, unknown>;
      const merged = { ...next };
      for (const k of keys) merged[k] = appliedDoc[k];
      return merged as unknown as Settings;
    });
    setDraft((d) => {
      if (!d) return d;
      const next = d as unknown as Record<string, unknown>;
      const merged = { ...next };
      for (const k of keys) merged[k] = appliedDoc[k];
      return merged as unknown as Settings;
    });
  }, []);

  async function onSave() {
    if (!draft || !saved || saving) return;
    setSaving(true);
    try {
      // PATCH, not the whole document: only the top-level fields this draft
      // actually changed are sent, computed against `saved` (the copy this
      // draft was seeded from), never the whole thing. `draft`/`saved` carry
      // more real keys than the Settings type names (packagizer, connections,
      // reconnect, ... - see SettingsDraft's own doc comment on `cfg`), so
      // the diff walks the real runtime object rather than TypeScript's
      // narrower view of it. A field a DIFFERENT browser tab saved in the
      // meantime, one this tab never touched, survives instead of being
      // silently put back to whatever stale copy this tab loaded with - see
      // patchSettings' own doc comment (lib/api.ts) and PATCH
      // /api/settings's (routes_settings.go) for the server side of that
      // promise.
      const savedDoc = saved as unknown as Record<string, unknown>;
      const draftDoc = draft as unknown as Record<string, unknown>;
      const changed: Record<string, unknown> = {};
      for (const key of Object.keys(draftDoc)) {
        if (!same(draftDoc[key], savedDoc[key])) changed[key] = draftDoc[key];
      }
      if (Object.keys(changed).length === 0) return;
      const applied = await patchSettings(changed as Partial<Settings>);
      setSaved(applied);
      setDraft(applied);
      // The registry reads live settings, so a save can have moved a module: the
      // watch folder cleared by hand is the folder-watch module going off.
      reloadFeatures();
      toast(t('settings.saved'), 'ok');
    } catch (e) {
      toast(saveErrorText(e), 'fail');
    } finally {
      setSaving(false);
    }
  }

  // Saves the instant anything changes, on EVERY settings tab (jdp: "In
  // allen Einstellungstabs soll alles was man einstellt automatisch sofort
  // gespeichert werden, ohne dass ein Speichern Button erscheint und man den
  // anklicken muss") - reuses onSave()'s own diff-against-`saved` logic
  // verbatim, just triggered by a debounced watch on the draft instead of a
  // manual click. `dirty` starts false (draft seeded from saved), so this
  // is inert until an actual edit lands; no separate skip-first-mount guard
  // needed the way Look.tsx's own field-watching effect requires one.
  const saveTimer = useRef<number | null>(null);
  useEffect(() => {
    if (!dirty) return;
    if (saveTimer.current !== null) window.clearTimeout(saveTimer.current);
    saveTimer.current = window.setTimeout(() => {
      saveTimer.current = null;
      void onSave();
    }, 600);
    return () => {
      if (saveTimer.current !== null) {
        window.clearTimeout(saveTimer.current);
        saveTimer.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft]);

  /**
   * What the refusal says, in the reader's language when the server said which
   * refusal it was.
   *
   * A code that has no entry here falls through to the server's sentence rather
   * than to the key: an untranslated explanation is worth more than a dotted
   * identifier, and far more than the SyntaxError this used to show, back when
   * the client handed a plain-text 400 straight to JSON.parse.
   */
  function saveErrorText(e: unknown): string {
    if (e instanceof ApiError && e.code) {
      const key = `settings.${e.code}`.replace('settings.reconnect.', 'settings.reconnect.reason.') as never;
      const text = tx(key, (e.params ?? {}) as never);
      if (text !== key) return text;
    }
    return String(e).replace(/^(Error|ApiError):\s*/, '');
  }

  const toggle = useCallback(
    async (id: string, enabled: boolean) => {
      if (dirty) throw new Error(tx('settings.modules.saveFirst'));
      const next = await setFeature(id, enabled);
      setFeatures(next);
      // A module switch writes settings on the server — it clears the watch
      // folder, empties the timetable. Re-reading them is not a refresh for
      // tidiness: the draft still holds the old value, and the next save would
      // put it straight back and quietly undo the switch.
      const fresh = await fetchSettings();
      setSaved(fresh);
      setDraft(fresh);
    },
    [dirty, setFeatures, setSaved, tx],
  );

  if (loading || featuresLoading) return <LoadingCard label={t('common.loading')} />;
  if (failed || featuresFailed || !draft || !features) {
    return (
      <ErrorCard
        message={t('common.loadFailed')}
        retry={() => {
          reload();
          reloadFeatures();
        }}
        retryLabel={t('common.retry')}
      />
    );
  }

  const featureAccess: FeatureAccess = { features, toggle };
  const settingsDraft: SettingsDraft = { cfg: draft, patch, replace, dirty, patchNow };

  return (
    <SettingsProvider draft={settingsDraft} features={featureAccess}>
      {/* The title is rendered for screen readers only — the rail entry beside
          it already says the same word. See PageHeader. */}
      <PageHeader title={t('settings.title')} />
      {/* The sections run across the top, not down the side. BombVault puts
          them there, JDownloader puts them there, and GlimStone is supposed to
          be the same everywhere — a side rail here and a tab strip in the
          sibling app is two answers to one question. It also gives the page its
          full width back, which is what the wide tables on Advanced and Rules
          were always short of. */}
      <div className="mt-4 flex flex-col gap-6">
        <SectionTabs pages={features.pages} />

        {/* No sticky Save/Discard bar any more - every edit on every settings
            tab saves itself automatically (the debounced effect above),
            confirmed by the same toast Look.tsx's own auto-save already
            uses. */}
        <div className="flex min-w-0 flex-1 flex-col gap-6">
          <Routes>
            <Route index element={<RememberedPage pages={features.pages} />} />
            <Route path=":page" element={<SubPage pages={features.pages} />} />
            {/* Anything deeper than one segment is not a page we ever made. */}
            <Route path="*" element={<Navigate to={pagePath(FALLBACK_PAGE)} replace />} />
          </Routes>
        </div>
      </div>
      <VersionFooter />
    </SettingsProvider>
  );
}

// GlimStone version this UI is built against — bump by hand whenever index.css /
// appearance.ts are re-copied from a newer github.com/junkerderprovinz/glimstone release.
const GLIMSTONE_VERSION = '1.0.0';

/**
 * The build/GlimStone version, quiet in a window corner on every settings
 * tab (jdp: "Die versionsnummern sollen in den Einstellungen in jedem Tab
 * quasi im Hintergrund immer ganz unten im Fenster stehen") - moved here
 * from a single card on the System tab, which only showed it on that one
 * page. `fixed`, not `sticky`: it pins to the actual browser window rather
 * than to the settings content's own scroll container, so it neither
 * scrolls away nor competes with the unsaved-changes bar above for the same
 * sticky-bottom slot - the two are visually and positionally independent.
 * `pointer-events-none` keeps it out of the way of anything real underneath.
 */
function VersionFooter() {
  const { t } = useT();
  const [version, setVersion] = useState('');
  useEffect(() => {
    fetchHealth()
      .then((h) => setVersion(h.version))
      .catch(() => {});
  }, []);
  return (
    <div className="pointer-events-none fixed bottom-1.5 right-4 z-0 select-none md:right-6" aria-hidden>
      <span className="glim-num text-[10px] text-carbon-textMuted/50">
        {version && version !== 'dev' ? version : t('nav.workingTitle')}
        {' · GlimStone '}
        {GLIMSTONE_VERSION}
      </span>
    </div>
  );
}

/**
 * The section tabs, across the top.
 *
 * This was a left rail of hand-rolled NavLinks. It is now the app's one
 * horizontal chooser — the same component as the download list's quick filters
 * and the corner picker on Look — so the active section is FILLED exactly the
 * way an active filter and an active segment are, and in rainbow mode each tab
 * takes its own palette position. Nothing here restates how a tab looks; that
 * lives in Tabs.tsx once.
 *
 * Every tab is also a real link. Ctrl-clicking Advanced to read it beside
 * Downloads is a thing people do with something that looks like navigation, and
 * a strip of buttons quietly swallows the gesture.
 */
/**
 * orderPages applies a saved custom order, then appends anything the saved
 * order doesn't mention (a page added since the order was last saved) in the
 * registry's own order — so a new settings page shows up rather than
 * silently vanishing because an old order array doesn't name it yet, and a
 * page removed from the registry since just drops out on its own (the
 * `.filter` below only keeps ids `pages` still has).
 */
function orderPages(pages: FeaturePage[], order: string[]): FeaturePage[] {
  const byId = new Map(pages.map((p) => [p.id, p]));
  const known = order.map((id) => byId.get(id)).filter((p): p is FeaturePage => p !== undefined);
  const seen = new Set(known.map((p) => p.id));
  return [...known, ...pages.filter((p) => !seen.has(p.id))];
}

function SectionTabs({ pages }: { pages: FeaturePage[] }) {
  const { tx } = useTx();
  const navigate = useNavigate();
  const here = useMatch('/settings/:page');
  // Persisted drag order (jdp: "die Tabs in den Einstellungen soll man nach
  // Belieben anordnen können") — same useUIState store as the remembered
  // last-open page below, a per-browser preference rather than a server
  // setting: which order somebody likes their own tabs in has nothing to do
  // with what the instance itself is configured to do.
  const [order, setOrder] = useUIState<string[]>('settingsTabOrder', []);
  const ordered = orderPages(pages, order);

  return (
    <Tabs
      label={tx('settings.railLabel')}
      active={here?.params.page ?? null}
      onSelect={(id) => navigate(pagePath(id))}
      // Arrow keys move focus without selecting. Selecting as they move is what
      // a JTabbedPane does and what Tabs defaults to, but every selection here
      // pushes a history entry — holding Right to reach Advanced would leave
      // twelve of them behind and turn the Back button into a chore.
      activateOnFocus={false}
      reorderable
      onReorder={setOrder}
      // Content-hugging, not equal-width - the actual BombVault test
      // container's own Settings tab strip sizes each tab to its own label
      // (jdp: "Bitte orientiere dich am Bombvault-Testcontainer!!!"), and a
      // prior "gleich breit" pass here was matching GlimStone's docs, not
      // what is actually deployed there.
      items={ordered.map((p) => ({
        id: p.id,
        label: label(tx, 'settings.nav.', p.id),
        icon: pageIcon(p.id),
        href: pagePath(p.id),
        // A page with no controls yet is dimmed, not hidden or disabled: the
        // address works, the page explains itself, and saying so before the
        // click is cheaper than after it.
        dim: !hasContent(p.id),
      }))}
      // The reorder gesture itself has no visible affordance at rest (jdp
      // explicitly did not want a grab cursor advertising it) - this is the
      // one place that says it is possible at all, since "hold and drag"
      // is not discoverable from the tab strip's own idle appearance.
      after={<InfoBubble tip={tx('settings.railReorderHint')} />}
    />
  );
}

/**
 * The index route: go to the page that was open last.
 *
 * It waits for the stored value to arrive before deciding. useUIState hands out
 * its fallback until the document loads, so redirecting on the first render
 * would send everybody to General and then write General back as the remembered
 * page — the setting would erase itself on every reload.
 */
function RememberedPage({ pages }: { pages: FeaturePage[] }) {
  const [remembered] = useUIState<string>('settingsPage', FALLBACK_PAGE);
  const [ready, setReady] = useState(false);
  useEffect(() => {
    let live = true;
    readUIState().then(() => live && setReady(true));
    return () => {
      live = false;
    };
  }, []);

  if (!ready) return null;
  const known = pages.some((p) => p.id === remembered);
  return <Navigate to={pagePath(known ? remembered : FALLBACK_PAGE)} replace />;
}

function SubPage({ pages }: { pages: FeaturePage[] }) {
  const { page = '' } = useParams();
  const [, remember] = useUIState<string>('settingsPage', FALLBACK_PAGE);
  const known = pages.some((p) => p.id === page);

  useEffect(() => {
    if (known) remember(page);
  }, [page, known, remember]);

  if (!known) return <Navigate to={pagePath(FALLBACK_PAGE)} replace />;
  return <>{renderSettingsPage(page)}</>;
}
