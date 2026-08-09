// The outbound connection list, as everything OUTSIDE the settings page needs
// it: a task carries the id of the connection that is carrying it, and an id is
// not something anybody can read.
//
// Why this is a store of its own rather than a call in the component that wants
// it: the download list asks per row. Several hundred rows fetching the settings
// document each is not a rendering choice, it is a denial of service against the
// instance being rendered. So the document is fetched once per instance, held
// here, and handed to every row that asks.
//
// It is deliberately NOT live. There is no websocket event for a settings save,
// so a connection renamed in another tab keeps its old label here until
// something calls refreshConnections or the page is reloaded. That is the right
// trade for a label: showing yesterday's name for a minute is a much smaller
// failure than a list that re-fetches its own configuration on every repaint.

import { useEffect, useSyncExternalStore } from 'react';

/**
 * The direct gateway: the machine's own connection, offered in the same list as
 * the configured rows. Mirrors proxycfg.DirectID and must not drift from it.
 *
 * A task carrying this is a task somebody deliberately kept off every proxy. A
 * task carrying the EMPTY string is a task nothing has routed yet, which is a
 * different thing and must not be shown as though a decision had been made.
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

// One entry per instance, because a second instance's task list is that
// instance's tasks routed over that instance's connections, and the ids mean
// nothing across the two.
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
    // The settings type in lib/api.ts does not name `connections`: the document
    // is deliberately a superset of it, see SettingsDraft.cfg, so this cast is
    // the same gap the settings page casts across and disappears with it.
    const cfg = (await r.json()) as { connections?: Connection[] };
    byBase.set(base, new Map((cfg.connections ?? []).map((c) => [c.id, c])));
    announce();
  } catch {
    // Left unset rather than retried. A failure here costs a label, and a list
    // that retries per row turns one unreachable instance into a request storm.
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
 *
 * The snapshot has to be a STABLE REFERENCE or React re-renders for ever, which
 * is why EMPTY is a module constant and a loaded map is replaced rather than
 * mutated. Building `new Map()` in the getter here would spin the whole task
 * list, every row of it.
 *
 * The fetch is kicked off from an effect and not from the render, even though
 * ensure is guarded and calling it twice does nothing: a render that starts
 * network traffic is a render with a side effect, and the next person to wrap
 * this list in a transition or a suspense boundary would have no way to know.
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
 * endpointOf is how a proxy row reads: the protocol and where it points, which
 * is the same identity the connection page shows for it.
 *
 * Empty for the kinds that name no endpoint. Credentials are never part of it -
 * a user name in a task list is a secret leaking one column at a time.
 */
export function endpointOf(c: Connection): string {
  if (!c.host || c.type === 'none' || c.type === 'direct') return '';
  // Bracketed, because an IPv6 literal read against a port is a different
  // machine entirely, which is the same reason the Go side joins it this way.
  const host = c.host.includes(':') ? `[${c.host}]` : c.host;
  return `${c.type}://${c.port ? `${host}:${c.port}` : host}`;
}
