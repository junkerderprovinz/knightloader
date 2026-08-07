import { useCallback, useEffect, useMemo, useState } from 'react';
import { NavLink, Navigate, Route, Routes, useParams } from 'react-router-dom';
import { type Settings, fetchSettings, saveSettings } from '../lib/api';
import { useResource } from '../lib/useResource';
import { readUIState, useUIState } from '../lib/uistate';
import { useT } from '../lib/i18n';
import { Button, ErrorCard, LoadingCard, PageHeader } from '../components/ui';
import { SettingsProvider, type FeatureAccess, type SettingsDraft } from './settings/context';
import { fetchFeatures, setFeature, type FeaturePage, type FeatureState } from './settings/features';
import { FALLBACK_PAGE, hasContent, renderSettingsPage } from './settings/registry';
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
    if (!draft) return;
    setSaveError('');
    setSaving(true);
    try {
      const applied = await saveSettings(draft);
      setSaved(applied);
      setDraft(applied);
      // The registry reads live settings, so a save can have moved a module: the
      // watch folder cleared by hand is the folder-watch module going off.
      reloadFeatures();
    } catch (e) {
      setSaveError(String(e).replace(/^Error:\s*/, ''));
    } finally {
      setSaving(false);
    }
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
      <div className="mt-4 flex flex-col gap-6 lg:flex-row lg:items-start lg:gap-8">
        <Rail pages={features.pages} />

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
 * The rail. The active entry is FILLED with the accent, the same treatment as
 * the sidebar — no rules, no leading-edge bars: a vertical mark reads as a stray
 * border under the square corner setting, and the fill is the one marking that
 * survives all three.
 */
function Rail({ pages }: { pages: FeaturePage[] }) {
  const { tx } = useTx();
  const base =
    'rounded-[var(--radius-control)] px-3 py-2 text-left text-[13px] font-medium transition duration-150 select-none whitespace-nowrap';
  const active = 'glim-active bg-accent text-accentContrast';
  const inactive = 'text-carbon-textSub hover:bg-carbon-hover hover:text-carbon-text';
  return (
    // A row that scrolls on a phone and a column beside the content above it.
    // The rail scrolls inside itself rather than widening the page, so a long
    // page name never puts the body into a horizontal scroll.
    <nav
      aria-label={tx('settings.railLabel')}
      className="-mx-1 flex shrink-0 gap-1 overflow-x-auto px-1 pb-1 lg:mx-0 lg:w-44 lg:flex-col lg:overflow-visible lg:px-0 lg:pb-0"
    >
      {pages.map((p) => (
        <NavLink
          key={p.id}
          to={pagePath(p.id)}
          // A page with no controls yet is dimmed, not hidden or disabled: the
          // address works, the page explains itself, and saying so before the
          // click is cheaper than after it. Only the inactive state is dimmed —
          // a selected entry has to stay legible on the accent.
          className={({ isActive }) =>
            `${base} ${isActive ? active : `${inactive} ${hasContent(p.id) ? '' : 'opacity-60'}`}`
          }
        >
          {label(tx, 'settings.nav.', p.id)}
        </NavLink>
      ))}
    </nav>
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
