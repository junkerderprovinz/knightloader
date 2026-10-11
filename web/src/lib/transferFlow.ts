// The ways the running downloads take, for the picture of the line from the
// hoster to the disk (components/pictures/TransferFlow.tsx): where each one
// loads from, what carries it and the folder it lands in. Everything here is
// read off the tasks; a way nothing names is left out rather than guessed.
import type { CatalogueService, DiskReport, DiskVolume, Task } from './api';
import { DIRECT_ID, endpointOf, type Connection } from './connections';
import type { useT } from './i18n';
import { resolverLabel } from './resolverLabels';
import { hostOf } from './searchQuery';
import { downloadsVolume, folderName, roleLabel } from './useDiskSpace';

type Translate = ReturnType<typeof useT>['t'];

export interface FlowNode {
  id: string;
  name: string;
  /** Bytes a second through this node, 0 while everything on it waits. */
  speed: number;
}

export interface FlowSource extends FlowNode {
  /** The file host, absent where the bytes come from peers, from Usenet or from a backend's own store. */
  host?: string;
}

export interface FlowRoute extends FlowNode {
  kind: 'direct' | 'proxy' | 'account';
  /** The proxy an account's link is fetched over. */
  via?: string;
}

export interface FlowFolder extends FlowNode {
  dir: string;
  /** The folder's row in the disk report, null where the report has none for it. */
  volume: DiskVolume | null;
}

/** One line of the picture, between two nodes by id. */
export interface FlowLink {
  from: string;
  to: string;
  speed: number;
}

/** One way from end to end, for the words that stand in for the picture. */
export interface FlowPath {
  source: string;
  route?: string;
  folder: string;
  speed: number;
}

export interface Flow {
  sources: FlowSource[];
  routes: FlowRoute[];
  folders: FlowFolder[];
  /** A source to its route, a route to its folder, and a source straight to its folder where nothing says what carries it. */
  links: FlowLink[];
  paths: FlowPath[];
  speed: number;
}

export interface FlowContext {
  connections: ReadonlyMap<string, Connection>;
  /** The services an account can be stored for; null until the list has arrived. */
  services: readonly CatalogueService[] | null;
  disk: DiskReport | null;
}

/** How many nodes a column shows before the slowest are drawn as one. */
export const FLOW_ROWS = 4;

const MIB = 1024 * 1024;

/**
 * density is how closely packets follow each other on a line, 1 to 5, and 0
 * for a line nothing moves on. The steps are fixed, so a line reads the same
 * whatever else is loading and does not flicker between two neighbours.
 */
export function density(speed: number): number {
  if (speed <= 0) return 0;
  if (speed < MIB / 8) return 1;
  if (speed < MIB) return 2;
  if (speed < 8 * MIB) return 3;
  if (speed < 32 * MIB) return 4;
  return 5;
}

function volumeOf(dir: string, disk: DiskReport | null): DiskVolume | null {
  let pick: DiskVolume | null = null;
  for (const v of disk?.volumes ?? []) {
    if (v.dir === dir) return v;
    // A destination from a path template is listed by its fixed head.
    if (v.dir && dir.startsWith(v.dir) && (!pick || v.dir.length > pick.dir.length)) pick = v;
  }
  return pick;
}

/** The debrid service a task's link goes through, with the account where it is a named one. */
function accountOf(task: Task, services: readonly CatalogueService[] | null): string | undefined {
  let slot = task.resolver;
  // The files of a Usenet job a service fetched are staged under usenet://<account>/.
  if (slot === 'usenet') {
    try {
      slot = decodeURIComponent(new URL(task.url).hostname);
    } catch {
      return undefined;
    }
  }
  const cut = slot.indexOf('#');
  const id = cut < 0 ? slot : slot.slice(0, cut);
  const service = services?.find((s) => s.id === id && s.group === 'debrid');
  if (!service) return undefined;
  return cut < 0 ? service.label : `${service.label} (${slot.slice(cut + 1)})`;
}

function sourceOf(task: Task, account: string | undefined, t: Translate): FlowSource {
  if (task.resolver === 'torrent') return { id: 's:peers', name: t('columns.peers'), speed: 0 };
  if (task.resolver === 'nntp') return { id: 's:nntp', name: resolverLabel('nntp', t), speed: 0 };
  const host = hostOf(task);
  // A name without a dot is no site: the server files a magnet under a bucket
  // of its own, and a service's own file is addressed by an id.
  if (host.includes('.')) return { id: `s:${host}`, name: host, host, speed: 0 };
  const name = account ?? resolverLabel(task.resolver, t);
  return { id: `s:=${name}`, name, speed: 0 };
}

function routeOf(task: Task, source: FlowSource, account: string | undefined, ctx: FlowContext, t: Translate): FlowRoute | null {
  const id = task.connection ?? '';
  const row = ctx.connections.get(id);
  const proxy = row ? endpointOf(row) : '';
  // An account that is the source already is not also the way.
  if (account && source.host) {
    return { id: `r:${task.resolver}|${proxy}`, kind: 'account', name: account, via: proxy || undefined, speed: 0 };
  }
  // No connection id means nothing has routed the task, which is not "direct".
  if (!id) return null;
  if (proxy) return { id: `r:${id}`, kind: 'proxy', name: proxy, speed: 0 };
  // An id without a row was deleted or has not arrived, and keeps its id for a name.
  if (id !== DIRECT_ID && !row) return { id: `r:${id}`, kind: 'proxy', name: id, speed: 0 };
  return { id: 'r:direct', kind: 'direct', name: t('settings.connections.kind.direct'), speed: 0 };
}

/** The folder a download lands in: its own, or the download folder where it names none. */
function folderOf(own: string | undefined, ctx: FlowContext, t: Translate): FlowFolder {
  const dir = own || downloadsVolume(ctx.disk)?.dir || '';
  return {
    id: `f:${dir}`,
    name: dir ? folderName(dir) : roleLabel(t, 'downloads'),
    dir,
    volume: dir ? volumeOf(dir, ctx.disk) : null,
    speed: 0,
  };
}

/** add counts `speed` on the node or link filed under `key`, filing `fresh` first where there is none. */
function add<T extends { speed: number }>(into: Map<string, T>, key: string, fresh: T, speed: number): T {
  let kept = into.get(key);
  if (!kept) {
    kept = fresh;
    into.set(key, kept);
  }
  kept.speed += speed;
  return kept;
}

/**
 * fold keeps the fastest nodes of a column and draws the rest as one, so a
 * queue across twenty hosters stays a picture. It returns the nodes to draw
 * and, for each folded node, the id it goes by in the picture.
 */
function fold<T extends FlowNode>(nodes: T[], rest: (count: number, speed: number) => T): [T[], Map<string, string>] {
  const sorted = [...nodes].sort((a, b) => b.speed - a.speed || a.name.localeCompare(b.name));
  if (sorted.length <= FLOW_ROWS) return [sorted, new Map()];
  const kept = sorted.slice(0, FLOW_ROWS - 1);
  const folded = sorted.slice(FLOW_ROWS - 1);
  const merged = rest(
    folded.length,
    folded.reduce((sum, n) => sum + n.speed, 0),
  );
  return [[...kept, merged], new Map(folded.map((n) => [n.id, merged.id]))];
}

/** flowOf reads the ways of every running download. */
export function flowOf(tasks: readonly Task[], ctx: FlowContext, t: Translate): Flow {
  const sources = new Map<string, FlowSource>();
  const routes = new Map<string, FlowRoute>();
  const folders = new Map<string, FlowFolder>();
  const links = new Map<string, FlowLink>();
  const paths = new Map<string, FlowPath>();
  let total = 0;

  const link = (from: string, to: string, speed: number) => add(links, `${from}\n${to}`, { from, to, speed: 0 }, speed);

  for (const task of tasks) {
    if (task.status !== 'running') continue;
    const account = accountOf(task, ctx.services);
    const source = sourceOf(task, account, t);
    const route = routeOf(task, source, account, ctx, t);
    const folder = folderOf(task.dir, ctx, t);
    const speed = Math.max(0, task.speed);
    total += speed;

    add(sources, source.id, source, speed);
    add(folders, folder.id, folder, speed);
    if (route) {
      add(routes, route.id, route, speed);
      link(source.id, route.id, speed);
      link(route.id, folder.id, speed);
    } else {
      link(source.id, folder.id, speed);
    }
    add(
      paths,
      `${source.id}\n${route?.id ?? ''}\n${folder.id}`,
      { source: source.name, route: route?.name, folder: folder.name, speed: 0 },
      speed,
    );
  }

  // At rest the picture keeps the folder the next download would land in.
  if (sources.size === 0 && downloadsVolume(ctx.disk)) {
    const home = folderOf(undefined, ctx, t);
    folders.set(home.id, home);
  }

  const more = (n: number) => t('picture.flow.more', { n });
  const [shownSources, sourceIds] = fold<FlowSource>([...sources.values()], (n, speed) => ({ id: 's:+', name: more(n), speed }));
  const [shownRoutes, routeIds] = fold<FlowRoute>([...routes.values()], (n, speed) => ({
    id: 'r:+',
    kind: 'proxy',
    name: more(n),
    speed,
  }));
  const [shownFolders, folderIds] = fold<FlowFolder>([...folders.values()], (n, speed) => ({
    id: 'f:+',
    name: more(n),
    dir: '',
    volume: null,
    speed,
  }));

  const drawn = new Map<string, FlowLink>();
  const drawnAs = (id: string) => sourceIds.get(id) ?? routeIds.get(id) ?? folderIds.get(id) ?? id;
  for (const l of links.values()) {
    const from = drawnAs(l.from);
    const to = drawnAs(l.to);
    add(drawn, `${from}\n${to}`, { from, to, speed: 0 }, l.speed);
  }

  return {
    sources: shownSources,
    routes: shownRoutes,
    folders: shownFolders,
    links: [...drawn.values()],
    paths: [...paths.values()].sort((a, b) => b.speed - a.speed),
    speed: total,
  };
}
