// @vitest-environment jsdom
import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { DiskReport, Settings, Task } from '../../lib/api';
import type { Connection } from '../../lib/connections';
import { I18nProvider, type TranslationKey } from '../../lib/i18n';
import { interpolate } from '../../lib/interpolate';
import { en } from '../../lib/locales/en';
import { flowOf, type Flow } from '../../lib/transferFlow';
import { TransferFlow, TransferFlowStrip } from './TransferFlow';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const MIB = 1024 * 1024;
const GIB = 1024 * MIB;

let root: Root;
let host: HTMLDivElement;

function reducedMotion(reduced: boolean) {
  vi.stubGlobal('matchMedia', () => ({ matches: reduced, addEventListener() {}, removeEventListener() {} }));
}

beforeEach(() => {
  reducedMotion(true);
  // jsdom has no SMIL clock and no way to see what is on screen.
  Object.assign(SVGSVGElement.prototype, { pauseAnimations() {}, unpauseAnimations() {}, setCurrentTime() {} });
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const draw = (picture: ReactNode) => act(async () => root.render(<I18nProvider>{picture}</I18nProvider>));

const t = ((key: TranslationKey, vars?: Record<string, string | number>) => interpolate(en[key], vars)) as Parameters<
  typeof flowOf
>[2];

const task = (over: Partial<Task>): Task =>
  ({
    id: 'a',
    url: 'https://files.example/a.zip',
    name: 'a.zip',
    package: '',
    resolver: 'direct',
    status: 'running',
    size: 100,
    loaded: 0,
    speed: MIB,
    enabled: true,
    connection: 'direct',
    createdAt: '2026-10-10T10:00:00Z',
    ...over,
  }) as Task;

const disk: DiskReport = {
  volumes: [
    { dir: '/downloads', measured: '/downloads', exists: true, known: true, free: 40 * GIB, used: 60 * GIB, total: 100 * GIB, queued: 0, tasks: 0, role: 'downloads' },
  ],
  truncated: false,
  sampledAt: '',
};
const connections = new Map<string, Connection>([
  ['direct', { id: 'direct', type: 'direct', enabled: true, order: 0 }],
  ['p1', { id: 'p1', type: 'socks5', host: '10.0.0.2', port: 1080, enabled: true, order: 1 }],
]);
const flow = (...tasks: Task[]): Flow => flowOf(tasks, { connections, services: [], disk }, t);
const cfg = { diskLowSpace: 20 * GIB, diskCriticalSpace: 5 * GIB } as Settings;

/** The wide arrangement; the narrow one draws the same from the same flow. */
const wide = () => host.querySelector('svg[role="img"]')!;
const lines = (state: 'moving' | 'still') => [...wide().querySelectorAll(`[data-line="${state}"]`)];
const packets = (line: Element) => Number(line.querySelector('[data-packets]')?.getAttribute('data-packets') ?? 0);

describe('the line from the hoster to the disk', () => {
  it('draws a line through while data moves on it, and dashed without packets while it waits', async () => {
    await draw(
      <TransferFlow
        title="From hoster to disk"
        cfg={cfg}
        flow={flow(task({}), task({ id: 'b', url: 'https://slow.example/b.zip', connection: 'p1', speed: 0 }))}
      />,
    );
    expect(lines('moving')).toHaveLength(2);
    expect(lines('still')).toHaveLength(2);
    for (const line of lines('moving')) {
      expect(line.querySelector('path')!.getAttribute('stroke-dasharray')).toBeNull();
      expect(packets(line)).toBeGreaterThan(0);
    }
    for (const line of lines('still')) {
      expect(line.querySelector('path')!.getAttribute('stroke-dasharray')).toBe('2 7');
      expect(line.querySelector('[data-packets]')).toBeNull();
    }
  });

  it('puts more packets on a faster line', async () => {
    await draw(<TransferFlow title="From hoster to disk" cfg={cfg} flow={flow(task({ speed: 200 * 1024 }))} />);
    const slow = packets(lines('moving')[0]);
    await draw(<TransferFlow title="From hoster to disk" cfg={cfg} flow={flow(task({ speed: 40 * MIB }))} />);
    expect(packets(lines('moving')[0])).toBeGreaterThan(slow);
  });

  it('stands the packets still on the line when motion is off, and sends them along it otherwise', async () => {
    const one = flow(task({}));
    await draw(<TransferFlow title="From hoster to disk" cfg={cfg} flow={one} />);
    expect(wide().querySelectorAll('animateMotion')).toHaveLength(0);
    expect(wide().querySelectorAll('[data-packets] circle').length).toBeGreaterThan(0);

    reducedMotion(false);
    await draw(<TransferFlow key="moving" title="From hoster to disk" cfg={cfg} flow={one} />);
    expect(wide().querySelectorAll('animateMotion').length).toBeGreaterThan(0);
  });

  it('fills the station that carries data and leaves a waiting one grey', async () => {
    await draw(
      <TransferFlow
        title="From hoster to disk"
        cfg={cfg}
        flow={flow(task({}), task({ id: 'b', url: 'https://slow.example/b.zip', connection: 'p1', speed: 0 }))}
      />,
    );
    const fill = (kind: string) => wide().querySelector(`[data-kind="${kind}"] circle`)!.getAttribute('fill');
    expect(fill('direct')).toBe('var(--accent)');
    expect(fill('proxy')).toBe('var(--carbon-surface2)');
  });

  it('shows the folder as a vessel at the level of its disk', async () => {
    await draw(<TransferFlow title="From hoster to disk" cfg={cfg} flow={flow(task({}))} />);
    expect(wide().querySelector('[data-folder] [role="progressbar"]')!.getAttribute('aria-valuenow')).toBe('60');
    expect(wide().querySelector('[data-folder]')!.textContent).toContain('downloads');
  });

  it('says in words which way each download takes', async () => {
    await draw(
      <TransferFlow
        title="From hoster to disk"
        cfg={cfg}
        flow={flow(task({ speed: 2 * MIB }), task({ id: 'b', url: 'magnet:?xt=urn:btih:abc', resolver: 'torrent', connection: undefined, speed: 0 }))}
      />,
    );
    const words = wide().getAttribute('aria-label')!.replace(/[⁦-⁩]/g, '');
    expect(words).toBe(
      'From hoster to disk: files.example over Direct into downloads: 2.0 MiB/s; Peers into downloads: Standing still',
    );
  });

  it('rests and says so when nothing is loading', async () => {
    await draw(<TransferFlow title="From hoster to disk" cfg={cfg} flow={flow()} />);
    expect(wide().getAttribute('data-state')).toBe('idle');
    expect(wide().querySelectorAll('[data-line], [data-source], [data-route]')).toHaveLength(0);
    // The folder the next download lands in stays.
    expect(wide().querySelectorAll('[data-folder]')).toHaveLength(1);
    expect(host.textContent).toContain('Nothing is loading right now.');
    expect(wide().getAttribute('aria-label')).toBe('From hoster to disk: Nothing is loading right now.');
  });
});

describe('the strip in the head bar', () => {
  const strip = () => host.querySelector('svg[role="img"]')!;

  it('shows the same ways on one line, with the packets of the summed speed', async () => {
    const both = flow(task({}), task({ id: 'b', url: 'https://other.example/b.zip', connection: 'p1', speed: 3 * MIB }));
    await draw(<TransferFlowStrip title="From hoster to disk" flow={both} />);
    expect(strip().getAttribute('data-state')).toBe('loading');
    expect(strip().querySelectorAll('[data-route]')).toHaveLength(2);
    expect(strip().querySelectorAll('[data-line="moving"]')).toHaveLength(2);
    expect(strip().getAttribute('aria-label')).toContain('other.example');
  });

  it('rests on a dashed line when nothing is loading', async () => {
    await draw(<TransferFlowStrip title="From hoster to disk" flow={flow()} />);
    expect(strip().getAttribute('data-state')).toBe('idle');
    expect(strip().querySelectorAll('[data-line="still"]')).toHaveLength(1);
    expect(strip().querySelector('[data-packets]')).toBeNull();
  });
});
