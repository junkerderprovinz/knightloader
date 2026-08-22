import { createContext, useContext, type ReactNode } from 'react';
import type { Settings } from '../../lib/api';
import type { FeatureState } from './features';

/**
 * The draft every sub-page edits, held by the shell rather than by the page.
 *
 * Sub-pages are the reason this exists. With one long scroll there was one form
 * and one Save; split across thirteen routes, a draft owned by the page would
 * be discarded the moment somebody clicked the rail — so changing the speed
 * limit and then the accent colour would silently lose the first one. The draft
 * lives above the router outlet, so a rail move is not a form submit and the
 * save bar can say what is still outstanding.
 */
export interface SettingsDraft {
  /**
   * The whole settings document as the server sent it.
   *
   * Typed as Settings, which is a SUBSET of what is really in here — the server
   * also sends packagizer, linkFilter, connections, reconnect and schedule, and
   * PUT /api/settings replaces the document wholesale. So every edit spreads the
   * object rather than rebuilding it: dropping a field on the way through would
   * delete somebody's rule set with no error anywhere.
   */
  cfg: Settings;
  /** Merge one or more fields into the draft. */
  patch: (fields: Partial<Settings>) => void;
  /** Replace the whole draft, for the advanced table which edits by key path. */
  replace: (next: Settings) => void;
  dirty: boolean;
  /**
   * Patches AND saves the named fields immediately, bypassing the shared
   * Save bar - for pages where every change is already its own live preview
   * (Look.tsx: jdp "Wenn einstellungen geändert werden, zb die badge form,
   * dann soll man das nicht speichern müssen"). Only the named fields are
   * sent and only they are folded back into `draft`/`saved` on return, so an
   * unsaved edit sitting on a DIFFERENT page's fields survives untouched.
   */
  patchNow: (fields: Partial<Settings>) => Promise<void>;
}

export interface FeatureAccess {
  features: FeatureState;
  /** Switch a module and take the whole answer; throws with the server's reason. */
  toggle: (id: string, enabled: boolean) => Promise<void>;
}

const DraftCtx = createContext<SettingsDraft | null>(null);
const FeatureCtx = createContext<FeatureAccess | null>(null);

export function SettingsProvider({
  draft,
  features,
  children,
}: {
  draft: SettingsDraft;
  features: FeatureAccess;
  children: ReactNode;
}) {
  return (
    <DraftCtx.Provider value={draft}>
      <FeatureCtx.Provider value={features}>{children}</FeatureCtx.Provider>
    </DraftCtx.Provider>
  );
}

/**
 * useDraft throws rather than returning null when a page is mounted outside the
 * shell. A sub-page rendered without the draft would silently show the defaults
 * of an empty object and save them over the user's configuration.
 */
export function useDraft(): SettingsDraft {
  const v = useContext(DraftCtx);
  if (!v) throw new Error('a settings sub-page was rendered outside the settings shell');
  return v;
}

export function useFeatures(): FeatureAccess {
  const v = useContext(FeatureCtx);
  if (!v) throw new Error('a settings sub-page asked for the module registry outside the settings shell');
  return v;
}
