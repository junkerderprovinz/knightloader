// The module registry as the browser sees it. The shapes mirror
// internal/api/routes_features.go; the Go file is the source of truth, and
// everything here is deliberately permissive about values it does not know, so
// a module or a page a later wave adds renders as an unlabelled row rather than
// breaking the page it appears on.

/** Open on purpose: the server may add a verdict before this file learns it. */
export type FeatureVerdict = 'shipped' | 'desktop' | 'not-built' | (string & {});

export type FeatureSwitch = 'none' | 'setting' | 'parked' | (string & {});

export interface Feature {
  id: string;
  verdict: FeatureVerdict;
  /** The settings sub-page this module is configured on; '' when it has none. */
  page: string;
  /** Derived from live state on the server. Never a stored flag — see the Go file. */
  enabled: boolean;
  switch: FeatureSwitch;
  /**
   * Whether a parked-kind module has a value waiting to come back — which is
   * how "somebody switched this off" is told apart from "this was never set
   * up". Without the distinction the two deadlock each other: the page that
   * configures it disables its field because the module reads off, and the
   * switch refuses to turn on because nothing is configured.
   */
  parked: boolean;
  /** Why the verdict is what it is, or why there is no switch. English, from the server. */
  reason?: string;
  /** One line of live state: a folder, a port, a count. */
  detail?: string;
}

/**
 * A registered sub-page. There is no "is it built yet" flag on purpose: that is
 * whether a component exists, which only this side knows — see registry.tsx's
 * hasContent. The server owns the set and the order.
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
 * setFeature switches one module and answers with the whole table.
 *
 * The whole table, because switching folder watch off changes what the
 * Downloads page may offer and which rail entries mean anything — a client that
 * patched the one row locally would keep rendering the stale rest, which is the
 * drift the single server-side registry exists to prevent.
 */
export async function setFeature(id: string, enabled: boolean): Promise<FeatureState> {
  const r = await fetch(`/api/features/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  });
  // The server refuses a switch that cannot do anything, and the refusal names
  // the reason. Swallowing it would put us back at a control that looks like it
  // worked, which is the whole failure this registry exists to avoid.
  if (!r.ok) throw new Error((await r.text()).trim());
  return r.json();
}

/**
 * What the advanced table needs beyond the settings document itself: the
 * factory values, for the per-row reset, and the type of every key.
 *
 * The types cannot come from the values. Go writes an empty []string as JSON
 * null, so an unset list and an unset string arrive identically — a table that
 * guessed from the value would offer a text box for `archivePasswords` and the
 * save would be refused by the decoder. The kind map also names the keys
 * `omitempty` drops, so an empty connection list is a row you can fill rather
 * than a row that only appears once it is no longer empty.
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
