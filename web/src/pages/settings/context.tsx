import { createContext, useContext, useEffect, type ReactNode } from 'react';
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
  /** What the server holds, so a page can tell its own edit from a value that arrived from elsewhere. */
  saved: Settings;
  /** Merge one or more fields into the draft. */
  patch: (fields: Partial<Settings>) => void;
  /** Replace the whole draft, for the advanced table which edits by key path. */
  replace: (next: Settings) => void;
  /**
   * The draft holds something the server does not: an edit still on its way,
   * or one it refused. A button that acts on the stored settings waits for it.
   */
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
  /**
   * Why the server refused what a field holds. The draft keeps the refused
   * value and the autosave leaves it out until it is edited. Read it through
   * useFieldError, which also tells the shell the refusal has a place.
   */
  fieldError: (field: string, below?: boolean) => string | undefined;
  /** Marks a place a mounted control shows refusals at; the result unmarks it. */
  showRefusalsAt: (field: string, below: boolean) => () => void;
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

/**
 * useFieldError is the refusal a control shows beside itself for field, a
 * settings key or a dotted path below one such as "reconnect.checkUrl". With
 * below, a refusal anywhere under field is shown too, for a control that edits
 * a whole list. A refusal no mounted control shows goes to a toast instead.
 */
export function useFieldError(field: string, below = false): string | undefined {
  const { fieldError, showRefusalsAt } = useDraft();
  useEffect(() => showRefusalsAt(field, below), [showRefusalsAt, field, below]);
  return fieldError(field, below);
}

export function useFeatures(): FeatureAccess {
  const v = useContext(FeatureCtx);
  if (!v) throw new Error('a settings sub-page asked for the module registry outside the settings shell');
  return v;
}
