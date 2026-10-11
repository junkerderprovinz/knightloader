import { useLayoutEffect, useRef, useState, type ComponentType, type SVGProps } from 'react';
import logoUrl from '../../assets/logo.svg';
import type { Task } from '../../lib/api';
import { DIRECT_ID, addressOf, type Connection } from '../../lib/connections';
import { useT } from '../../lib/i18n';
import { IconCheck, IconClose, IconGlobe, IconNetwork, IconShieldOn } from '../../lib/icons';
import type { ButtonVerdict } from '../ui';
import { FAIL, FILL, GROUND, HUE, OK, QUIET, SURFACE, TRAVEL, useStage } from './scene';
import { HubLabel, SpokeLabel, Stream, lineOf, spokeLayout, type Spoke, type SpokeShape } from './spokes';

/** One way out of this machine: its own connection, a row of the connection list, or the interface torrents are tied to. */
export interface Route {
  /** The connection's id, DIRECT_ID for the machine's own connection, TORRENT_ROUTE for the interface. */
  id: string;
  kind: 'direct' | 'proxy' | 'interface';
  /** The address or the interface's name; empty for a direct route, which has none. */
  name: string;
  /** Whether anything can go out over it as things stand. */
  on: boolean;
  /** The hosts it is kept for. Empty on a route that takes what no other claims. */
  claims: string[];
  /** Whether other routes claim hosts, so this one is left with the rest. */
  rest: boolean;
  /** The hosts with a download running over it. */
  hosts: string[];
  /** Whether something is going out over it at the moment. */
  busy: boolean;
  /** What a test dials: the row as it stands and a host to reach through it. Absent where there is nothing to dial. */
  probe?: { entry: Connection; target: string };
}

export const TORRENT_ROUTE = 'torrents';

const hasWildcard = (pattern: string) => /[*?[]/.test(pattern);

/**
 * routesOf reads the routes off the settings and the running downloads, as
 * proxycfg's picker walks them: a row whose filter names a host has it to
 * itself, the rows without a filter share the rest, and the machine's own
 * connection takes the rest only while no such row is in use.
 */
export function routesOf({
  rows,
  enabled,
  torrentInterface,
  interfaceUp,
  tasks,
}: {
  rows: Connection[];
  /** The connections module's switch; off, everything goes out directly. */
  enabled: boolean;
  /** The interface the built-in torrent client is tied to, empty for none. */
  torrentInterface: string;
  interfaceUp: boolean;
  tasks: Task[];
}): Route[] {
  const running = tasks.filter((task) => task.status === 'running');
  const hostsOver = (id: string) => [
    ...new Set(running.filter((task) => task.connection === id && task.host).map((task) => task.host!)),
  ];

  const named = rows.filter((row) => row.type !== 'none');
  const inUse = (row: Connection) => enabled && row.enabled;
  const claimed = named.some((row) => inUse(row) && (row.filter ?? []).length > 0);
  const shared = named.some((row) => inUse(row) && (row.filter ?? []).length === 0);

  const out = named.map((row): Route => {
    const claims = row.filter ?? [];
    const on = inUse(row);
    // A direct row has no address of its own, so the test reaches for a host it is kept for.
    const target = row.type === 'direct' ? claims.find((host) => !hasWildcard(host)) : '';
    return {
      id: row.id,
      kind: row.type === 'direct' ? 'direct' : 'proxy',
      name: addressOf(row),
      on,
      claims,
      rest: claimed && claims.length === 0,
      hosts: on ? hostsOver(row.id) : [],
      busy: on && running.some((task) => task.connection === row.id),
      probe: on && target !== undefined && (row.type === 'direct' || !!row.host) ? { entry: row, target } : undefined,
    };
  });

  if (!shared) {
    out.push({
      id: DIRECT_ID,
      kind: 'direct',
      name: '',
      on: true,
      claims: [],
      rest: claimed,
      hosts: hostsOver(DIRECT_ID),
      busy: running.some((task) => task.connection === DIRECT_ID),
    });
  }
  if (torrentInterface) {
    out.push({
      id: TORRENT_ROUTE,
      kind: 'interface',
      name: torrentInterface,
      on: interfaceUp,
      claims: [],
      rest: false,
      hosts: [],
      busy: interfaceUp && running.some((task) => task.resolver === 'torrent'),
    });
  }
  return out;
}

/** The answers of the last test: its number, counted up with every run, and each route's verdict once it is in. */
export interface RouteTest {
  run: number;
  verdicts: Record<string, ButtonVerdict>;
}

const SIZES = { pitch: 54, hub: 30, node: 17 };
const GLYPH: Record<Route['kind'], ComponentType<SVGProps<SVGSVGElement>>> = {
  direct: IconGlobe,
  proxy: IconNetwork,
  interface: IconShieldOn,
};
// How many hosts a label names before it counts the rest.
const NAMED = 2;
// How many rows of the list the picture draws. An imported proxy list can run to hundreds.
const DRAWN = 10;

/**
 * Routes draws how downloads leave this machine: this instance, each route
 * beside it and the hosts behind each. A route in use carries a packet while a
 * download runs over it. A test sends a packet down every line it can dial, and
 * the line turns green or red with the answer.
 */
export function Routes({
  routes,
  test,
  label,
  onPick,
}: {
  routes: Route[];
  test: RouteTest;
  /** What the picture shows, read out before the routes. */
  label: string;
  /** Opens what a route is set up in. */
  onPick: (id: string) => void;
}) {
  const { t } = useT();

  const nameOf = (route: Route) => {
    if (route.kind === 'direct') return t('settings.connections.kind.direct');
    return route.kind === 'interface' && !route.on ? t('settings.torrents.interfaceGone', { name: route.name }) : route.name;
  };
  const some = (hosts: string[]) =>
    hosts.length > NAMED
      ? `${hosts.slice(0, NAMED).join(', ')} ${t('tray.more', { n: hosts.length - NAMED })}`
      : hosts.join(', ');
  const behind = (route: Route) => {
    if (route.kind === 'interface') return t('settings.nav.torrents');
    if (!route.on) return t('settings.modules.off');
    if (route.claims.length > 0) return some(route.claims);
    const share = route.rest ? t('volume.other') : t('settings.connections.filterAll');
    return route.hosts.length > 0 ? `${share}: ${some(route.hosts)}` : share;
  };

  // The machine's own connection and the torrents' interface are always drawn; the list's rows up to a point.
  const listed = routes.filter((route) => route.id !== DIRECT_ID && route.id !== TORRENT_ROUTE);
  const left = listed.slice(DRAWN);
  const drawn = routes
    .filter((route) => !left.includes(route))
    .map((route) => ({ route, name: nameOf(route), sub: behind(route) }));
  const more = left.length > 0 ? t('tray.more', { n: left.length }) : '';
  const alt = `${label}: ${drawn.map((d) => `${d.name} (${d.sub})`).join(', ')} ${more}`.trimEnd();
  return (
    <div data-new="routes" className="kl-pic @container">
      {(['wide', 'narrow'] as const).map((shape) => (
        <Net key={shape} shape={shape} drawn={drawn} test={test} alt={alt} onPick={onPick} />
      ))}
      {more && <p className="mt-2 text-center text-meta text-carbon-textMuted">{more}</p>}
    </div>
  );
}

interface Drawn {
  route: Route;
  name: string;
  sub: string;
}

function Net({
  shape,
  drawn,
  test,
  alt,
  onPick,
}: {
  shape: SpokeShape;
  drawn: Drawn[];
  test: RouteTest;
  alt: string;
  onPick: (id: string) => void;
}) {
  const { t } = useT();
  const { stage, still } = useStage(drawn.map((d) => `${d.route.id}${d.route.busy ? '+' : ''}`).join(' '));
  const { width, height, hub, hubLabel, spokes } = spokeLayout(shape, drawn.length, SIZES);
  return (
    <svg
      ref={stage}
      viewBox={`0 0 ${width} ${height}`}
      role="group"
      aria-label={alt}
      className={`mx-auto w-full overflow-visible ${
        shape === 'wide' ? 'hidden max-w-[50rem] @[42rem]:block' : 'block max-w-[26rem] @[42rem]:hidden'
      }`}
    >
      {drawn.map((d, i) => (
        <RouteSpoke key={d.route.id} {...d} spoke={spokes[i]} lead={0.2 + (i % 3) * 0.3} still={still} test={test} onPick={onPick} />
      ))}
      {/* The app's own mark stands for the app. */}
      <image href={logoUrl} x={hub.x - 17} y={hub.y - 27} width="34" height="54" className="kl-pic-lift" />
      <HubLabel at={hubLabel}>{t('instances.thisInstance')}</HubLabel>
    </svg>
  );
}

function RouteSpoke({
  route,
  name,
  sub,
  spoke,
  lead,
  still,
  test,
  onPick,
}: Drawn & {
  spoke: Spoke;
  lead: number;
  still: boolean;
  test: RouteTest;
  onPick: (id: string) => void;
}) {
  const packet = useRef<SVGCircleElement>(null);
  // The run whose packet has reached the route.
  const [landed, setLanded] = useState(0);
  const tested = route.probe !== undefined && test.run > 0;
  const [fromX, fromY] = spoke.from;
  const [toX, toY] = spoke.to;

  useLayoutEffect(() => {
    if (!tested || still) return;
    const flight = packet.current!.animate(
      [{ transform: `translate(${fromX}px, ${fromY}px)` }, { transform: `translate(${toX}px, ${toY}px)` }],
      { duration: (Math.hypot(toX - fromX, toY - fromY) / TRAVEL) * 1000, fill: 'forwards' },
    );
    const run = test.run;
    flight.finished.then(
      () => setLanded(run),
      () => {
        // A new test or a redrawn picture called this packet back.
      },
    );
    return () => flight.cancel();
  }, [tested, still, test.run, fromX, fromY, toX, toY]);

  // The answer shows once the packet is there, so the line never turns ahead of it.
  const verdict = tested && (still || landed === test.run) ? test.verdicts[route.id] : undefined;
  const paint = verdict === 'ok' ? OK : verdict ? FAIL : route.on ? HUE : QUIET;
  const Glyph = GLYPH[route.kind];
  const Mark = verdict === 'ok' ? IconCheck : IconClose;
  // The machine's own connection is set up nowhere.
  const opens = route.id !== DIRECT_ID;

  return (
    <g data-route={route.id} data-state={verdict ?? (route.on ? 'on' : 'off')}>
      <path
        d={lineOf(spoke)}
        fill="none"
        stroke={paint}
        strokeOpacity={route.on ? undefined : 0.6}
        strokeWidth="2"
        strokeLinecap="round"
        strokeDasharray={route.on ? undefined : '2 7'}
      />
      {route.busy && !still && <Stream from={spoke.from} to={spoke.to} lead={lead} />}
      {tested && !still && !verdict && <circle ref={packet} r="4.5" fill={FILL} />}
      <g
        className={opens ? 'kl-net-node' : undefined}
        tabIndex={opens ? 0 : undefined}
        role={opens ? 'button' : undefined}
        aria-label={opens ? `${name} (${sub})` : undefined}
        onClick={opens ? () => onPick(route.id) : undefined}
        onKeyDown={
          opens
            ? (e) => {
                if (e.key !== 'Enter' && e.key !== ' ') return;
                e.preventDefault();
                onPick(route.id);
              }
            : undefined
        }
      >
        {opens && <circle className="kl-net-ring" cx={spoke.x} cy={spoke.y} r={SIZES.node + 5} />}
        <circle cx={spoke.x} cy={spoke.y} r={SIZES.node} fill={GROUND} />
        <g color={route.on ? HUE : QUIET} opacity={route.on ? undefined : 0.7}>
          <Glyph x={spoke.x - 9} y={spoke.y - 9} width={18} height={18} />
        </g>
      </g>
      {verdict && (
        <g transform={`translate(${spoke.x + 12} ${spoke.y - 12})`}>
          <circle r="7" fill={paint} stroke={SURFACE} strokeWidth="1.5" />
          <g color={SURFACE}>
            <Mark x={-4.5} y={-4.5} width={9} height={9} />
          </g>
        </g>
      )}
      <SpokeLabel at={spoke.label} name={name} sub={sub} faint={!route.on} ltr={route.kind !== 'direct'} />
    </g>
  );
}
