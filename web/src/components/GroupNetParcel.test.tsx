// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { GroupNet, PARCEL_REST_MS, netLayout, type NetNode, type NetParcel } from './GroupNet';
import { TRAVEL } from './pictures/scene';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  delete document.documentElement.dataset.motion;
  delete (Element.prototype as { animate?: unknown }).animate;
  vi.useRealTimers();
});

const nodes: NetNode[] = [
  { key: 'member:laptop', deployment: 'desktop', name: 'Laptop', sub: 'Direct' },
  { key: 'member:nas', deployment: 'container', name: 'NAS', sub: 'Via relay', relay: true },
];

function draw(parcels: NetParcel[] = [], onDelivered: (id: number) => void = () => {}) {
  act(() =>
    root.render(
      <GroupNet
        self={{ name: 'tower', sub: '' }}
        nodes={nodes}
        parcels={parcels}
        onDelivered={onDelivered}
        hot={null}
        onPick={() => {}}
        alt="Your group"
      />,
    ),
  );
}

const spoke = (key: string) => host.querySelector(`svg.wide g[data-nk="${key}"]`)!;

interface Run {
  el: Element;
  frames: { transform?: string }[];
  duration: number;
  onfinish: (() => void) | null;
  cancel: () => void;
}

/** Stands in for the Web Animations API, which jsdom has none of, and keeps what was asked of it. */
function recordAnimations(): Run[] {
  const runs: Run[] = [];
  (Element.prototype as { animate?: unknown }).animate = function (this: Element, frames: Run['frames'], timing: { duration: number }) {
    const run: Run = { el: this, frames, duration: timing.duration, onfinish: null, cancel: () => {} };
    runs.push(run);
    return run;
  };
  return runs;
}

/** The two ends of the second node's line, as the picture draws them. */
function ends() {
  const { hub, points } = netLayout('wide', nodes.length);
  const p = points[1];
  const length = Math.hypot(p.x - hub.x, p.y - hub.y);
  const ux = (p.x - hub.x) / length;
  const uy = (p.y - hub.y) / length;
  return {
    here: { x: hub.x + ux * 39, y: hub.y + uy * 39 },
    there: { x: p.x - ux * 32, y: p.y - uy * 32 },
    length: length - 39 - 32,
  };
}

const place = (p: { x: number; y: number }) => `translate(${p.x}px, ${p.y}px)`;

describe('the lines of the net', () => {
  it('draws a line through the relay dashed and a direct one solid', () => {
    draw();
    expect(spoke('member:nas').querySelector('.kl-net-link')!.classList.contains('relay')).toBe(true);
    expect(spoke('member:laptop').querySelector('.kl-net-link')!.classList.contains('relay')).toBe(false);
  });
});

describe('drawing the net in', () => {
  /** An IntersectionObserver that reports only when the test says so. */
  function watchSight() {
    const said: ((entries: { isIntersecting: boolean }[]) => void)[] = [];
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        constructor(report: (entries: { isIntersecting: boolean }[]) => void) {
          said.push(report);
        }
        observe() {}
        disconnect() {}
      },
    );
    return (...sight: boolean[]) => act(() => said.forEach((report) => report(sight.map((isIntersecting) => ({ isIntersecting })))));
  }

  afterEach(() => vi.unstubAllGlobals());

  const hiding = (runs: Run[]) => runs.filter((r) => r.el.closest('.kl-net-spoke:not(.hub)'));

  it('hides no node while the picture waits to come into sight', () => {
    const runs = recordAnimations();
    watchSight();
    draw();
    expect(hiding(runs)).toEqual([]);
  });

  it('draws the nodes in once the picture is in sight, also when the same report says it left first', () => {
    const runs = recordAnimations();
    const report = watchSight();
    draw();
    report(false, true);
    const nodes = hiding(runs).filter((r) => r.el.classList.contains('kl-net-node'));
    // Each node in both shapes of the picture.
    expect(nodes).toHaveLength(4);
  });
});

describe('a handover', () => {
  it('travels along its node’s line at the speed everything in a picture travels at', () => {
    const runs = recordAnimations();
    draw([{ id: 1, key: 'member:nas', out: true }]);
    expect(spoke('member:laptop').querySelector('.kl-net-parcel')).toBeNull();
    const parcel = spoke('member:nas').querySelector('.kl-net-parcel')!;
    const trip = runs.find((r) => parcel.contains(r.el))!;
    const { here, there, length } = ends();
    expect(trip.frames.map((f) => f.transform)).toEqual([place(here), place(there)]);
    expect(trip.duration).toBeCloseTo((length / TRAVEL) * 1000, 3);
  });

  it('runs the other way for links that come in', () => {
    const runs = recordAnimations();
    draw([{ id: 1, key: 'member:nas', out: false }]);
    const parcel = spoke('member:nas').querySelector('.kl-net-parcel')!;
    const { here, there } = ends();
    expect(runs.find((r) => parcel.contains(r.el))!.frames.map((f) => f.transform)).toEqual([place(there), place(here)]);
  });

  it('is answered by the node it reaches and then leaves the picture', () => {
    const runs = recordAnimations();
    const delivered = vi.fn();
    draw([{ id: 7, key: 'member:nas', out: true }], delivered);
    const parcel = spoke('member:nas').querySelector('.kl-net-parcel')!;
    const mine = () => runs.filter((r) => parcel.contains(r.el));
    expect(mine()).toHaveLength(1);
    act(() => mine()[0].onfinish!());
    // The parcel is in sight until it arrives; then the ring around the node plays.
    expect(mine()).toHaveLength(2);
    expect(mine()[1].el.classList.contains('kl-net-answer')).toBe(true);
    expect(delivered).not.toHaveBeenCalled();
    act(() => mine()[1].onfinish!());
    expect(delivered).toHaveBeenCalledWith(7);
  });

  it('stands arrived at its node while nothing may move, and leaves after a moment', () => {
    vi.useFakeTimers();
    const runs = recordAnimations();
    document.documentElement.dataset.motion = 'off';
    const delivered = vi.fn();
    draw([{ id: 3, key: 'member:nas', out: true }], delivered);
    expect(runs).toHaveLength(0);
    const { there } = ends();
    const box = spoke('member:nas').querySelector('.kl-net-parcel > g')!;
    expect(box.getAttribute('transform')).toBe(`translate(${+there.x.toFixed(1)} ${+there.y.toFixed(1)})`);
    act(() => vi.advanceTimersByTime(PARCEL_REST_MS - 1));
    expect(delivered).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(1));
    expect(delivered).toHaveBeenCalledWith(3);
  });
});
