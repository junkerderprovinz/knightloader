// @vitest-environment jsdom
import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { DiskVolume, Settings } from '../../lib/api';
import { I18nProvider } from '../../lib/i18n';
import { IconPlay } from '../../lib/icons';
import { ToastProvider } from '../../lib/toast';
import { Accounts } from '../../pages/Accounts';
import { DiskVolumeRow } from '../DiskSpaceTile';
import { AfterDownload, type AfterDownloadSteps } from './AfterDownload';
import { PriorityFlow } from './PriorityFlow';
import { ReconnectScene } from './ReconnectScene';
import { TRAVEL, trip } from './scene';
import { SeedGauge, SeedRing } from './SeedRing';
import { Vessel } from './Vessel';
import { WeekBand, stretches } from './WeekBand';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

/** The system's answer to "less motion?". Most tests ask for it, so a picture stands on its still frame. */
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
  delete document.documentElement.dataset.motion;
  vi.unstubAllGlobals();
});

const draw = (picture: ReactNode) => act(async () => root.render(<I18nProvider>{picture}</I18nProvider>));

describe('a vessel', () => {
  /** How far down the picture the liquid's surface stands. */
  function surface(): number {
    const level = host.querySelector('g[clip-path] > g')!.getAttribute('transform')!;
    return Number(/translate\(0 ([\d.]+)\)/.exec(level)![1]);
  }

  it('stands the liquid higher the fuller it is', async () => {
    await draw(<Vessel share={0.25} label="Used" />);
    const quarter = surface();
    await draw(<Vessel share={0.75} label="Used" />);
    expect(surface()).toBeLessThan(quarter);
    // The floor is at 35.5 and the line for full at 8.
    expect(surface()).toBeCloseTo(35.5 - 0.75 * 27.5, 1);
    expect(host.querySelector('[role="progressbar"]')!.getAttribute('aria-valuenow')).toBe('75');
  });

  it('draws a mark as a line at the level it names', async () => {
    await draw(<Vessel share={0.5} label="Used" marks={[{ at: 0.9, tone: 'fail', label: 'Everything stops below here' }]} />);
    const line = host.querySelector('path[stroke="var(--status-fail-solid)"]')!;
    expect(line.getAttribute('d')).toBe('M3 10.75H31');
    const words = host.querySelector<HTMLElement>('[role="img"][aria-label="Everything stops below here"]')!;
    expect(parseFloat(words.style.top)).toBeCloseTo((10.75 / 40) * 100, 3);
  });

  it('stands open, with no line for full, when nothing says how much it holds', async () => {
    await draw(<Vessel share={null} label="Volume" />);
    expect(host.querySelector('[role="progressbar"]')).toBeNull();
    expect(host.querySelector('[role="img"]')!.getAttribute('aria-label')).toBe('Volume');
    expect(host.querySelector('path[stroke-dasharray="2.5 2.5"]')).toBeNull();
  });
});

describe('a target folder', () => {
  const GIB = 1024 ** 3;
  const folder = (free: number): DiskVolume =>
    ({
      dir: '/downloads',
      measured: '/downloads',
      exists: true,
      known: true,
      free,
      used: 100 * GIB - free,
      total: 100 * GIB,
      queued: 0,
      tasks: 0,
      role: 'downloads',
    }) as DiskVolume;
  const floors = { diskLowSpace: 20 * GIB, diskCriticalSpace: 5 * GIB } as Settings;

  it('carries both floors as lines on its vessel, the stop floor above the start floor', async () => {
    await draw(<DiskVolumeRow v={folder(60 * GIB)} cfg={floors} />);
    const top = (label: string) =>
      parseFloat(host.querySelector<HTMLElement>(`[role="img"][aria-label="${label}"]`)!.style.top);
    expect(top('Everything stops below here')).toBeLessThan(top('Nothing new starts below here'));
    expect(host.querySelector('[role="progressbar"]')!.getAttribute('aria-valuenow')).toBe('40');
  });

  it('turns the liquid red once the free space is under the stop floor', async () => {
    await draw(<DiskVolumeRow v={folder(60 * GIB)} cfg={floors} />);
    const liquid = () => host.querySelector('g[clip-path] path')!.getAttribute('fill');
    expect(liquid()).toBe('var(--accent-ink)');
    await draw(<DiskVolumeRow v={folder(2 * GIB)} cfg={floors} />);
    expect(liquid()).toBe('var(--status-fail-solid)');
  });
});

describe('the scene after a download', () => {
  const all: AfterDownloadSteps = { verify: true, repair: true, unpack: true, move: true, cleanup: true };
  const station = (id: string) => host.querySelector(`[data-station="${id}"]`)!;
  const lit = (id: string) => station(id).querySelector('circle[fill="var(--accent)"]') !== null;

  it('leaves a switched-off station grey and says it is off', async () => {
    await draw(<AfterDownload title="After a download" steps={{ ...all, verify: false, cleanup: false }} />);
    expect(station('verify').getAttribute('data-state')).toBe('off');
    expect(lit('verify')).toBe(false);
    expect(lit('cleanup')).toBe(false);
    expect(lit('unpack')).toBe(true);
    // The download itself has no switch.
    expect(lit('download')).toBe(true);
    expect(host.querySelector('svg')!.textContent).toContain('VerifyingOff');
  });

  it('reads out the steps that run', async () => {
    await draw(<AfterDownload title="After a download" steps={{ ...all, repair: false, move: false }} />);
    expect(host.querySelector('[role="img"]')!.getAttribute('aria-label')).toBe(
      'After a download: Downloading, Verifying, Unpacking, Cleaning up',
    );
  });

  it('lights a station again when its step is switched on', async () => {
    await draw(<AfterDownload title="After a download" steps={{ ...all, unpack: false }} />);
    expect(lit('unpack')).toBe(false);
    await draw(<AfterDownload title="After a download" steps={all} />);
    expect(lit('unpack')).toBe(true);
  });
});

describe('a trip', () => {
  const points: [number, number][] = [
    [0, 0],
    [70, 0],
    [210, 0],
  ];

  it('moves at one speed, so a leg twice as long takes twice as long', () => {
    const { dur, stops } = trip(points, () => 0);
    const first = (stops[1].arrive - stops[0].leave) * dur;
    const second = (stops[2].arrive - stops[1].leave) * dur;
    expect(first).toBeCloseTo(70 / TRAVEL, 5);
    expect(second).toBeCloseTo(2 * first, 5);
  });

  it('passes a point nothing rests at without stopping', () => {
    const { stops } = trip(points, (i) => (i === 2 ? 1 : 0));
    expect(stops[1].leave).toBe(stops[1].arrive);
    expect(stops[2].leave).toBeGreaterThan(stops[2].arrive);
  });
});

describe('the priority flow', () => {
  const backends = [
    { id: 'realdebrid', name: 'Real-Debrid' },
    { id: 'jd', name: 'JDownloader' },
    { id: 'direct', name: 'Direct download' },
  ];
  const names = () => [...host.querySelectorAll('foreignObject')].map((f) => f.textContent);

  it('stands the backends in the order they are asked', async () => {
    await draw(<PriorityFlow backends={backends} label="Priority order" />);
    expect(names()).toEqual(['Real-Debrid', 'JDownloader', 'Direct download']);
    expect(host.querySelector('[role="img"]')!.getAttribute('aria-label')).toBe(
      'Priority order: 1. Real-Debrid, 2. JDownloader, 3. Direct download',
    );
  });

  it('follows a new order', async () => {
    await draw(<PriorityFlow backends={backends} label="Priority order" />);
    await draw(<PriorityFlow backends={[backends[2], backends[0], backends[1]]} label="Priority order" />);
    expect(names()).toEqual(['Direct download', 'Real-Debrid', 'JDownloader']);
  });
});

describe('the priority order card', () => {
  const reply = (body: unknown) =>
    new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

  beforeEach(() => {
    vi.stubGlobal(
      'WebSocket',
      class {
        readyState = 0;
        send() {}
        close() {}
      },
    );
    const unobserved = class {
      observe() {}
      disconnect() {}
    };
    vi.stubGlobal('IntersectionObserver', unobserved);
    vi.stubGlobal('ResizeObserver', unobserved);
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url === '/api/resolvers/priority') {
          return reply([
            { id: 'torbox', prio: 2 },
            { id: 'jd', prio: 1 },
          ]);
        }
        if (url === '/api/settings') return reply({ premiumOnly: false });
        if (url.startsWith('/api/uistate')) return reply({});
        return reply([]);
      }),
    );
  });

  it('shows the flow first and keeps the draggable list behind the selector', async () => {
    await draw(
      <ToastProvider>
        <MemoryRouter>
          <Accounts />
        </MemoryRouter>
      </ToastProvider>,
    );
    expect(host.querySelector('[role="img"]')!.getAttribute('aria-label')).toBe(
      'Priority order: 1. TorBox, 2. JDownloader',
    );
    expect(host.querySelector('[data-ladder-id]')).toBeNull();

    const list = [...host.querySelectorAll<HTMLElement>('[aria-label="View"] [role="tab"]')].find(
      (tab) => tab.textContent === 'List',
    )!;
    await act(async () => list.click());
    expect([...host.querySelectorAll('[data-ladder-id]')].map((row) => row.getAttribute('data-ladder-id'))).toEqual([
      'torbox',
      'jd',
    ]);
    expect(host.textContent).toContain('Automatic');
  });
});

describe('the week band', () => {
  const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  // A Wednesday at noon.
  const now = new Date(2026, 9, 7, 12, 0);
  const band = (entries: Parameters<typeof WeekBand>[0]['entries'], quiet = false) => (
    <WeekBand entries={entries} dayLabels={days} now={now} nowLabel="12:00" quiet={quiet} label="Schedules" />
  );
  const lane = (day: number) => host.querySelector(`[data-day="${day}"]`)!;

  it('carries a window past midnight into the next day', () => {
    const night = stretches([{ days: [1], start: '22:00', end: '06:00', action: 'pause' }]);
    expect(night.map(({ day, from, to }) => [day, from, to])).toEqual([
      [1, 22 * 60, 24 * 60],
      [2, 0, 6 * 60],
    ]);
  });

  it('keeps two schedules that overlap on lines of their own', () => {
    const both = stretches([
      { days: [1], start: '08:00', end: '12:00', action: 'pause' },
      { days: [1], start: '10:00', end: '14:00', action: 'limit' },
      { days: [1], start: '13:00', end: '15:00', action: 'resume' },
    ]);
    expect(both.map((s) => s.track)).toEqual([0, 1, 0]);
  });

  it('draws each schedule on the days it was given, over its hours and in its action', async () => {
    await draw(
      band([
        { days: [1, 3], start: '06:00', end: '12:00', action: 'pause' },
        { days: [6], start: '00:00', end: '18:00', action: 'limit' },
      ]),
    );
    const monday = lane(1).querySelector<HTMLElement>('[data-entry="0"]')!;
    expect(monday.getAttribute('data-action')).toBe('pause');
    expect(parseFloat(monday.style.insetInlineStart)).toBe(25);
    expect(parseFloat(monday.style.width)).toBe(25);
    expect(lane(3).querySelector('[data-entry="0"]')).not.toBeNull();
    expect(lane(2).querySelector('[data-entry]')).toBeNull();
    expect(lane(6).querySelector('[data-entry="1"]')!.getAttribute('data-action')).toBe('limit');
    expect(host.querySelectorAll('[data-entry]')).toHaveLength(3);
  });

  it('marks the present moment on today and on no other day', async () => {
    await draw(band([]));
    const needle = lane(3).querySelector<HTMLElement>('[data-now]')!;
    expect(parseFloat(needle.style.insetInlineStart)).toBe(50);
    expect(needle.textContent).toBe('12:00');
    expect(host.querySelectorAll('[data-now]')).toHaveLength(1);
  });

  it('draws a schedule that is switched off faintly', async () => {
    await draw(
      band([
        { days: [1], start: '06:00', end: '12:00', action: 'pause', disabled: true },
        { days: [2], start: '06:00', end: '12:00', action: 'pause' },
      ]),
    );
    expect(lane(1).querySelector('[data-entry]')!.className).toContain('opacity-40');
    expect(lane(2).querySelector('[data-entry]')!.className).not.toContain('opacity-40');
  });
});

describe('the reconnect scene', () => {
  const glyph = <IconPlay width={16} height={16} />;

  it('stands grey, with no address, while reconnect is off', async () => {
    await draw(<ReconnectScene method="none" glyph={undefined} off />);
    const scene = host.querySelector('[role="img"]')!;
    expect(scene.getAttribute('data-state')).toBe('off');
    expect(scene.getAttribute('aria-label')).toContain('Reconnect is off');
    expect(scene.querySelector('[stroke="var(--accent-ink)"]')).toBeNull();
    expect(scene.textContent).not.toContain('address');
  });

  it('holds a new address once a method is chosen', async () => {
    await draw(<ReconnectScene method="command" glyph={glyph} off={false} />);
    const scene = host.querySelector('[role="img"]')!;
    expect(scene.getAttribute('data-state')).toBe('on');
    expect(scene.querySelector('[stroke="var(--accent-ink)"]')).not.toBeNull();
    expect(scene.textContent).toContain('New address');
  });

  it('shows the address the last run came back with', async () => {
    await draw(<ReconnectScene method="command" glyph={glyph} off={false} from="198.51.100.7" to="203.0.113.9" />);
    expect(host.textContent).toContain('203.0.113.9');
    expect(host.textContent).not.toContain('New address');
  });
});

describe('the seed ring', () => {
  const around = 2 * Math.PI * 27;
  const arc = () => host.querySelector('circle[stroke-linecap="round"]');
  const drawn = () => Number(arc()!.getAttribute('stroke-dasharray')!.split(' ')[0]);

  it('closes by the share of the target that is seeded', async () => {
    await draw(<SeedRing ratio={0.5} target={2} label="Ratio" />);
    expect(drawn()).toBeCloseTo(around / 4, 1);
    expect(arc()!.getAttribute('stroke')).toBe('var(--accent-ink)');
    expect(host.querySelector('text')!.textContent).toBe('0.50');
  });

  it('is closed and green once the target is reached', async () => {
    await draw(<SeedRing ratio={2.4} target={2} label="Ratio" />);
    expect(drawn()).toBeCloseTo(around, 1);
    expect(arc()!.getAttribute('stroke')).toBe('var(--status-ok-solid)');
  });

  it('stays an open track when no target is set', async () => {
    await draw(<SeedGauge ratio={1.2} target={0} />);
    expect(arc()).toBeNull();
    expect(host.querySelector('[role="progressbar"]')).toBeNull();
    expect(host.textContent).toContain('No seed target');
  });

  it('names the target it measures against', async () => {
    await draw(<SeedGauge ratio={1.2} target={2} />);
    expect(host.textContent).toContain('Target 2.00');
  });
});

describe('the motion gate', () => {
  const steps: AfterDownloadSteps = { verify: true, repair: false, unpack: true, move: false, cleanup: true };
  const moving = () => host.querySelectorAll('animate, animateMotion, animateTransform').length;

  let paused: number;
  let resumed: number;
  let comeIntoView: () => void;

  beforeEach(() => {
    paused = 0;
    resumed = 0;
    comeIntoView = () => {};
    // jsdom has no SMIL clock.
    Object.assign(SVGSVGElement.prototype, {
      pauseAnimations: () => void paused++,
      unpauseAnimations: () => void resumed++,
      setCurrentTime: () => {},
    });
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        constructor(seen: (entries: { isIntersecting: boolean }[]) => void) {
          comeIntoView = () => seen([{ isIntersecting: true }]);
        }
        observe() {}
        disconnect() {}
      },
    );
  });

  it('shows a still frame when the system asks for less motion', async () => {
    await draw(<AfterDownload title="After a download" steps={steps} />);
    expect(moving()).toBe(0);
    expect(paused).toBe(0);
  });

  it('shows a still frame at the motion level off, and moves again when the level is raised', async () => {
    reducedMotion(false);
    document.documentElement.dataset.motion = 'off';
    await draw(<Vessel share={0.5} label="Used" />);
    expect(moving()).toBe(0);

    await act(async () => {
      document.documentElement.dataset.motion = 'subtle';
      await Promise.resolve();
    });
    expect(moving()).toBeGreaterThan(0);
  });

  it('holds a scene on its first frame until it is on screen', async () => {
    reducedMotion(false);
    await draw(<SeedRing ratio={1} target={2} label="Ratio" />);
    expect(moving()).toBeGreaterThan(0);
    expect([paused, resumed]).toEqual([1, 0]);
    comeIntoView();
    expect(resumed).toBe(1);
  });

  it('sends the request to the router with the glyph of the chosen method', async () => {
    reducedMotion(false);
    await draw(<ReconnectScene method="command" glyph={<IconPlay width={16} height={16} data-method="command" />} off={false} />);
    const request = host.querySelector('[data-method="command"]')!;
    expect(request.closest('g[opacity="0"]')!.querySelector('animateMotion')).not.toBeNull();
    // While the scene plays, the old address is on the line first.
    expect(host.textContent).toContain('Old address');
  });

  it('gives a station that is off no answer and no stop', async () => {
    reducedMotion(false);
    await draw(<AfterDownload title="After a download" steps={steps} />);
    const row = host.querySelector('svg')!;
    expect(row.querySelector('[data-station="repair"]')!.querySelectorAll('animate, animateTransform')).toHaveLength(0);
    expect(row.querySelector('[data-station="verify"]')!.querySelectorAll('animate, animateTransform').length).toBeGreaterThan(0);
  });
});
