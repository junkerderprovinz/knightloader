// The outbound connection list for everything outside the settings page, so a
// task's connection id can be shown by name. The download list asks per row,
// so the settings document is fetched once per instance and shared.
//
// Not live: nothing announces a settings save, so a connection renamed in
// another tab keeps its old label until refreshConnections or a reload.

import { useEffect, useSyncExternalStore } from 'react';

/**
 * The machine's own connection, listed with the configured rows. Mirrors
 * proxycfg.DirectID. A task carrying it was kept off every proxy by a rule;
 * an empty id means nothing has routed the task yet.
 */
export const DIRECT_ID = 'direct';

/** One row of the connection list, as the server sends it. */
export interface Connection {
  id: string;
  type: 'none' | 'direct' | 'http' | 'https' | 'socks4' | 'socks4a' | 'socks5';
  host?: string;
  port?: number;
  enabled: boolean;
  order: number;
  filter?: string[];
  maxDownloads?: number;
}

const EMPTY: ReadonlyMap<string, Connection> = new Map();

// Keyed by instance, since connection ids mean nothing across instances.
const byBase = new Map<string, ReadonlyMap<string, Connection>>();
const inflight = new Map<string, Promise<void>>();
const listeners = new Set<() => void>();

function announce(): void {
  for (const l of listeners) l();
}

async function load(base: string): Promise<void> {
  try {
    const r = await fetch(`${base}/settings`);
    if (!r.ok) return;
    // The Settings type does not declare `connections`; the document is a
    // superset of it (see SettingsDraft.cfg).
    const cfg = (await r.json()) as { connections?: Connection[] };
    byBase.set(base, new Map((cfg.connections ?? []).map((c) => [c.id, c])));
    announce();
  } catch {
    // Not retried: a failure costs a label, and per-row retries would storm
    // an unreachable instance.
  } finally {
    inflight.delete(base);
  }
}

function ensure(base: string): void {
  if (byBase.has(base) || inflight.has(base)) return;
  inflight.set(base, load(base));
}

/** Forget what was loaded, so the next render fetches it again. */
export function refreshConnections(base?: string): void {
  if (base === undefined) byBase.clear();
  else byBase.delete(base);
  announce();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

/**
 * useConnections is the list for one instance, empty until it has arrived.
 * The snapshot must be a stable reference, hence the EMPTY constant and maps
 * that are replaced rather than mutated. The fetch starts in an effect so
 * rendering has no side effects.
 */
export function useConnections(base: string): ReadonlyMap<string, Connection> {
  useEffect(() => {
    ensure(base);
  }, [base]);
  return useSyncExternalStore(
    subscribe,
    () => byBase.get(base) ?? EMPTY,
    () => EMPTY,
  );
}

/**
 * endpointOf is how a proxy row reads, protocol and address, as on the
 * connection page. Empty for kinds without an endpoint, and never with
 * credentials.
 */
export function endpointOf(c: Connection): string {
  if (!c.host || c.type === 'none' || c.type === 'direct') return '';
  // IPv6 literals are bracketed before the port is appended.
  const host = c.host.includes(':') ? `[${c.host}]` : c.host;
  return `${c.type}://${c.port ? `${host}:${c.port}` : host}`;
}
