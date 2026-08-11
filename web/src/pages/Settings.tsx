import { useCallback, useEffect, useMemo, useState } from 'react';
import { Navigate, Route, Routes, useMatch, useNavigate, useParams } from 'react-router-dom';
import { ApiError, type Settings, fetchSettings, patchSettings } from '../lib/api';
import { useResource } from '../lib/useResource';
import { readUIState, useUIState } from '../lib/uistate';
import { useT } from '../lib/i18n';
import { Button, ErrorCard, LoadingCard, PageHeader } from '../components/ui';
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

  const [saveError, setSaveError] = useState('');
  const [saving, setSaving] = useState(false);

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

  async function onSave() {
    if (!draft || !saved) return;
    setSaveError('');
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
      const applied = Object.keys(changed).length > 0 ? await patchSettings(changed as Partial<Settings>) : saved;
      setSaved(applied);
      setDraft(applied);
      // The registry reads live settings, so a save can have moved a module: the
      // watch folder cleared by hand is the folder-watch module going off.
      reloadFeatures();
    } catch (e) {
      setSaveError(saveErrorText(e));
    } finally {
      setSaving(false);
    }
  }

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
  const settingsDraft: SettingsDraft = { cfg: draft, patch, replace, dirty };

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

        <div className="flex min-w-0 flex-1 flex-col gap-6">
          <Routes>
            <Route index element={<RememberedPage pages={features.pages} />} />
            <Route path=":page" element={<SubPage pages={features.pages} />} />
            {/* Anything deeper than one segment is not a page we ever made. */}
            <Route path="*" element={<Navigate to={pagePath(FALLBACK_PAGE)} replace />} />
          </Routes>

          {dirty && (
            <div className="glim-card sticky bottom-0 flex items-center gap-3 p-4">
              <span className="text-sm text-carbon-textSub">{tx('settings.unsaved')}</span>
              <span className="flex-1" />
              {saveError && <span className="text-sm text-statusFail">{saveError}</span>}
              <Button kind="ghost" onClick={() => setDraft(saved)} disabled={saving}>
                {tx('settings.discard')}
              </Button>
              <Button onClick={onSave} disabled={saving}>
                {t('settings.save')}
              </Button>
            </div>
          )}
        </div>
      </div>
    </SettingsProvider>
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
function SectionTabs({ pages }: { pages: FeaturePage[] }) {
  const { tx } = useTx();
  const navigate = useNavigate();
  const here = useMatch('/settings/:page');

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
      items={pages.map((p) => ({
        id: p.id,
        label: label(tx, 'settings.nav.', p.id),
        icon: pageIcon(p.id),
        href: pagePath(p.id),
        // A page with no controls yet is dimmed, not hidden or disabled: the
        // address works, the page explains itself, and saying so before the
        // click is cheaper than after it.
        dim: !hasContent(p.id),
      }))}
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
