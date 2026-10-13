import { describe, expect, it } from 'vitest';
import { appNodeKind, netLayout } from './GroupNet';

describe('netLayout', () => {
  it('puts a single other node to the right of this instance', () => {
    const { hub, points, width } = netLayout('wide', 1);
    expect(points).toHaveLength(1);
    expect(points[0].x).toBeGreaterThan(hub.x);
    expect(points[0].y).toBe(hub.y);
    // The name stands on the outside and ends inside the box.
    const [x, , w, align] = points[0].label;
    expect(align).toBe('start');
    expect(x + w).toBeLessThanOrEqual(width);
  });

  it('stacks the others on both sides of a wide box, more of them on the right', () => {
    const { hub, points, height } = netLayout('wide', 5);
    const right = points.filter((p) => p.x > hub.x);
    const left = points.filter((p) => p.x < hub.x);
    expect([right.length, left.length]).toEqual([3, 2]);
    expect(left.every((p) => p.label[3] === 'end')).toBe(true);
    for (const p of points) {
      expect(p.y).toBeGreaterThan(0);
      expect(p.y).toBeLessThan(height);
    }
    expect(hub.y).toBe(height / 2);
  });

  it('keeps every other node on one side of a narrow box and grows with them', () => {
    const three = netLayout('narrow', 3);
    expect(three.points.every((p) => p.x > three.hub.x)).toBe(true);
    expect(netLayout('narrow', 6).height).toBeGreaterThan(three.height);
    expect(three.width).toBeLessThan(netLayout('wide', 3).width);
  });

  it('never lets two nodes of one side share a row', () => {
    for (const shape of ['wide', 'narrow'] as const) {
      const { hub, points } = netLayout(shape, 8);
      for (const side of [-1, 1]) {
        const rows = points.filter((p) => Math.sign(p.x - hub.x) === side).map((p) => p.y);
        expect(new Set(rows).size).toBe(rows.length);
      }
    }
  });
});

describe('appNodeKind', () => {
  it('draws an extension as a browser and everything else as a phone', () => {
    expect(appNodeKind('extension')).toBe('browser');
    expect(appNodeKind('mobile')).toBe('phone');
  });
});
