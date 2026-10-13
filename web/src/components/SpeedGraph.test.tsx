// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { fmtSpeed } from '../lib/format';
import { I18nProvider } from '../lib/i18n';
import { DOWNLOAD_INK, SEEDING_INK } from '../lib/speedLines';
import { ceilingOf, labelTops, limitMark, meterWindow, Plot, spanLabel, SpeedLegend } from './SpeedGraph';

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

  it('counts a limit above the scale as out of it and puts it on the top edge', () => {
    const m = limitMark(100 * MIB, MIB, top, base);
    expect(m.over).toBe(true);
    expect(m.y).toBe(top + 0.5);
  });
});

describe('ceilingOf with both limits', () => {
  it('takes in the higher of two limits within reach', () => {
    expect(ceilingOf([MIB], 2 * MIB, 3 * MIB)).toBeGreaterThan(3 * MIB);
  });

  it('leaves out a far limit and keeps the near one', () => {
    expect(ceilingOf([MIB], 2 * MIB, 100 * MIB)).toBeCloseTo(2.4 * MIB);
  });
});

describe('labelTops', () => {
  it('puts a figure over its line where there is room and under it where there is not', () => {
    expect(labelTops([50.5], 100)).toEqual([35.5]);
    expect(labelTops([3.5], 100)).toEqual([6.5]);
  });

  it('moves a second figure clear of the first and keeps it in the box', () => {
    for (const [ys, h] of [
      [[50.5, 52.5], 100],
      [[3.5, 3.5], 100],
      [[40.5, 40.5], 50],
    ] as const) {
      const [a, b] = labelTops(ys, h);
      expect(Math.abs(a - b)).toBeGreaterThan(12);
      expect(Math.min(a, b)).toBeGreaterThanOrEqual(0);
      expect(Math.max(a, b) + 12).toBeLessThanOrEqual(h);
    }
  });

  it('leaves two figures far apart where they are', () => {
    expect(labelTops([20.5, 80.5], 100)).toEqual([5.5, 65.5]);
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
    expect(draw([MIB, 2 * MIB, 2 * MIB], 0)).toBeNull();
    expect(draw([0, 0, 0], 0)).toBeNull();
    expect(mark('upload')).toBeNull();
  });

  it('draws the download limit as a red line with its figure at the right edge in the download colour', () => {
    const g = draw([MIB, 2 * MIB, 2 * MIB, 2 * MIB], 2 * MIB);
    expect(g).not.toBeNull();
    expect(g!.querySelector('text')!.textContent).toBe(fmtSpeed(2 * MIB));
    expect(g!.querySelector('text')!.getAttribute('fill')).toBe(DOWNLOAD_INK);
    expect(g!.querySelector('text')!.getAttribute('text-anchor')).toBe('end');
    expect(g!.querySelector('title')!.textContent).toBe(`Download limit ${fmtSpeed(2 * MIB)}`);
    expect(offset(g!)).toBeGreaterThan(3);
    expect(offset(g!)).toBeLessThan(97);
    const line = g!.querySelector('line')!;
    expect(line.getAttribute('stroke')).toBe('var(--status-fail-solid)');
    expect(line.hasAttribute('stroke-dasharray')).toBe(false);
    expect(line.hasAttribute('visibility')).toBe(false);
  });

  it('draws the upload limit the same way in the seeding colour', () => {
    draw([MIB, 2 * MIB, 2 * MIB], 0, [MIB, MIB, MIB], MIB);
    const g = mark('upload')!;
    expect(g.querySelector('text')!.getAttribute('fill')).toBe(SEEDING_INK);
    expect(g.querySelector('path')!.getAttribute('fill')).toBe(SEEDING_INK);
    expect(g.querySelector('title')!.textContent).toBe(`Upload limit ${fmtSpeed(MIB)}`);
    expect(offset(g)).toBeGreaterThan(3);
    expect(g.querySelector('line')!.hasAttribute('visibility')).toBe(false);
  });

  it('draws both limits while idle', () => {
    draw([0, 0, 0], 5 * MIB, [0, 0, 0], MIB);
    expect(offset(mark('download')!)).toBeGreaterThan(3);
    expect(offset(mark('upload')!)).toBeGreaterThan(offset(mark('download')!));
  });

  it('puts a limit the scale left out on the top edge with a chevron and no line', () => {
    const g = draw([100 * 1024, 200 * 1024, 200 * 1024], 100 * MIB);
    expect(offset(g!)).toBe(3.5);
    expect(g!.querySelector('line')!.getAttribute('visibility')).toBe('hidden');
    const inside = draw([MIB, 2 * MIB, 2 * MIB], 2 * MIB)!.querySelector('path')!.getAttribute('d');
    const over = draw([100 * 1024, 200 * 1024, 200 * 1024], 100 * MIB)!.querySelector('path')!.getAttribute('d');
    expect(over).not.toBe(inside);
    expect(over).toMatch(/^M-8 4L-4 0L0 4Z$/);
  });

  it('keeps two figures apart where the limits meet', () => {
    draw([100 * 1024, 200 * 1024, 200 * 1024], 100 * MIB, [100 * 1024, 100 * 1024, 100 * 1024], 90 * MIB);
    const down = mark('download')!;
    const up = mark('upload')!;
    expect(offset(down)).toBe(offset(up));
    expect(Math.abs(figureTop(down) - figureTop(up))).toBeGreaterThan(12);
  });

  it('takes the mark away when the limit is lifted', () => {
    draw([MIB, 2 * MIB, 2 * MIB], 2 * MIB, [0, 0, 0], MIB);
    expect(draw([MIB, 2 * MIB, 2 * MIB], 0)).toBeNull();
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
    legend(100 * MIB, 4 * MIB);
    expect(host.querySelector('[data-speed-limit]')).toBeNull();
    expect(host.querySelector('.sr-only')!.textContent).toBe(
      `Download limit ${fmtSpeed(100 * MIB)}, Upload limit ${fmtSpeed(4 * MIB)}`,
    );
    legend(0, 0);
    expect(host.querySelector('.sr-only')).toBeNull();
  });

  const uploadLine = () => host.querySelector('[data-upload-line]');

  it('draws the upload line along the ground while nothing is sent', () => {
    draw([MIB, 2 * MIB, 2 * MIB], 0);
    expect(uploadLine()!.getAttribute('d')).toMatch(/^M[\d.]+,97\.00C/);
  });

  it('draws the upload line on an idle box too, with no area under it', () => {
    draw([0, 0, 0], 0);
    expect(host.querySelector('.kl-curve-line')).toBeNull();
    expect(uploadLine()!.getAttribute('stroke')).toBe(SEEDING_INK);
    expect(host.querySelector('[data-upload-area]')).toBeNull();
  });

  it('draws the upload as a line of its own colour beside the download', () => {
    draw([MIB, 2 * MIB, 2 * MIB], 0, [MIB / 2, MIB / 2, MIB]);
    const line = uploadLine();
    expect(line).not.toBeNull();
    expect(line!.getAttribute('stroke')).toBe(SEEDING_INK);
    expect(line!.getAttribute('fill')).toBe('none');
    expect(line!.getAttribute('d')).not.toBe(host.querySelector('.kl-curve-line')!.getAttribute('d'));
  });

  it('fills the area under the upload in the seeding colour, over the download area', () => {
    draw([MIB, 2 * MIB, 2 * MIB], 0, [MIB / 2, MIB / 2, MIB]);
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
    draw([0, 0, 0], 0, [MIB, MIB, MIB]);
    expect(uploadLine()).not.toBeNull();
  });
});

