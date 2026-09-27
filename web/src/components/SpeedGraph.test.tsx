// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { fmtSpeed } from '../lib/format';
import { ceilingOf, limitMark, meterWindow, Plot, spanLabel } from './SpeedGraph';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const MIB = 1024 * 1024;

describe('spanLabel', () => {
  it('writes the span in the symbols the window field writes', () => {
    expect(spanLabel(30)).toBe('-30s');
    expect(spanLabel(90)).toBe('-1.5min');
    expect(spanLabel(3600)).toBe('-1h');
  });
});

describe('meterWindow', () => {
  it('keeps whole seconds below a minute and half minutes from there, within the hour', () => {
    expect([5, 45.4, 59.6, 75, 100, 125, 4000].map(meterWindow)).toEqual([10, 45, 60, 90, 90, 120, 3600]);
  });

  it('gives the coarse ring a whole number of its ten-second steps past two minutes', () => {
    for (let s = 121; s <= 3600; s += 7) expect(meterWindow(s) % 10).toBe(0);
  });
});

describe('ceilingOf', () => {
  it('is the peak without a limit', () => {
    expect(ceilingOf([1 * MIB, 3 * MIB, 2 * MIB])).toBe(3 * MIB);
    expect(ceilingOf([1 * MIB, 3 * MIB], 0)).toBe(3 * MIB);
  });

  it('takes in a limit above the peak with room over it', () => {
    const top = ceilingOf([4 * MIB, 5 * MIB], 5 * MIB);
    expect(top).toBeGreaterThan(5 * MIB);
    expect(top).toBeLessThan(7 * MIB);
  });

  it('stays on a peak that already runs above the limit', () => {
    expect(ceilingOf([1 * MIB, 9 * MIB], 2 * MIB)).toBe(9 * MIB);
  });

  it('leaves a limit far above a slow download out of the scale', () => {
    expect(ceilingOf([200 * 1024, 300 * 1024], 100 * MIB)).toBe(300 * 1024);
  });

  it('lets the limit set the scale while idle', () => {
    expect(ceilingOf([0, 0, 0], 100 * MIB)).toBeGreaterThan(100 * MIB);
  });
});

describe('limitMark', () => {
  const top = 3;
  const base = 97;

  it('sits inside the plot, on a half pixel, when the scale takes the limit in', () => {
    const limit = 5 * MIB;
    const m = limitMark(limit, ceilingOf([limit], limit), top, base);
    expect(m.over).toBe(false);
    expect(m.y).toBeGreaterThan(top);
    expect(m.y).toBeLessThan(base);
    expect(m.y % 1).toBe(0.5);
  });

  it('is pinned to the top edge for a limit above the scale', () => {
    const m = limitMark(100 * MIB, MIB, top, base);
    expect(m.over).toBe(true);
    expect(m.y).toBe(top + 0.5);
    expect(m.labelBelow).toBe(true);
  });

  it('puts the figure over the line where there is room for it', () => {
    expect(limitMark(MIB, 2 * MIB, top, base).labelBelow).toBe(false);
  });
});

describe('Plot', () => {
  let root: Root;
  let host: HTMLDivElement;

  beforeEach(() => {
    // Reduced motion, so the curve is placed once and nothing glides.
    vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }));
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
  });

  afterEach(() => {
    act(() => root.unmount());
    host.remove();
    vi.unstubAllGlobals();
  });

  function draw(samples: number[], limit: number) {
    const ceiling = ceilingOf(samples, limit);
    act(() =>
      root.render(
        <svg viewBox="0 0 300 100">
          <Plot
            win={{ samples, step: 1, newestAt: 0 }}
            span={samples.length}
            w={300}
            h={100}
            pad={3}
            ceiling={ceiling}
            stroke={1.5}
            limit={limit}
          />
        </svg>,
      ),
    );
    return host.querySelector<SVGGElement>('[data-limit-line]');
  }

  function offset(g: SVGGElement): number {
    const m = /translate\(0 ([\d.]+)\)/.exec(g.getAttribute('transform') ?? '');
    return m ? Number(m[1]) : NaN;
  }

  it('draws no limit line without a limit', () => {
    expect(draw([MIB, 2 * MIB, 2 * MIB], 0)).toBeNull();
    expect(draw([0, 0, 0], 0)).toBeNull();
  });

  it('draws the limit with its figure under a running download', () => {
    const line = draw([MIB, 2 * MIB, 2 * MIB, 2 * MIB], 2 * MIB);
    expect(line).not.toBeNull();
    expect(line!.textContent).toBe(fmtSpeed(2 * MIB));
    expect(offset(line!)).toBeGreaterThan(3);
    expect(offset(line!)).toBeLessThan(97);
    expect(line!.querySelector('line')!.hasAttribute('stroke-dasharray')).toBe(false);
  });

  it('draws the limit while idle', () => {
    const line = draw([0, 0, 0], 5 * MIB);
    expect(line).not.toBeNull();
    expect(offset(line!)).toBeGreaterThan(3);
  });

  it('dashes a limit the scale left out at the top edge', () => {
    const line = draw([100 * 1024, 200 * 1024, 200 * 1024], 100 * MIB);
    expect(line).not.toBeNull();
    expect(offset(line!)).toBe(3.5);
    expect(line!.querySelector('line')!.hasAttribute('stroke-dasharray')).toBe(true);
  });

  it('takes the line away when the limit is lifted', () => {
    draw([MIB, 2 * MIB, 2 * MIB], 2 * MIB);
    expect(draw([MIB, 2 * MIB, 2 * MIB], 0)).toBeNull();
  });
});
