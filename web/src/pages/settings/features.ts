// The module registry as the browser sees it. The shapes mirror
// internal/api/routes_features.go and accept values they do not know, so a new
// module or page renders as an unlabelled row instead of breaking the page.

/** Open on purpose: the server may add a verdict before this file learns it. */
export type FeatureVerdict = 'shipped' | 'desktop' | 'not-built' | (string & {});

export type FeatureSwitch = 'none' | 'setting' | 'parked' | (string & {});

export interface Feature {
  id: string;
  verdict: FeatureVerdict;
  /** The settings sub-page this module is configured on; '' when it has none. */
  page: string;
  /** Derived from live state on the server, not stored. */
  enabled: boolean;
  switch: FeatureSwitch;
  /**
   * Whether a parked-kind module has a value waiting to come back, which tells
   * "switched off" apart from "never set up". Without it the page would disable
   * the field while the switch refused to turn on.
   */
  parked: boolean;
  /** Why the verdict is what it is, or why there is no switch. English, from the server. */
  reason?: string;
  /** `reason` as a value the interface words itself (moduleReason in tx.ts), with the values it needs. */
  reasonCode?: string;
  reasonArgs?: Record<string, string>;
  /** One line of live state: a folder, a port, a count. English, from the server. */
  detail?: string;
  /** `detail` as a value the interface words itself (moduleDetail in tx.ts), with the values it needs. */
  detailCode?: string;
  detailArgs?: Record<string, string>;
}

/**
 * FeaturePage is a registered sub-page. The server owns the set and the order;
 * whether a page has content is known only here (registry.tsx's hasContent).
 */
export interface FeaturePage {
  id: string;
  modules: string[] | null;
}

export interface FeatureState {
  modules: Feature[];
  pages: FeaturePage[];
}

export async function fetchFeatures(): Promise<FeatureState> {
  const r = await fetch('/api/features');
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}

/**
 * setFeature switches one module and answers with the whole table, since one
 * switch can change what other rows and pages offer.
 */
export async function setFeature(id: string, enabled: boolean): Promise<FeatureState> {
  const r = await fetch(`/api/features/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  });
  // The server refuses a switch that cannot do anything and names the reason.
  if (!r.ok) throw new Error((await r.text()).trim());
  return r.json();
}

/**
 * SettingsSchema carries the factory values and the type of every key for the
 * advanced table. The types cannot be guessed from the values, because Go
 * writes an empty list as null and omitempty drops keys.
 */
export interface SettingsSchema {
  values: Record<string, unknown>;
  kinds: Record<string, string>;
}

export async function fetchSettingsSchema(): Promise<SettingsSchema> {
  const r = await fetch('/api/settings/defaults');
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}
