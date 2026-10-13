// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { fmtSpeed } from '../lib/format';
import { I18nProvider } from '../lib/i18n';
import { DOWNLOAD_INK, SEEDING_INK } from '../lib/speedLines';
import { ceilingOf, labelTops, limitMark, meterWindow, Plot, spanLabel, SpeedLegend } from './SpeedGraph';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const MB = 1_000_000;

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
    expect(ceilingOf([1 * MB, 3 * MB, 2 * MB])).toBe(3 * MB);
    expect(ceilingOf([1 * MB, 3 * MB], 0)).toBe(3 * MB);
  });

  it('takes in a limit above the peak with room over it', () => {
    const top = ceilingOf([4 * MB, 5 * MB], 5 * MB);
    expect(top).toBeGreaterThan(5 * MB);
    expect(top).toBeLessThan(7 * MB);
  });

  it('stays on a peak that already runs above the limit', () => {
    expect(ceilingOf([1 * MB, 9 * MB], 2 * MB)).toBe(9 * MB);
  });

  it('leaves a limit far above a slow download out of the scale', () => {
    expect(ceilingOf([200 * 1000, 300 * 1000], 100 * MB)).toBe(300 * 1000);
  });

  it('lets the limit set the scale while idle', () => {
    expect(ceilingOf([0, 0, 0], 100 * MB)).toBeGreaterThan(100 * MB);
  });
});

describe('limitMark', () => {
  const top = 3;
  const base = 97;

  it('sits inside the plot, on a whole pixel, when the scale takes the limit in', () => {
    const limit = 5 * MB;
    const m = limitMark(limit, ceilingOf([limit], limit), top, base);
    expect(m.over).toBe(false);
    expect(m.y).toBeGreaterThan(top);
    expect(m.y).toBeLessThan(base);
    expect(m.y % 1).toBe(0);
  });

  it('counts a limit above the scale as out of it and puts it on the top edge', () => {
    const m = limitMark(100 * MB, MB, top, base);
    expect(m.over).toBe(true);
    expect(m.y).toBe(top);
  });
});

describe('ceilingOf with both limits', () => {
  it('takes in the higher of two limits within reach', () => {
    expect(ceilingOf([MB], 2 * MB, 3 * MB)).toBeGreaterThan(3 * MB);
  });

  it('leaves out a far limit and keeps the near one', () => {
    expect(ceilingOf([MB], 2 * MB, 100 * MB)).toBeCloseTo(2.4 * MB);
  });
});

describe('labelTops', () => {
  it('puts a figure over its mark where there is room and under it where there is not', () => {
    expect(labelTops([50], 100)).toEqual([35]);
    expect(labelTops([3], 100)).toEqual([6]);
  });

  it('moves a second figure clear of the first and keeps it in the box', () => {
    for (const [ys, h] of [
      [[50, 52], 100],
      [[3, 3], 100],
      [[40, 40], 50],
    ] as const) {
      const [a, b] = labelTops(ys, h);
      expect(Math.abs(a - b)).toBeGreaterThan(12);
      expect(Math.min(a, b)).toBeGreaterThanOrEqual(0);
      expect(Math.max(a, b) + 12).toBeLessThanOrEqual(h);
    }
  });

  it('leaves two figures far apart where they are', () => {
    expect(labelTops([20, 80], 100)).toEqual([5, 65]);
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

  function draw(samples: number[], limit: number, upload = samples.map(() => 0), uploadLimit = 0) {
    const ceiling = ceilingOf(samples.concat(upload), limit, uploadLimit);
    act(() =>
      root.render(
        <I18nProvider>
          <svg viewBox="0 0 300 100">
            <Plot
              win={{ samples, upload, step: 1, newestAt: 0 }}
              span={samples.length}
              w={300}
              h={100}
              pad={3}
              ceiling={ceiling}
              stroke={1.5}
              limits={{ download: limit, upload: uploadLimit }}
            />
          </svg>
        </I18nProvider>,
      ),
    );
    return mark('download');
  }

  const mark = (kind: 'download' | 'upload') => host.querySelector<SVGGElement>(`[data-limit-mark="${kind}"]`);

  function offset(g: SVGGElement): number {
    const m = /translate\(0 ([\d.]+)\)/.exec(g.getAttribute('transform') ?? '');
    return m ? Number(m[1]) : NaN;
  }

  const figureTop = (g: SVGGElement) => offset(g) + Number(g.querySelector('text')!.getAttribute('y')) - 12;

  it('draws no limit mark without a limit', () => {
    expect(draw([MB, 2 * MB, 2 * MB], 0)).toBeNull();
    expect(draw([0, 0, 0], 0)).toBeNull();
    expect(mark('upload')).toBeNull();
  });

  it('marks the download limit at the right edge in the download colour, with no line across the plot', () => {
    const g = draw([MB, 2 * MB, 2 * MB, 2 * MB], 2 * MB);
    expect(g).not.toBeNull();
    expect(g!.querySelector('text')!.textContent).toBe(fmtSpeed(2 * MB));
    expect(g!.querySelector('text')!.getAttribute('fill')).toBe(DOWNLOAD_INK);
    expect(g!.querySelector('text')!.getAttribute('text-anchor')).toBe('end');
    expect(g!.querySelector('title')!.textContent).toBe(`Download limit ${fmtSpeed(2 * MB)}`);
    expect(offset(g!)).toBeGreaterThan(3);
    expect(offset(g!)).toBeLessThan(97);
    expect(g!.querySelector('path')!.getAttribute('d')).toBe('M0 -4L-4 0L0 4Z');
    expect(g!.querySelector('path')!.getAttribute('fill')).toBe(DOWNLOAD_INK);
    expect(g!.querySelector('line')).toBeNull();
    expect(host.querySelector('[stroke="var(--status-fail-solid)"]')).toBeNull();
  });

  it('draws the upload limit the same way in the seeding colour', () => {
    draw([MB, 2 * MB, 2 * MB], 0, [MB, MB, MB], MB);
    const g = mark('upload')!;
    expect(g.querySelector('text')!.getAttribute('fill')).toBe(SEEDING_INK);
    expect(g.querySelector('path')!.getAttribute('fill')).toBe(SEEDING_INK);
    expect(g.querySelector('title')!.textContent).toBe(`Upload limit ${fmtSpeed(MB)}`);
    expect(offset(g)).toBeGreaterThan(3);
    expect(g.querySelector('line')).toBeNull();
  });

  it('draws both limits while idle', () => {
    draw([0, 0, 0], 5 * MB, [0, 0, 0], MB);
    expect(offset(mark('download')!)).toBeGreaterThan(3);
    expect(offset(mark('upload')!)).toBeGreaterThan(offset(mark('download')!));
  });

  it('puts a limit the scale left out on the top edge with a chevron', () => {
    const g = draw([100 * 1000, 200 * 1000, 200 * 1000], 100 * MB);
    expect(offset(g!)).toBe(3);
    expect(g!.querySelector('line')).toBeNull();
    const inside = draw([MB, 2 * MB, 2 * MB], 2 * MB)!.querySelector('path')!.getAttribute('d');
    const over = draw([100 * 1000, 200 * 1000, 200 * 1000], 100 * MB)!.querySelector('path')!.getAttribute('d');
    expect(over).not.toBe(inside);
    expect(over).toMatch(/^M-8 4L-4 0L0 4Z$/);
  });

  it('keeps two figures apart where the limits meet', () => {
    draw([100 * 1000, 200 * 1000, 200 * 1000], 100 * MB, [100 * 1000, 100 * 1000, 100 * 1000], 90 * MB);
    const down = mark('download')!;
    const up = mark('upload')!;
    expect(offset(down)).toBe(offset(up));
    expect(Math.abs(figureTop(down) - figureTop(up))).toBeGreaterThan(12);
  });

  it('takes the mark away when the limit is lifted', () => {
    draw([MB, 2 * MB, 2 * MB], 2 * MB, [0, 0, 0], MB);
    expect(draw([MB, 2 * MB, 2 * MB], 0)).toBeNull();
    expect(mark('upload')).toBeNull();
  });

  it('names no limit in the legend and reads the limits out to a screen reader', () => {
    const legend = (download: number, upload: number) =>
      act(() =>
        root.render(
          <I18nProvider>
            <SpeedLegend limits={{ download, upload }} />
          </I18nProvider>,
        ),
      );
    legend(100 * MB, 4 * MB);
    expect(host.querySelector('[data-speed-limit]')).toBeNull();
    expect(host.querySelector('.sr-only')!.textContent).toBe(
      `Download limit ${fmtSpeed(100 * MB)}, Upload limit ${fmtSpeed(4 * MB)}`,
    );
    legend(0, 0);
    expect(host.querySelector('.sr-only')).toBeNull();
  });

  const uploadLine = () => host.querySelector('[data-upload-line]');

  it('draws the upload line along the ground while nothing is sent', () => {
    draw([MB, 2 * MB, 2 * MB], 0);
    expect(uploadLine()!.getAttribute('d')).toMatch(/^M[\d.]+,97\.00C/);
  });

  it('draws the upload line on an idle box too, with no area under it', () => {
    draw([0, 0, 0], 0);
    expect(host.querySelector('.kl-curve-line')).toBeNull();
    expect(uploadLine()!.getAttribute('stroke')).toBe(SEEDING_INK);
    expect(host.querySelector('[data-upload-area]')).toBeNull();
  });

  it('draws the upload as a line of its own colour beside the download', () => {
    draw([MB, 2 * MB, 2 * MB], 0, [MB / 2, MB / 2, MB]);
    const line = uploadLine();
    expect(line).not.toBeNull();
    expect(line!.getAttribute('stroke')).toBe(SEEDING_INK);
    expect(line!.getAttribute('fill')).toBe('none');
    expect(line!.getAttribute('d')).not.toBe(host.querySelector('.kl-curve-line')!.getAttribute('d'));
  });

  it('fills the area under the upload in the seeding colour, over the download area', () => {
    draw([MB, 2 * MB, 2 * MB], 0, [MB / 2, MB / 2, MB]);
    const area = host.querySelector('[data-upload-area]')!;
    expect(area.getAttribute('d')).toMatch(/^M.*C.*L[\d.]+,97\.00L[\d.]+,97\.00Z$/);
    const fill = /^url\(#(.+)\)$/.exec(area.getAttribute('fill')!)![1];
    const stops = [...host.querySelectorAll(`[id="${fill}"] stop`)];
    expect(stops.map((s) => s.getAttribute('stop-color'))).toEqual([SEEDING_INK, SEEDING_INK]);
    // Painted after the download's area, so it lies on top where the two overlap.
    const download = host.querySelector(`path[fill^="url"]:not([data-upload-area])`)!;
    expect(download.compareDocumentPosition(area) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('draws a seeding box that downloads nothing', () => {
    draw([0, 0, 0], 0, [MB, MB, MB]);
    expect(uploadLine()).not.toBeNull();
  });
});

