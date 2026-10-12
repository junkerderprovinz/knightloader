// @vitest-environment jsdom
import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Account, CatalogueService, Settings } from '../../lib/api';
import { I18nProvider } from '../../lib/i18n';
import { ToastProvider } from '../../lib/toast';
import { CaptchaCards } from '../../pages/settings/accounts/CaptchaCards';
import { SettingsProvider, type SettingsDraft } from '../../pages/settings/context';
import { SelfTestPulseCard } from '../../pages/settings/health/SelfTestPulse';
import { Rules } from '../../pages/settings/Rules';
import type { LinkReport, Rule } from '../RuleEditor';
import { CaptchaChain, type ChainSolver } from './CaptchaChain';
import { Pulse, type PulseCheck } from './Pulse';
import { RuleSwitch, wayOf, type Landing } from './RuleSwitch';
import { TrackMerge, type MediaTrack } from './TrackMerge';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

/** The system's answer to "less motion?". Most tests ask for it, so a picture stands on its still frame. */
function reducedMotion(reduced: boolean) {
  vi.stubGlobal('matchMedia', () => ({ matches: reduced, addEventListener() {}, removeEventListener() {} }));
}

/** A picture that may move asks the stage to hold it until it is on screen, which jsdom cannot answer. */
function letMove() {
  reducedMotion(false);
  Object.assign(SVGSVGElement.prototype, { pauseAnimations() {}, unpauseAnimations() {}, setCurrentTime() {} });
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
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

const draw = (picture: ReactNode) => act(async () => root.render(<I18nProvider>{picture}</I18nProvider>));
const label = () => host.querySelector('svg')!.getAttribute('aria-label');
const moving = (el: Element) => el.querySelectorAll('animate, animateMotion, animateTransform').length;

describe('the captcha chain', () => {
  const solvers: ChainSolver[] = [
    { id: 'capsolver', name: 'CapSolver', place: 1 },
    { id: '2captcha', name: '2Captcha', place: 2, skipped: 'No key set' },
    { id: '9kw', name: '9kw', place: 0 },
  ];
  const station = (id: string) => host.querySelector(`[data-station="${id}"]`)!;
  const lit = (id: string) => station(id).querySelector('circle[fill="var(--accent)"]') !== null;
  const chain = (props: Partial<Parameters<typeof CaptchaChain>[0]> = {}) => (
    <CaptchaChain solvers={solvers} wait={60} off={false} label="Captcha account order" {...props} />
  );

  it('sends a captcha to you first and then to the accounts in their order', async () => {
    await draw(chain());
    expect(label()).toBe(
      'Captcha account order: You, Up to 60 s for your answer, 1. CapSolver, 2. 2Captcha (No key set)',
    );
    expect([...host.querySelectorAll('[data-station]')].map((s) => s.getAttribute('data-station'))).toEqual([
      'you',
      'wait',
      'capsolver',
      '2captcha',
      '9kw',
    ]);
    expect(lit('you')).toBe(true);
    expect(lit('capsolver')).toBe(true);
  });

  it('leaves an account without a key and one that is switched off grey, and says why', async () => {
    await draw(chain());
    expect(lit('2captcha')).toBe(false);
    expect(station('2captcha').textContent).toContain('No key set');
    expect(lit('9kw')).toBe(false);
    expect(station('9kw').textContent).toBe('9kwOff');
  });

  it('shows the configured wait in the ring', async () => {
    await draw(chain({ wait: 240 }));
    expect(station('wait').getAttribute('data-state')).toBe('on');
    expect(station('wait').querySelector('text')!.textContent).toBe('240');
    expect(station('wait').textContent).toContain('Up to 240 s for your answer');
  });

  it('draws the ring switched off when the accounts start at once', async () => {
    await draw(chain({ wait: null }));
    expect(station('wait').getAttribute('data-state')).toBe('off');
    expect(station('wait').querySelector('text')).toBeNull();
    expect(station('wait').textContent).toBe('Captcha accounts start at once');
  });

  it('has no ring while no account is in the order', async () => {
    await draw(chain({ solvers: solvers.map((s) => ({ ...s, place: 0 })) }));
    expect(host.querySelector('[data-station="wait"]')).toBeNull();
    expect(label()).toBe('Captcha account order: You');
  });

  it('stands grey when captchas are switched off', async () => {
    await draw(chain({ off: true }));
    expect(host.querySelector('svg')!.getAttribute('data-state')).toBe('off');
    expect(label()).toBe('Captcha account order: Off');
    expect(host.querySelector('circle[fill="var(--accent)"]')).toBeNull();
  });

  it('runs the ring down while the captcha waits for you, and passes an account that takes nothing', async () => {
    letMove();
    await draw(chain());
    expect(station('wait').querySelector('animate[attributeName="stroke-dashoffset"]')).not.toBeNull();
    expect(moving(station('capsolver'))).toBeGreaterThan(0);
    expect(moving(station('2captcha'))).toBe(0);
    expect(host.querySelector('animateMotion')).not.toBeNull();
  });
});

describe('the way of a link through the rules', () => {
  const rule = (reject = false): Rule => ({ name: '', conditions: [], action: reject ? { reject: true } : {} });
  const link = (matched: number[], rejected = false): LinkReport => ({
    url: 'https://example.org/a.mkv',
    filename: 'a.mkv',
    matched,
    effect: {},
    verdict: { rejected },
    result: { package: '', filename: 'a.mkv' },
  });

  it('runs past every rule while the list does not stop at a match', () => {
    expect(wayOf(link([0, 2]), [rule(), rule(), rule()], false, 'packagizer')).toEqual({
      matched: [0, 2],
      ends: -1,
      rejected: false,
    });
  });

  it('ends at the first match when the list stops there', () => {
    expect(wayOf(link([1]), [rule(), rule(), rule()], true, 'packagizer')).toEqual({
      matched: [1],
      ends: 1,
      rejected: false,
    });
  });

  it('ends at the first matching rule that rejects, whatever matches below it', () => {
    expect(wayOf(link([0, 1, 2], true), [rule(), rule(true), rule(true)], false, 'filter')).toEqual({
      matched: [0, 1],
      ends: 1,
      rejected: true,
    });
  });
});

describe('the rule switch', () => {
  const rules = [{ name: 'Films' }, { name: 'Series' }, { name: 'Old rule', skipped: 'Off' }, { name: 'Everything else' }];
  const folder: Landing = { kind: 'folder', title: '/downloads/series', note: 'Category: Series' };
  const state = (i: number) => host.querySelector(`[data-rule="${i}"]`)!.getAttribute('data-state');
  const states = () => rules.map((_, i) => state(i));
  const picture = (props: Partial<Parameters<typeof RuleSwitch>[0]> = {}) => (
    <RuleSwitch rules={rules} way={null} landing={null} off={false} label="Rules, in the order they run" onOpen={() => {}} {...props} />
  );

  it('stands the rules in their order and waits while there is no sample', async () => {
    await draw(picture());
    expect(host.querySelector('svg')!.getAttribute('data-state')).toBe('waiting');
    expect(states()).toEqual(['passed', 'passed', 'off', 'passed']);
    expect(host.querySelector('[data-way]')).toBeNull();
    expect(host.querySelector('[data-landing]')!.textContent).toBe('Enter a link above to see the way it takes');
    expect(label()).toBe('Rules, in the order they run: 1. Films, 2. Series, 3. Old rule, 4. Everything else');
  });

  it('turns the link off at the rule that stops it and never asks the rules below', async () => {
    await draw(picture({ way: { matched: [1], ends: 1, rejected: false }, landing: folder }));
    expect(states()).toEqual(['passed', 'takes', 'unasked', 'unasked']);
    // Down the lane to the second rule, then onto the siding and down to the landing.
    expect(host.querySelector('[data-way]')!.getAttribute('d')).toBe('M22 20L22 64L22 100L48 118L48 214');
    expect(host.querySelector('[data-landing]')!.getAttribute('data-landing')).toBe('folder');
    expect(host.querySelector('[data-landing]')!.textContent).toBe('/downloads/seriesCategory: Series');
    expect(label()).toBe('Rules, in the order they run: Series → /downloads/series → Category: Series');
  });

  it('carries the link past every rule when none stops it, and lights each that matches', async () => {
    await draw(picture({ way: { matched: [0, 3], ends: -1, rejected: false }, landing: folder }));
    expect(states()).toEqual(['takes', 'passed', 'off', 'takes']);
    expect(host.querySelector('[data-way]')!.getAttribute('d')).toBe('M22 20L22 64L22 100L22 136L22 172L48 190L48 214');
  });

  it('lands a link no rule matched and says so', async () => {
    const landing: Landing = { kind: 'folder', title: '/downloads', note: 'No rule matched' };
    await draw(picture({ way: { matched: [], ends: -1, rejected: false }, landing }));
    expect(states()).toEqual(['passed', 'passed', 'off', 'passed']);
    expect(label()).toBe('Rules, in the order they run: No rule matched → /downloads → No rule matched');
  });

  it('lands a rejected link in red', async () => {
    const landing: Landing = { kind: 'rejected', title: 'Rejected', note: 'Too small' };
    await draw(picture({ way: { matched: [0], ends: 0, rejected: true }, landing }));
    const end = host.querySelector('[data-landing="rejected"]')!;
    expect(end.querySelector('circle[fill="var(--status-fail-solid)"]')).not.toBeNull();
    expect(end.textContent).toBe('RejectedToo small');
  });

  it('stands grey while the whole list is switched off', async () => {
    await draw(picture({ off: true, way: { matched: [0], ends: -1, rejected: false }, landing: folder }));
    expect(host.querySelector('svg')!.getAttribute('data-state')).toBe('off');
    expect(states()).toEqual(['off', 'off', 'off', 'off']);
    expect(label()).toBe('Rules, in the order they run: This list is switched off');
  });

  it('opens a rule from its name', async () => {
    const opened: number[] = [];
    await draw(picture({ onOpen: (i) => opened.push(i) }));
    await act(async () => host.querySelector<HTMLButtonElement>('[data-rule="1"] button')!.click());
    expect(opened).toEqual([1]);
  });

  it('sends the link along its way, and only the rules that match answer', async () => {
    letMove();
    await draw(picture({ way: { matched: [1], ends: 1, rejected: false }, landing: folder }));
    expect(host.querySelector('animateMotion')!.getAttribute('path')).toBe('M22 20L22 64L22 100L48 118L48 214');
    expect(moving(host.querySelector('[data-rule="0"]')!)).toBe(0);
    expect(moving(host.querySelector('[data-rule="1"]')!)).toBeGreaterThan(0);
    expect(moving(host.querySelector('[data-rule="3"]')!)).toBe(0);
  });
});

describe('the rules card', () => {
  const reply = (body: unknown) =>
    new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });
  const cfg = {
    downloadDir: '/downloads',
    categories: [{ id: 'series', name: 'Series', dir: '/media/series' }],
    packagizer: {
      rules: [
        { name: 'Films', conditions: [], action: {} },
        { name: 'Series', conditions: [], action: { category: 'series' } },
      ],
    },
  } as unknown as Settings;
  let asked: { set: { rules: Rule[] }; links: { url: string }[] }[];

  beforeEach(() => {
    asked = [];
    const unobserved = class {
      observe() {}
      disconnect() {}
    };
    vi.stubGlobal('IntersectionObserver', unobserved);
    vi.stubGlobal('ResizeObserver', unobserved);
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url === '/api/rules/grammar') {
          return reply({ fields: [], operators: [], actions: [], variables: [], categories: [], limits: {} });
        }
        if (url === '/api/rules/preview') {
          const body = JSON.parse(String(init!.body));
          asked.push(body);
          const rulesOf = body.set.rules.map((r: Rule, index: number) => ({ index, name: r.name, matched: 0 }));
          const links = body.links.map((l: { url: string }) => ({
            url: l.url,
            filename: '',
            matched: [1],
            effect: { category: 'series' },
            verdict: { rejected: false },
            result: { package: '', filename: '' },
          }));
          return reply({ problems: [], rules: rulesOf, links });
        }
        if (url.startsWith('/api/uistate')) return reply({});
        return reply([]);
      }),
    );
  });

  const page = () => {
    const draft = {
      cfg,
      saved: cfg,
      patch() {},
      replace() {},
      dirty: false,
      async patchNow() {},
      reseed() {},
      fieldError: () => undefined,
      showRefusalsAt: () => () => {},
    } satisfies SettingsDraft;
    return (
      <ToastProvider>
        <MemoryRouter>
          <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: async () => {} }}>
            <Rules />
          </SettingsProvider>
        </MemoryRouter>
      </ToastProvider>
    );
  };
  const picture = () => host.querySelector('[data-new="rule-switch"]');
  /** The rows of the rule list. The categories below are rows of the same kind, without a way to duplicate one. */
  const ruleRows = () =>
    [...host.querySelectorAll('[data-new="row-window"] > li')].filter((row) => row.querySelector('[aria-label="Duplicate this rule"]'));
  /** The card remembers which of the two it shows, also from one test to the next. */
  const show = (view: 'Graphic' | 'List') =>
    act(async () =>
      [...host.querySelectorAll<HTMLElement>('[aria-label="View"] [role="tab"]')].find((tab) => tab.textContent === view)!.click(),
    );
  /** The dry run waits a quarter of a second after the last edit. */
  const dryRun = () => act(async () => void (await new Promise((done) => setTimeout(done, 320))));

  it('draws the way of the sample typed above it, with the folder and the category it lands in', async () => {
    await draw(page());
    await dryRun();
    expect(picture()!.getAttribute('data-state')).toBe('waiting');

    const field = host.querySelector<HTMLInputElement>('label input')!;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, 'https://example.org/s01e01.mkv');
      field.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await dryRun();

    expect(asked[asked.length - 1].links).toEqual([expect.objectContaining({ url: 'https://example.org/s01e01.mkv' })]);
    expect(picture()!.getAttribute('data-state')).toBe('going');
    expect(picture()!.querySelector('[data-rule="1"]')!.getAttribute('data-state')).toBe('takes');
    expect(picture()!.querySelector('[data-landing]')!.textContent).toBe('/media/seriesCategory: Series');
  });

  it('keeps the list with its switches and buttons behind the selector', async () => {
    await draw(page());
    await dryRun();
    expect(ruleRows()).toHaveLength(0);

    await show('List');
    expect(picture()).toBeNull();
    const rows = ruleRows();
    expect(rows).toHaveLength(2);
    expect([...rows[0].querySelectorAll('button[aria-label]')].map((b) => b.getAttribute('aria-label'))).toEqual([
      'This rule runs',
      'Move up',
      'Move down',
      'Duplicate this rule',
      'Remove this rule',
    ]);
  });

  it('opens a rule in its window from the picture', async () => {
    await draw(page());
    await dryRun();
    await show('Graphic');
    await act(async () => picture()!.querySelector<HTMLButtonElement>('[data-rule="1"] button')!.click());
    expect(document.querySelector('[role="dialog"]')!.textContent).toContain('Series');
  });
});

describe('the tracks of a video', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        disconnect() {}
      },
    );
  });

  const tracks: MediaTrack[] = [
    { id: 'video', name: 'Video', value: 'Up to 1080p' },
    { id: 'audio', name: 'Audio', value: 'Best available' },
    { id: 'subtitle', name: 'Subtitles', value: 'en,de' },
  ];
  const track = (id: string) => host.querySelector(`[data-track="${id}"]`)!;
  const picture = (list = tracks, filename = '%(title)s.%(ext)s', more = {}) => (
    <TrackMerge tracks={list} extension="mkv" filename={filename} label="Quality" {...more} />
  );

  it('says what each track takes and what file comes out', async () => {
    await draw(picture());
    expect(label()).toBe('Quality: Video Up to 1080p, Audio Best available, Subtitles en,de, mkv, %(title)s.%(ext)s');
    expect(track('video').textContent).toBe('VideoUp to 1080p');
    expect(host.querySelector('[data-file]')!.getAttribute('data-file')).toBe('mkv');
    expect(host.querySelector('[data-file]')!.textContent).toContain('mkv');
    expect(host.querySelector('[data-filename]')!.textContent).toBe('%(title)s.%(ext)s');
  });

  it('runs every track into the file', async () => {
    await draw(picture());
    const ways = tracks.map((tr) => track(tr.id).querySelector('path')!.getAttribute('d'));
    expect(ways).toEqual(['M20 88L66 38H250L292 88', 'M20 88L66 88H250L292 88', 'M20 88L66 138H250L292 88']);
    expect(track('subtitle').querySelector('circle[fill="var(--accent)"]')).not.toBeNull();
  });

  it('lays itself out at the width of its card and keeps the file at the far end', async () => {
    const width = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(900);
    await draw(picture());
    expect(host.querySelector('svg')!.getAttribute('viewBox')).toMatch(/^0 0 900 /);
    expect(host.querySelector('[data-file]')!.getAttribute('transform')).toBe('translate(872 88)');
    width.mockRestore();
  });

  it('draws the files the chapters become and a sidecar beside the file, and says so', async () => {
    await draw(picture(tracks, '%(title)s.%(ext)s', { split: 'One more file per chapter', sidecar: 'nfo' }));
    expect(host.querySelector('[data-split]')).not.toBeNull();
    expect(host.querySelector('[data-sidecar="nfo"]')).not.toBeNull();
    expect(label()).toBe(
      'Quality: Video Up to 1080p, Audio Best available, Subtitles en,de, mkv, nfo, %(title)s.%(ext)s, One more file per chapter',
    );
  });

  it('gives the file and every track a rainbow position of its own', async () => {
    await draw(picture());
    const hues = [...host.querySelectorAll<SVGGElement>('.kl-pic-hue')].map((g) => g.style.getPropertyValue('--item-hue'));
    expect(hues).toHaveLength(tracks.length + 1);
    expect(new Set(hues).size).toBe(hues.length);
  });

  it('shows the output template as it is set', async () => {
    await draw(picture(tracks, '%(uploader)s/%(title)s.%(ext)s'));
    expect(host.querySelector('[data-filename]')!.textContent).toBe('%(uploader)s/%(title)s.%(ext)s');
  });

  it('sends one parcel per track', async () => {
    letMove();
    await draw(picture());
    expect(host.querySelectorAll('animateMotion')).toHaveLength(tracks.length);
  });
});

describe('the pulse of the self-test', () => {
  const checks = (...statuses: PulseCheck['status'][]): PulseCheck[] =>
    statuses.map((status, i) => ({ id: `check${i}`, name: `Check ${i + 1}`, status, word: status === 'pending' ? 'Waiting' : status }));
  /** The wide arrangement, which stands first. */
  const row = () => host.querySelector('svg')!;
  const bead = (i: number) => row().querySelector(`[data-check="check${i}"]`)!;
  const fill = (i: number) => bead(i).querySelector('circle')!.getAttribute('fill');

  it('gives each bead the colour of its answer', async () => {
    await draw(<Pulse checks={checks('pass', 'warn', 'fail', 'unknown', 'skipped')} running={false} label="Self-test" />);
    expect([0, 1, 2, 3, 4].map(fill)).toEqual([
      'var(--status-ok-solid)',
      'var(--status-warn-solid)',
      'var(--status-fail-solid)',
      'var(--status-info-solid)',
      'var(--carbon-surface2)',
    ]);
    expect(bead(4).querySelector('circle')!.getAttribute('stroke-dasharray')).toBe('3 4');
    expect(row().getAttribute('aria-label')).toBe(
      'Self-test: Check 1 pass, Check 2 warn, Check 3 fail, Check 4 unknown, Check 5 skipped',
    );
  });

  it('shows the whole line in colour at rest', async () => {
    await draw(<Pulse checks={checks('pass', 'pass', 'fail')} running={false} label="Self-test" />);
    expect(host.firstElementChild!.getAttribute('data-state')).toBe('rest');
    expect(row().querySelector('[data-done]')!.getAttribute('data-done')).toBe('2');
    expect(row().querySelector('[data-beat]')).toBeNull();
  });

  it('marks the check that is being made, and colours the line as far as the test has come', async () => {
    await draw(<Pulse checks={checks('pass', 'pending', 'pending')} running label="Self-test" />);
    expect(host.firstElementChild!.getAttribute('data-state')).toBe('running');
    expect(bead(1).querySelector('circle')!.getAttribute('stroke')).toBe('var(--accent-glyph)');
    expect(bead(2).querySelector('circle')!.getAttribute('stroke')).toBeNull();
    expect(row().querySelector('[data-done]')!.getAttribute('data-done')).toBe('1');
  });

  it('stands still, with no beat, when the system asks for less motion', async () => {
    await draw(<Pulse checks={checks('pass', 'pending')} running label="Self-test" />);
    expect(moving(host)).toBe(0);
  });

  it('moves the beat from the bead it stood on to the next check without an answer', async () => {
    letMove();
    await draw(<Pulse checks={checks('pending', 'pending', 'pending')} running label="Self-test" />);
    expect(row().querySelector('[data-beat]')!.getAttribute('data-beat')).toBe('0');
    // It starts on the first bead, so nothing travels yet.
    expect(row().querySelector('animateMotion')).toBeNull();

    await draw(<Pulse checks={checks('pass', 'fail', 'pending')} running label="Self-test" />);
    expect(row().querySelector('[data-beat]')!.getAttribute('data-beat')).toBe('2');
    const way = row().querySelector('animateMotion')!.getAttribute('path')!;
    expect(way.startsWith('M44 30L')).toBe(true);
    expect(way.endsWith('L220 30')).toBe(true);
    expect(fill(1)).toBe('var(--status-fail-solid)');
  });

  it('lets the beat go when the run has finished', async () => {
    letMove();
    await draw(<Pulse checks={checks('pass', 'pending')} running label="Self-test" />);
    expect(row().querySelector('[data-beat]')).not.toBeNull();
    await draw(<Pulse checks={checks('pass', 'pass')} running={false} label="Self-test" />);
    expect(row().querySelector('[data-beat]')).toBeNull();
    expect(moving(host)).toBe(0);
  });
});

describe('the pictures on their pages', () => {
  const reply = (body: unknown, status = 200) =>
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
  const module = (id: string, enabled: boolean) => ({ id, verdict: 'shipped', page: id, enabled, switch: 'setting', parked: false });
  let answers: Record<string, () => unknown>;

  beforeEach(() => {
    answers = {};
    const unobserved = class {
      observe() {}
      disconnect() {}
    };
    vi.stubGlobal('IntersectionObserver', unobserved);
    vi.stubGlobal('ResizeObserver', unobserved);
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        const answer = answers[`${init?.method ?? 'GET'} ${url}`];
        if (answer) return reply(answer());
        if (url.startsWith('/api/uistate')) return reply({});
        return reply({}, 404);
      }),
    );
  });

  const on = (cfg: object, modules: object[], page: ReactNode) => {
    const draft = {
      cfg: cfg as Settings,
      saved: cfg as Settings,
      patch() {},
      replace() {},
      dirty: false,
      async patchNow() {},
      reseed() {},
      fieldError: () => undefined,
      showRefusalsAt: () => () => {},
    } satisfies SettingsDraft;
    return (
      <ToastProvider>
        <MemoryRouter>
          <SettingsProvider draft={draft} features={{ features: { modules, pages: [] } as never, toggle: async () => {} }}>
            {page}
          </SettingsProvider>
        </MemoryRouter>
      </ToastProvider>
    );
  };
  const settle = () => act(async () => void (await new Promise((done) => setTimeout(done, 0))));

  describe('the captcha cards', () => {
    const solver = (id: string, label: string) => ({ id, label, kind: 'apiKey', group: 'captchaSolver', whereUrl: '' });
    const cfg = { captchaSolverOrder: ['capsolver', '2captcha'], captchaSolverOnlyUnwatched: true, captchaSolverWait: 90 };

    const catalogue = [solver('2captcha', '2Captcha'), solver('9kw', '9kw.eu'), solver('capsolver', 'CapSolver')] as CatalogueService[];
    const accounts = [{ service: 'capsolver', account: '', configured: true, enabled: true }] as Account[];
    const cards = <CaptchaCards hue={0} accounts={accounts} catalogue={catalogue} />;

    it('draws the chain from the order, the keys and the wait that are set', async () => {
      await draw(on(cfg, [module('captcha', true)], cards));
      await settle();
      const chain = host.querySelector('[data-new="captcha-chain"]')!;
      expect(chain.getAttribute('aria-label')).toBe(
        'Captcha account order: You, Up to 90 s for your answer, 1. CapSolver, 2. 2Captcha (No key set)',
      );
      expect(chain.querySelector('[data-station="9kw"]')!.textContent).toBe('9kw.euOff');
    });

    it('says the accounts start at once when they do not wait for somebody watching', async () => {
      await draw(on({ ...cfg, captchaSolverOnlyUnwatched: false }, [module('captcha', true)], cards));
      await settle();
      expect(host.querySelector('[data-station="wait"]')!.textContent).toBe('Captcha accounts start at once');
    });

    it('stands the chain grey while the captcha module is switched off', async () => {
      await draw(on(cfg, [module('captcha', false)], cards));
      await settle();
      expect(host.querySelector('[data-new="captcha-chain"]')!.getAttribute('data-state')).toBe('off');
    });
  });

  describe('the self-test card on the health page', () => {
    const result = (id: string, status: string) => ({ id, status, code: `${id}.ok`, at: '2026-10-01T10:00:00Z' });
    const bead = (id: string) => host.querySelector(`[data-check="${id}"]`);

    it('says so when the instance was never tested', async () => {
      answers['GET /api/selftest'] = () => ({ id: '', startedAt: '0001-01-01T00:00:00Z', planned: [], results: [] });
      await draw(on({}, [], <SelfTestPulseCard hue={1} />));
      await settle();
      expect(host.textContent).toContain('Not run yet.');
      expect(host.querySelector('[data-new="selftest-pulse"]')).toBeNull();
    });

    it('shows the last run at rest, a bead per check that was planned', async () => {
      answers['GET /api/selftest'] = () => ({
        id: 'a',
        startedAt: '2026-10-01T10:00:00Z',
        finishedAt: '2026-10-01T10:00:05Z',
        planned: ['jd', 'ytdlp', 'relay'],
        results: [result('jd', 'pass'), result('ytdlp', 'fail'), result('relay', 'skipped')],
      });
      await draw(on({}, [], <SelfTestPulseCard hue={1} />));
      await settle();
      expect(host.querySelector('[data-new="selftest-pulse"]')!.getAttribute('data-state')).toBe('rest');
      expect(['jd', 'ytdlp', 'relay'].map((id) => bead(id)!.getAttribute('data-status'))).toEqual(['pass', 'fail', 'skipped']);
      expect(host.querySelector('svg[role="img"]')!.getAttribute('aria-label')).toBe(
        'Self-test: JDownloader backend Fine, yt-dlp Broken, Relay Not set up',
      );
    });

    it('starts a run and waits on its first check', async () => {
      answers['GET /api/selftest'] = () => ({ id: '', startedAt: '0001-01-01T00:00:00Z', planned: [], results: [] });
      answers['POST /api/selftest'] = () => ({ id: 'b', startedAt: '2026-10-01T10:00:00Z', planned: ['jd', 'ytdlp'], results: [] });
      await draw(on({}, [], <SelfTestPulseCard hue={1} />));
      await settle();
      const run = [...host.querySelectorAll('button')].find((b) => b.textContent === 'Run self-test')!;
      await act(async () => run.click());
      await settle();
      expect(host.querySelector('[data-new="selftest-pulse"]')!.getAttribute('data-state')).toBe('running');
      expect(bead('jd')!.getAttribute('data-status')).toBe('pending');
      expect(bead('jd')!.querySelector('circle')!.getAttribute('stroke')).toBe('var(--accent-glyph)');
    });
  });
});
