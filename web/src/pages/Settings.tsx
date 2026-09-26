import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Navigate, Route, Routes, useLocation, useMatch, useNavigate, useParams } from 'react-router-dom';
import { type Settings, connectWS, fetchSettings, patchSettings } from '../lib/api';
import { useResource } from '../lib/useResource';
import { readUIState, useUIState } from '../lib/uistate';
import { useNavLabels } from '../lib/navLabels';
import { useTabletLayout } from '../lib/phoneLayout';
import { useT } from '../lib/i18n';
import { withBase } from '../lib/basePath';
import { useStagger, useTabSlide } from '../lib/motion';
import { useToast } from '../lib/toast';
import { ErrorCard, LoadingCard, PageHeader } from '../components/ui';
import { Tabs } from '../components/Tabs';
import { SettingsProvider, type FeatureAccess, type SettingsDraft } from './settings/context';
import { fetchFeatures, setFeature, type FeaturePage, type FeatureState } from './settings/features';
import {
  afterRound,
  foldAnswer,
  heldAfter,
  noAnswer,
  pendingFields,
  same,
  saveRound,
  settle,
  shownAt,
  toSend,
  topKey,
  typedIn,
  type Answer,
} from './settings/paths';
import { FALLBACK_PAGE, hasContent, pageIcon, pageId, renderSettingsPage } from './settings/registry';
import { SettingsSearch } from './settings/SettingsSearch';
import { label, refusalText, useTx } from './settings/tx';

/** Absolute, so resolution inside a splat route never matters. */
const pagePath = (id: string) => `/settings/${id}`;

type Doc = Record<string, unknown>;

/**
 * focusedText is what the text box that has focus shows, or null when focus is
 * not in one. A save that comes back tells by it which field someone is still
 * typing in.
 */
function focusedText(): string | null {
  const el = document.activeElement;
  return el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement ? el.value : null;
}

/**
 * SettingsPage is the settings shell: the rail, the draft and the autosave.
 * The controls live in pages/settings/, one file per sub-page. The draft lives
 * here so it survives moving through the rail, and a remembered page that no
 * longer exists falls back only once the page list has arrived.
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

  // The edited copy. Seeded once here; every later change to `saved` sets the
  // draft itself, so a fold of a few keys leaves unsaved edits elsewhere alone.
  const [draft, setDraft] = useState<Settings | null>(null);
  useEffect(() => {
    if (saved) setDraft((d) => d ?? saved);
  }, [saved]);

  const [saving, setSaving] = useState(false);
  const { toast } = useToast();

  // What the server did not take of the draft. A refused value shows beside
  // its field and is not sent again until the field holds something else.
  const [answer, setAnswer] = useState<Answer>(noAnswer);
  // A value the server tidied or masked while its text box still had focus, by
  // settings key, holding what was sent. The box keeps the typed text until it
  // is left.
  const held = useRef<Doc>({});

  // Something the autosave has yet to send.
  const pending = useMemo(
    () =>
      draft !== null &&
      saved !== null &&
      Object.keys(toSend(draft as unknown as Doc, saved as unknown as Doc, held.current, answer)).length > 0,
    [draft, saved, answer],
  );
  // The draft holds something the server does not, sent or not. A tidied
  // value the server took counts as stored.
  const dirty = useMemo(
    () =>
      draft !== null &&
      saved !== null &&
      Object.keys(pendingFields(draft as unknown as Doc, saved as unknown as Doc, held.current)).length > 0,
    [draft, saved],
  );

  useEffect(() => {
    if (draft) setAnswer((a) => settle(a, draft as unknown as Doc));
  }, [draft]);

  // The places a mounted control shows refusals at, so a refusal nobody shows
  // is raised as a toast rather than lost.
  const places = useRef<{ field: string; below: boolean }[]>([]);
  const showRefusalsAt = useCallback((field: string, below: boolean) => {
    const place = { field, below };
    places.current.push(place);
    return () => {
      places.current = places.current.filter((p) => p !== place);
    };
  }, []);

  const patch = useCallback((fields: Partial<Settings>) => {
    // Spread, never rebuild: the server sends more fields than Settings names
    // and PUT replaces the document wholesale.
    setDraft((d) => (d ? { ...d, ...fields } : d));
  }, []);

  const replace = useCallback((next: Settings) => setDraft(next), []);

  const patchNow = useCallback(async (fields: Partial<Settings>) => {
    setDraft((d) => (d ? { ...d, ...fields } : d));
    const applied = await patchSettings(fields);
    const keys = Object.keys(fields) as Array<keyof Settings>;
    // Fold back only the fields this call sent, so an unsaved edit on another
    // page survives.
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

  /**
   * reseed folds what the server now holds into `draft` and `saved` for the
   * named keys, for callers that wrote through another route. Without it the
   * next autosave after a settings import would send the old values back. The
   * keys come from the server, since it decides what it took over.
   */
  const reseed = useCallback((applied: Settings, keys: string[]) => {
    const appliedDoc = applied as unknown as Record<string, unknown>;
    const fold = (doc: Settings | null): Settings | null => {
      if (!doc) return doc;
      const merged = { ...(doc as unknown as Record<string, unknown>) };
      for (const k of keys) merged[k] = appliedDoc[k];
      return merged as unknown as Settings;
    };
    setSaved(fold);
    setDraft(fold);
  }, [setSaved]);

  // What another tab, the Modules page or a module's own switch saved reaches
  // this one live. A field this tab is still editing keeps the edit, which the
  // autosave then sends.
  const latest = useRef({ saved, draft });
  latest.current = { saved, draft };
  useEffect(
    () =>
      connectWS(
        (type) => {
          // Every socket also gets the task snapshot when it opens.
          if (type !== 'settings') return;
          reloadFeatures();
          void fetchSettings()
            .then((fresh) => {
              const { saved: s, draft: d } = latest.current;
              if (!s || !d) return;
              const savedDoc = s as unknown as Record<string, unknown>;
              const draftDoc = d as unknown as Record<string, unknown>;
              const next = { ...(fresh as unknown as Record<string, unknown>) };
              for (const k of Object.keys(draftDoc)) {
                if (!same(draftDoc[k], savedDoc[k])) next[k] = draftDoc[k];
              }
              setSaved(fresh);
              setDraft(next as unknown as Settings);
            })
            .catch(() => {});
        },
        ['settings'],
      ),
    [reloadFeatures, setSaved],
  );

  // Leaving a box shows the value the server stored for it.
  useEffect(() => {
    const onFocusOut = () => {
      const waiting = held.current;
      if (Object.keys(waiting).length === 0) return;
      held.current = {};
      const stored = latest.current.saved as unknown as Doc | null;
      if (!stored) return;
      setDraft((d) => {
        if (!d) return d;
        const next = { ...(d as unknown as Doc) };
        for (const [k, sent] of Object.entries(waiting)) {
          if (same(next[k], sent)) next[k] = stored[k];
        }
        return next as unknown as Settings;
      });
    };
    document.addEventListener('focusout', onFocusOut);
    return () => document.removeEventListener('focusout', onFocusOut);
  }, []);

  /** onSave sends what is pending and returns the answer as it stands after the save. */
  async function onSave(): Promise<Answer> {
    if (!draft || !saved || saving) return answer;
    // A PATCH of only the top-level fields that differ from `saved`, walking
    // the runtime object, so a field another tab saved in the meantime is not
    // put back (patchSettings in lib/api.ts).
    const savedDoc = saved as unknown as Doc;
    const fields = toSend(draft as unknown as Doc, savedDoc, held.current, answer);
    if (Object.keys(fields).length === 0) return answer;
    setSaving(true);
    try {
      const round = await saveRound(fields, async (p) => (await patchSettings(p as Partial<Settings>)) as unknown as Doc);
      setAnswer((a) => afterRound(a, round));
      // Each refusal is shown once: beside its field where a control shows
      // refusals, and as a toast where none does.
      for (const r of Object.values(round.refused)) {
        if (!places.current.some((p) => shownAt(r.field, p.field, p.below))) toast(refusalText(tx, r.error), 'fail');
      }
      if (round.failed) toast(refusalText(tx, round.failed.error), 'fail');
      const { applied: reply, sent } = round;
      if (reply) {
        // Replacing the text in a box that still has focus would move the caret
        // and could drop the space someone is about to type a word after. A
        // masked secret would even empty the box, and the next keystrokes
        // would be stored as the whole secret.
        const text = focusedText();
        const keep = text === null ? [] : Object.keys(sent).filter((k) => typedIn(sent[k], reply[k], text));
        held.current = heldAfter(held.current, sent, keep);
        setSaved(reply as unknown as Settings);
        setDraft((d) => (d ? (foldAnswer(d as unknown as Doc, reply, sent, savedDoc, keep) as unknown as Settings) : d));
        // A save can move a module, such as clearing the watch folder.
        reloadFeatures();
        toast(t('settings.saved'), 'ok');
      }
      return afterRound(answer, round);
    } finally {
      setSaving(false);
    }
  }

  // Every settings tab saves itself, debounced, through onSave's diff against
  // `saved`. `pending` starts false, so this is inert until an edit lands. An
  // edit made while a save is out goes once that save is back, and a draft the
  // server refused waits for the next edit.
  const saveTimer = useRef<number | null>(null);
  useEffect(() => {
    if (!pending || saving) return;
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
  }, [draft, saving]);

  const toggle = useCallback(
    async (id: string, enabled: boolean) => {
      // Flush a pending autosave first rather than refusing the switch, so both
      // writes happen in order.
      let now = answer;
      if (pending) {
        if (saveTimer.current !== null) {
          window.clearTimeout(saveTimer.current);
          saveTimer.current = null;
        }
        now = await onSave();
      }
      const next = await setFeature(id, enabled);
      setFeatures(next);
      // A module switch changes settings on the server; the draft must follow,
      // or the next save would undo the switch. A refused, unsent or held
      // value stays in its box, the flush's own refusals included.
      const fresh = await fetchSettings();
      const kept: Doc = { ...now.failed, ...held.current };
      for (const [k, r] of Object.entries(now.refused)) kept[k] = r.value;
      setSaved(fresh);
      setDraft((d) => {
        if (!d) return fresh;
        const doc = d as unknown as Doc;
        const merged = { ...(fresh as unknown as Doc) };
        for (const [k, v] of Object.entries(kept)) if (same(doc[k], v)) merged[k] = doc[k];
        return merged as unknown as Settings;
      });
    },
    [pending, onSave, setFeatures, setSaved, answer],
  );

  /**
   * fieldError is the refusal to show at a field while it holds the refused
   * value: one refused right there, or with below anywhere under it.
   */
  const fieldError = (at: string, below = false): string | undefined => {
    const key = topKey(at);
    const r = answer.refused[key];
    if (!r || !draft || !same((draft as unknown as Doc)[key], r.value) || !shownAt(r.field, at, below)) {
      return undefined;
    }
    return refusalText(tx, r.error);
  };

  if (loading || featuresLoading) return <LoadingCard label={t('common.loading')} />;
  if (failed || featuresFailed || !draft || !features) {
    return (
      <ErrorCard
        message={t('common.loadFailed')}
        retry={() => {
          // Cleared, so the seeding effect takes the reloaded document rather
          // than keeping a draft that would save the old values back.
          setDraft(null);
          reload();
          reloadFeatures();
        }}
        retryLabel={t('common.retry')}
      />
    );
  }

  const featureAccess: FeatureAccess = { features, toggle };
  const settingsDraft: SettingsDraft = {
    cfg: draft,
    saved: saved ?? draft,
    patch,
    replace,
    dirty,
    patchNow,
    reseed,
    fieldError,
    showRefusalsAt,
  };

  return (
    <SettingsProvider draft={settingsDraft} features={featureAccess}>
      {/* For screen readers only; the rail already names the page. */}
      <PageHeader title={t('settings.title')} />
      {/* A column of tiles beside the sidebar, running the window's full
          height. Only the content column scrolls; app/Layout.tsx gives this
          page a definite height for that. It fades in over the loading
          card it replaces. */}
      <div className="glim-content-fade flex min-h-0 flex-1">
        <SettingsRail pages={features.pages} />

        {/* data-settings-content scopes the search's DOM lookups to this
            column (settings/jump.ts). The padding matches app/Layout.tsx, and
            glim-column-top keeps the first card's badge notch (index.css). */}
        <div
          data-settings-content
          className="glim-column-top flex min-w-0 flex-1 flex-col gap-6 overflow-y-auto ps-2 sm:px-6 md:px-8"
        >
          {/* Here rather than in PageHeader, which would push the rail down, and
              outside the Routes so a search and its jump survive a page change. */}
          <SettingsSearch pages={features.pages} />
          <Routes>
            <Route index element={<RememberedPage pages={features.pages} />} />
            <Route path=":page" element={<SubPage pages={features.pages} />} />
            {/* Anything deeper than one segment is not a page we ever made. */}
            <Route path="*" element={<Navigate to={pagePath(FALLBACK_PAGE)} replace />} />
          </Routes>
        </div>
      </div>
    </SettingsProvider>
  );
}

/**
 * orderPages applies a saved custom order, so a removed page drops out. A page
 * the order does not name goes in after its registry predecessor, so a new
 * page shows up where it belongs rather than at the end. A folded page's id
 * stands for the page holding its cards, which takes the first place any of
 * them has. The settings search uses it too.
 */
export function orderPages(pages: FeaturePage[], order: string[]): FeaturePage[] {
  const byId = new Map(pages.map((p) => [p.id, p]));
  const seen = new Set<string>();
  const out: FeaturePage[] = [];
  for (const id of order) {
    const p = byId.get(pageId(id));
    if (!p || seen.has(p.id)) continue;
    seen.add(p.id);
    out.push(p);
  }
  pages.forEach((p, i) => {
    if (seen.has(p.id)) return;
    const before = i === 0 ? -1 : out.findIndex((q) => q.id === pages[i - 1].id);
    out.splice(before + 1, 0, p);
    seen.add(p.id);
  });
  return out;
}

/**
 * SettingsRail draws the section tiles with Tabs, the app's one chooser. Every
 * tile is a real link, so Ctrl-click opens a page beside the current one.
 */
function SettingsRail({ pages }: { pages: FeaturePage[] }) {
  const { tx } = useTx();
  const navigate = useNavigate();
  const here = useMatch('/settings/:page');
  // Live, since the selector that changes it sits in this page's own column
  // (lib/navLabels.ts). Below `lg` the width goes to the page and the rail
  // shows the glyphs alone, with each name in its bubble: beside the full
  // sidebar, a rail with words would leave a card about 220px at 800px.
  const narrow = useTabletLayout();
  const chosen = useNavLabels();
  const display = narrow ? 'glyph' : chosen;
  // The drag order is a per-browser preference, like the remembered page.
  const [order, setOrder] = useUIState<string[]>('settingsTabOrder', []);
  const ordered = orderPages(pages, order);

  return (
    // Glyph-only narrows the rail; hover mode keeps its width so nothing moves
    // under the pointer. shrink-0 keeps wide tables from squeezing it.
    <div
      // No vertical padding, so the rail starts and ends flush with the
      // sidebar; the content column beside it keeps only the badge notch. The
      // start keeps just the 4px that the focus ring and a lifted tile's shadow
      // reach into, since main clips there.
      className={`flex h-full shrink-0 flex-col gap-2 ps-1 pe-2 ${display === 'glyph' ? 'w-13' : 'w-51'}`}
    >
      <Tabs
        className="min-h-0 flex-1"
        orientation="vertical"
        fill
        display={display}
        label={tx('settings.railLabel')}
        active={here?.params.page ?? null}
        onSelect={(id) => navigate(pagePath(id))}
        // Arrow keys move focus without selecting, since every selection pushes
        // a history entry.
        activateOnFocus={false}
        // A long press starts the drag.
        reorderable
        onReorder={setOrder}
        items={ordered.map((p) => ({
          id: p.id,
          label: label(tx, 'settings.nav.', p.id),
          icon: pageIcon(p.id),
          href: withBase(pagePath(p.id)),
          // A page without controls is dimmed, not hidden: it still explains itself.
          dim: !hasContent(p.id),
        }))}
      />
    </div>
  );
}

/**
 * RememberedPage redirects to the page that was open last. It waits for the
 * stored value, since useUIState returns its fallback until then and an early
 * redirect would overwrite the remembered page.
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
  const id = pageId(remembered);
  const known = pages.some((p) => p.id === id);
  return <Navigate to={pagePath(known ? id : FALLBACK_PAGE)} replace />;
}

function SubPage({ pages }: { pages: FeaturePage[] }) {
  const { page = '' } = useParams();
  const { search, hash } = useLocation();
  const [, remember] = useUIState<string>('settingsPage', FALLBACK_PAGE);
  const known = pages.some((p) => p.id === page);
  // A tab change slides the new page in from the side its tile lies on in the
  // rail, in the rail's own order, and the page's cards arrive one after
  // another. A page that is one card staggers what is in it.
  const [order] = useUIState<string[]>('settingsTabOrder', []);
  const slide = useTabSlide(
    page,
    orderPages(pages, order).map((p) => p.id),
  );
  const box = useRef<HTMLDivElement>(null);
  useStagger(() => {
    const el = box.current;
    return el?.childElementCount === 1 ? el.firstElementChild : el;
  });

  useEffect(() => {
    if (known) remember(page);
  }, [page, known, remember]);

  if (!known) {
    // A folded page's address goes to the page holding its cards, query and
    // all, so a bookmark or a link naming it still lands on them.
    const folded = pageId(page);
    if (folded !== page && pages.some((p) => p.id === folded)) {
      return <Navigate to={pagePath(folded) + search + hash} replace />;
    }
    return <Navigate to={pagePath(FALLBACK_PAGE)} replace />;
  }
  return (
    <div key={page} ref={box} className={slide.className} style={slide.style}>
      {renderSettingsPage(page)}
    </div>
  );
}
