import { createContext, useContext, type ReactNode } from 'react';
import type { Settings } from '../../lib/api';
import type { FeatureState } from './features';

/**
 * SettingsDraft is the draft every sub-page edits. The shell holds it above the
 * router outlet, so moving through the rail keeps unsaved edits.
 */
export interface SettingsDraft {
  /**
   * The whole settings document as the server sent it. Settings types only part
   * of it and PUT replaces the document wholesale, so every edit spreads the
   * object instead of rebuilding it.
   */
  cfg: Settings;
  /** Merge one or more fields into the draft. */
  patch: (fields: Partial<Settings>) => void;
  /** Replace the whole draft, for the advanced table which edits by key path. */
  replace: (next: Settings) => void;
  dirty: boolean;
  /**
   * Patches and saves the named fields at once, for pages where every change is
   * its own live preview. Only those fields are sent and folded back, so an
   * unsaved edit on another page survives.
   */
  patchNow: (fields: Partial<Settings>) => Promise<void>;
  /**
   * Folds an already-applied document into both `saved` and `draft` for the
   * named keys without sending anything. The settings import writes through its
   * own route, and without this the next autosave would put the old values back.
   */
  reseed: (applied: Settings, keys: string[]) => void;
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
 * useDraft throws outside the shell, because a page without the draft would
 * save empty defaults over the user's configuration.
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
