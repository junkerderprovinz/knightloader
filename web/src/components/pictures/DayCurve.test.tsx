// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../../lib/i18n';
import { DayCurve, planOf, todayOf, type DayPoint, type PlanEntry, type PlanStretch } from './DayCurve';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
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

const MB = 1_000_000;
// A Wednesday.
const WED = 3;
const noon = new Date(2026, 9, 7, 12, 0);

describe('the plan of a day', () => {
  const night: PlanEntry = { days: [WED], start: '00:00', end: '06:00', action: 'pause' };
  const work: PlanEntry = { days: [WED], start: '08:00', end: '18:00', action: 'limit', limit: 2 * MB };

  it('is the limit set by hand while no window is open', () => {
    expect(planOf([], WED, MB)).toEqual([{ from: 0, to: 1440, paused: false, limit: MB }]);
  });

  it('cuts the day where a window opens or closes', () => {
    expect(planOf([night, work], WED, 0)).toEqual([
      { from: 0, to: 360, paused: true, limit: 0 },
      { from: 360, to: 480, paused: false, limit: 0 },
      { from: 480, to: 1080, paused: false, limit: 2 * MB },
      { from: 1080, to: 1440, paused: false, limit: 0 },
    ]);
  });

  it('lets the last window win, each action in its own field', () => {
    const lunch: PlanEntry = { days: [WED], start: '12:00', end: '13:00', action: 'limit', limit: 0 };
    const all: PlanEntry = { days: [WED], start: '00:00', end: '23:59', action: 'pause' };
    const open: PlanEntry = { days: [WED], start: '09:00', end: '10:00', action: 'resume' };
    const plan = planOf([all, work, lunch, open], WED, MB);
    const at = (minute: number) => plan.find((s) => s.from <= minute && minute < s.to)!;
    // A limit inside a pause leaves the pause alone, and a resume leaves the limit.
    expect(at(8 * 60 + 30)).toMatchObject({ paused: true, limit: 2 * MB });
    expect(at(9 * 60 + 30)).toMatchObject({ paused: false, limit: 2 * MB });
    // A limit of none over a limit lifts it for its hour.
    expect(at(12 * 60 + 30)).toMatchObject({ paused: true, limit: 0 });
    expect(at(23 * 60 + 59)).toMatchObject({ paused: false, limit: MB });
  });

  it("carries the tail of yesterday's night into the morning", () => {
    const late: PlanEntry = { days: [WED - 1], start: '22:00', end: '06:00', action: 'pause' };
    expect(planOf([late], WED, 0)).toEqual([
      { from: 0, to: 360, paused: true, limit: 0 },
      { from: 360, to: 1440, paused: false, limit: 0 },
    ]);
  });

  it('leaves out a window that is switched off or belongs to another day', () => {
    const plan = planOf([{ ...night, disabled: true }, { ...work, days: [WED + 1] }], WED, 0);
    expect(plan).toEqual([{ from: 0, to: 1440, paused: false, limit: 0 }]);
  });

  it('puts the quiet limit in place of every other while a quiet window is open', () => {
    const hush: PlanEntry = { days: [WED], start: '08:00', end: '09:00', action: 'quiet' };
    const at = (plan: PlanStretch[]) => plan.find((s) => s.from === 480)!.limit;
    expect(at(planOf([work, hush], WED, 0, MB / 2))).toBe(MB / 2);
    // Without a quiet limit the mode changes nothing about speed.
    expect(at(planOf([work, hush], WED, 0))).toBe(2 * MB);
  });
});

describe("the day's record", () => {
  it("stands on today's clock, without what was measured before midnight", () => {
    // Three samples of five minutes, the first of them closing at midnight.
    const from = new Date(2026, 9, 6, 23, 55).toISOString();
    expect(todayOf({ samples: [100, 200, 300], samplesUp: [1, 2, 3], step: 300, from }, noon)).toEqual([
      { at: 5, speed: 200, up: 2 },
      { at: 10, speed: 300, up: 3 },
    ]);
  });

  it('reads a record without upload as nothing sent', () => {
    const from = new Date(2026, 9, 7, 0, 0).toISOString();
    expect(todayOf({ samples: [100, 200], step: 300, from }, noon)).toEqual([
      { at: 5, speed: 100, up: 0 },
      { at: 10, speed: 200, up: 0 },
    ]);
  });

  it('is empty while the server has measured nothing', () => {
    expect(todayOf({ samples: [], step: 300 }, noon)).toEqual([]);
  });
});

describe('the curve of the day', () => {
  const plan: PlanStretch[] = [
    { from: 0, to: 360, paused: true, limit: 0 },
    { from: 360, to: 720, paused: false, limit: 2 * MB },
    { from: 720, to: 1440, paused: false, limit: 0 },
  ];
  const points: DayPoint[] = [
    { at: 365, speed: MB },
    { at: 370, speed: 4 * MB },
    { at: 600, speed: 2 * MB },
  ];
  const draw = (over: Partial<Parameters<typeof DayCurve>[0]> = {}) =>
    act(async () =>
      root.render(
        <I18nProvider>
          <DayCurve plan={plan} points={points} now={noon} dayLabel="Wed" label="Schedules, Wed" {...over} />
        </I18nProvider>,
      ),
    );

  it('draws the planned limit as an area over its hours, as high as the limit', async () => {
    await draw();
    const areas = host.querySelectorAll('[data-plan="limit"]');
    expect(areas).toHaveLength(1);
    expect([areas[0].getAttribute('data-from'), areas[0].getAttribute('data-to')]).toEqual(['360', '720']);
    // The tallest reading, 4 MB/s, is the top of the plot, so 2 MB/s stands at half its height.
    expect(areas[0].querySelector('path')!.getAttribute('d')).toBe('M360 100V50.00H720V100z');
    expect(host.querySelector('[data-ceiling]')!.textContent).toContain('4');
  });

  it('draws a pause as a bar on the ground', async () => {
    await draw();
    const pause = host.querySelector<HTMLElement>('[data-plan="pause"]')!;
    expect(parseFloat(pause.style.insetInlineStart)).toBe(0);
    expect(parseFloat(pause.style.width)).toBe(25);
  });

  it('draws the measured speed as a line that steps from one sample to the next and breaks where none was taken', async () => {
    await draw();
    expect(host.querySelector('[data-measured]')!.getAttribute('d')).toBe(
      'M360.0 75.00H365.0L365.0 0.00H370.0M595.0 50.00H600.0',
    );
  });

  it('puts a hand on the present moment', async () => {
    await draw();
    expect(parseFloat(host.querySelector<HTMLElement>('[data-now]')!.style.insetInlineStart)).toBe(50);
  });

  it('says when the record begins, unless it holds the whole day', async () => {
    await draw();
    expect(host.querySelector('[data-since]')!.textContent).toBe('Measured since 06:00');
    await draw({ points: [{ at: 5, speed: MB }] });
    expect(host.querySelector('[data-since]')).toBeNull();
  });

  it('draws no line it has not measured', async () => {
    await draw({ points: [] });
    expect(host.querySelector('[data-measured]')).toBeNull();
    expect(host.querySelector('[data-since]')!.textContent).toBe('Measured since 12:00');
  });

  it('says in words what the day asks for and what was measured last', async () => {
    await draw();
    expect(host.querySelector('[role="img"]')!.getAttribute('aria-label')).toMatch(
      /^Schedules, Wed: 00:00-06:00 Pause, 06:00-12:00 .*2.*MB\/s.*, Speed 10:00: .*2.*MB\/s/,
    );
  });

  it('draws the upload beside the speed and names it, along the ground on a day without any', async () => {
    await draw();
    expect(host.querySelector('[data-upload]')!.getAttribute('d')).toBe(
      'M360.0 100.00H365.0L365.0 100.00H370.0M595.0 100.00H600.0',
    );
    expect(host.querySelector('[data-upload-key]')!.textContent).toBe('Seeding');

    await draw({ points: points.map((p) => ({ ...p, up: MB })) });
    expect(host.querySelector('[data-upload]')!.getAttribute('d')).toBe(
      'M360.0 75.00H365.0L365.0 75.00H370.0M595.0 75.00H600.0',
    );
    expect(host.querySelector('[data-upload-key]')!.textContent).toBe('Seeding');
  });

  it('draws the plan of a timetable that is set aside faintly, and the measured line as ever', async () => {
    await draw({ quiet: true });
    expect(host.querySelector('[data-plan="limit"]')!.parentElement!.getAttribute('class')).toContain('opacity-40');
    expect(host.querySelector('[data-plan="pause"]')!.className).toContain('opacity-40');
    expect(host.querySelector('[data-measured]')!.closest('.opacity-40')).toBeNull();
  });
});
