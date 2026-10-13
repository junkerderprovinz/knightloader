// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Task } from '../../lib/api';
import type { Connection } from '../../lib/connections';
import { I18nProvider } from '../../lib/i18n';
import { Routes, TORRENT_ROUTE, routesOf, type Route, type RouteTest } from './Routes';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

function reducedMotion(reduced: boolean) {
  vi.stubGlobal('matchMedia', () => ({ matches: reduced, addEventListener() {}, removeEventListener() {} }));
}

beforeEach(() => {
  reducedMotion(true);
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const proxy: Connection = { id: 'p', type: 'socks5', host: 'proxy.example.org', port: 1080, enabled: true, order: 0 };
const nas: Connection = { id: 'n', type: 'direct', enabled: true, order: 1, filter: ['nas.local'] };
const running = (connection: string, hostName: string): Task =>
  ({ id: `${connection}-${hostName}`, status: 'running', connection, host: hostName, resolver: 'http' }) as Task;

const read = (over: Partial<Parameters<typeof routesOf>[0]>) =>
  routesOf({ rows: [], enabled: true, torrentInterface: '', interfaceUp: true, tasks: [], ...over });

describe('the routes read off the settings', () => {
  it('are the one direct line while no connection is set up', () => {
    expect(read({})).toEqual([
      { id: 'direct', kind: 'direct', name: '', on: true, claims: [], rest: false, hosts: [], busy: false },
    ]);
  });

  it('give a row with a host filter a route of its own and leave the rest to the others', () => {
    const [shared, own, ...more] = read({ rows: [proxy, nas] });
    expect(shared).toMatchObject({ id: 'p', kind: 'proxy', name: 'proxy.example.org:1080', claims: [], rest: true });
    expect(own).toMatchObject({ id: 'n', kind: 'direct', claims: ['nas.local'], rest: false });
    // A proxy without a filter takes everything nobody claims, so nothing is left to go out directly.
    expect(more).toEqual([]);
  });

  it('keep the direct line for what no row claims while every row has a filter', () => {
    const routes = read({ rows: [nas] });
    expect(routes.map((r) => r.id)).toEqual(['n', 'direct']);
    expect(routes[1]).toMatchObject({ on: true, rest: true });
  });

  it('send everything out directly while the connections are switched off', () => {
    const routes = read({ rows: [proxy], enabled: false });
    expect(routes.map((r) => [r.id, r.on])).toEqual([
      ['p', false],
      ['direct', true],
    ]);
    expect(routes[0].probe).toBeUndefined();
  });

  it('leave out a row that names no connection', () => {
    expect(read({ rows: [{ id: 'x', type: 'none', enabled: true, order: 0 }] }).map((r) => r.id)).toEqual(['direct']);
  });

  it('add the interface torrents are tied to, off while it is down', () => {
    const up = read({ torrentInterface: 'wg0' }).find((r) => r.id === TORRENT_ROUTE)!;
    expect(up).toMatchObject({ kind: 'interface', name: 'wg0', on: true });
    expect(read({ torrentInterface: 'wg0', interfaceUp: false }).find((r) => r.id === TORRENT_ROUTE)!.on).toBe(false);
  });

  it('name the hosts with a download running over each route', () => {
    const tasks = [running('p', 'rapidgator.net'), running('p', 'rapidgator.net'), running('p', 'ddownload.com')];
    const [shared, own] = read({ rows: [proxy, nas], tasks: [...tasks, { ...running('n', 'nas.local'), status: 'queued' }] });
    expect(shared).toMatchObject({ busy: true, hosts: ['rapidgator.net', 'ddownload.com'] });
    expect(own).toMatchObject({ busy: false, hosts: [] });
  });

  it('dial a proxy as it stands and a direct row through a host it is kept for', () => {
    const [shared, own, wild] = read({
      rows: [proxy, { ...nas, filter: ['*.lan', 'nas.local'] }, { ...nas, id: 'w', filter: ['*.lan'] }],
    });
    expect(shared.probe).toEqual({ entry: proxy, target: '' });
    expect(own.probe!.target).toBe('nas.local');
    // A pattern is no host to reach.
    expect(wild.probe).toBeUndefined();
  });
});

describe('the picture of the routes', () => {
  const untested: RouteTest = { run: 0, verdicts: {} };
  const draw = (routes: Route[], test = untested, onPick = (_id: string) => {}) =>
    act(async () =>
      root.render(
        <I18nProvider>
          <Routes routes={routes} test={test} label="Outbound connections" onPick={onPick} />
        </I18nProvider>,
      ),
    );
  // The picture is drawn twice, for a wide card and a narrow one; the first is the wide one.
  const spoke = (id: string) => host.querySelector(`[data-route="${id}"]`)!;
  const line = (id: string) => spoke(id).querySelector('path')!;

  it('says in words what it draws', async () => {
    await draw(read({ rows: [proxy, nas] }));
    expect(host.querySelector('svg')!.getAttribute('aria-label')).toBe(
      'Outbound connections: proxy.example.org:1080 (Everything else), Direct (nas.local)',
    );
  });

  it('draws the one direct line for all hosts while nothing is set up', async () => {
    await draw(read({}));
    expect(host.querySelectorAll('svg:first-of-type [data-route]')).toHaveLength(1);
    expect(spoke('direct').textContent).toBe('Directall hosts');
    expect(line('direct').getAttribute('stroke')).toBe('var(--accent-glyph)');
    expect(host.textContent).toContain('This instance');
  });

  it('draws a route that is off as a grey dotted line and says so', async () => {
    await draw(read({ rows: [{ ...proxy, enabled: false }] }));
    expect(spoke('p').getAttribute('data-state')).toBe('off');
    expect(line('p').getAttribute('stroke')).toBe('var(--carbon-text-muted)');
    expect(line('p').getAttribute('stroke-dasharray')).toBe('2 7');
    expect(spoke('p').textContent).toContain('Off');
  });

  it('names the hosts running over a shared route behind what it is left with', async () => {
    const tasks = ['a.example', 'b.example', 'c.example', 'd.example'].map((name) => running('p', name));
    await draw(read({ rows: [proxy, nas], tasks }));
    expect(spoke('p').textContent).toContain('Everything else: a.example, b.example and 2 more');
  });

  it('draws the first ten rows of a long list and counts the rest', async () => {
    const rows = Array.from({ length: 14 }, (_, i) => ({ ...proxy, id: `p${i}`, filter: [`h${i}.example`] }));
    await draw(read({ rows }));
    // Fourteen rows with a filter each leave the rest to the machine's own connection, which stays in the picture.
    expect(host.querySelectorAll('svg:first-of-type [data-route]')).toHaveLength(11);
    expect(spoke('direct')).not.toBeNull();
    expect(host.querySelector('p')!.textContent).toBe('and 4 more');
    expect(host.querySelector('svg')!.getAttribute('aria-label')).toMatch(/ and 4 more$/);
  });

  it('turns a line green or red with the answer of the test and marks its route', async () => {
    const routes = read({ rows: [proxy, nas] });
    await draw(routes, { run: 1, verdicts: { p: 'ok', n: 'fail' } });
    expect(spoke('p').getAttribute('data-state')).toBe('ok');
    expect(line('p').getAttribute('stroke')).toBe('var(--status-ok-solid)');
    expect(line('n').getAttribute('stroke')).toBe('var(--status-fail-solid)');
    expect(spoke('p').querySelector('circle[fill="var(--status-ok-solid)"]')).not.toBeNull();
    expect(spoke('n').querySelector('circle[fill="var(--status-fail-solid)"]')).not.toBeNull();

    // An answer the test has not brought yet leaves the line as it was.
    await draw(routes, { run: 2, verdicts: { p: 'ok' } });
    expect(spoke('n').getAttribute('data-state')).toBe('on');
  });

  it("opens a route from its figure, but not the machine's own connection", async () => {
    const picked: string[] = [];
    await draw(read({ rows: [nas] }), untested, (id) => picked.push(id));
    const figure = spoke('n').querySelector<SVGGElement>('[role="button"]')!;
    expect(figure.getAttribute('aria-label')).toBe('Direct (nas.local)');
    await act(async () => figure.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    await act(async () => figure.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })));
    expect(picked).toEqual(['n', 'n']);
    expect(spoke('direct').querySelector('[role="button"]')).toBeNull();
  });

  describe('with motion on', () => {
    let flights: { to: string; land: () => void }[];

    beforeEach(() => {
      reducedMotion(false);
      flights = [];
      // jsdom has neither a SMIL clock nor the Web Animations API.
      Object.assign(SVGSVGElement.prototype, { pauseAnimations() {}, unpauseAnimations() {}, setCurrentTime() {} });
      vi.stubGlobal(
        'IntersectionObserver',
        class {
          observe() {}
          disconnect() {}
        },
      );
      Object.assign(SVGElement.prototype, {
        animate(frames: { transform: string }[]) {
          let land = () => {};
          const finished = new Promise<void>((done) => (land = done));
          flights.push({ to: frames[1].transform, land });
          return { finished, cancel() {} };
        },
      });
    });

    afterEach(() => {
      delete (SVGElement.prototype as { animate?: unknown }).animate;
    });

    it('carries a packet on a route only while a download runs over it', async () => {
      await draw(read({ rows: [proxy, nas], tasks: [running('p', 'a.example')] }));
      expect(spoke('p').querySelector('[data-stream] animateMotion')).not.toBeNull();
      expect(spoke('n').querySelector('[data-stream]')).toBeNull();
    });

    it('sends a packet down a tested line and shows the answer once it has arrived', async () => {
      const routes = read({ rows: [proxy] });
      await draw(routes, { run: 1, verdicts: { p: 'ok' } });
      // The answer is in, the packet still on its way.
      expect(spoke('p').getAttribute('data-state')).toBe('on');
      expect(flights.length).toBeGreaterThan(0);

      await act(async () => {
        flights.forEach((f) => f.land());
        await Promise.resolve();
      });
      expect(spoke('p').getAttribute('data-state')).toBe('ok');
    });

    it('redraws a tested picture with another number of routes', async () => {
      const test: RouteTest = { run: 1, verdicts: { p: 'ok' } };
      await draw(read({ rows: [{ ...proxy, filter: ['a.example'] }] }), test);
      await act(async () => {
        flights.forEach((f) => f.land());
        await Promise.resolve();
      });
      expect(spoke('p').getAttribute('data-state')).toBe('ok');

      await draw(read({ rows: [{ ...proxy, filter: ['a.example'] }, nas] }), test);
      expect(spoke('p').getAttribute('data-state')).toBe('ok');
      expect(spoke('n')).not.toBeNull();
    });
  });
});
