// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { Settings, YtdlpOptions } from '../../lib/api';
import { I18nProvider } from '../../lib/i18n';
import { SettingsProvider, type SettingsDraft } from './context';
import { Resolvers } from './Resolvers';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// The page's track picture animates, which jsdom's SVG cannot.
Object.assign(SVGSVGElement.prototype, { pauseAnimations() {}, unpauseAnimations() {}, setCurrentTime() {} });

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
globalThis.IntersectionObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
} as unknown as typeof IntersectionObserver;

let root: Root;
let host: HTMLDivElement;
let ytdlp: YtdlpOptions;

const YTDLP = {
  quality: 'best',
  customFormat: '',
  videoFormat: 'best',
  audioFormat: 'best',
  subtitleLangs: '',
  subtitleAuto: false,
  playlist: false,
  outputTemplate: '',
  audioBitrate: '',
  audioLang: '',
  subtitleStrict: false,
  music: false,
  embed: { metadata: false, thumbnail: false, chapters: false, subs: false, splitChapters: false, nfo: false, separate: false },
  measure: { enabled: false, shortPercent: 90, failOnShort: false },
  live: { enabled: false, fromStart: false, maxMinutes: 0, maxMB: 0 },
  sponsorBlock: null,
} as YtdlpOptions;

function Harness() {
  const [cfg, setCfg] = useState({ ytdlp: YTDLP, ytdlpPresets: {} } as unknown as Settings);
  ytdlp = cfg.ytdlp;
  const draft = {
    cfg,
    patch: (fields: Partial<Settings>) => setCfg((c) => ({ ...c, ...fields })),
    fieldError: () => undefined,
    showRefusalsAt: () => () => {},
  } as unknown as SettingsDraft;
  return (
    <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: async () => {} }}>
      <Resolvers />
    </SettingsProvider>
  );
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  act(() =>
    root.render(
      <MemoryRouter>
        <I18nProvider>
          <Harness />
        </I18nProvider>
      </MemoryRouter>,
    ),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

function tab(name: string, strip: string): HTMLButtonElement {
  const scope = document.querySelector(`[role="tablist"][aria-label="${strip}"]`);
  const found = [...(scope?.querySelectorAll<HTMLButtonElement>('[role="tab"]') ?? [])].find((b) => b.textContent === name);
  if (!found) throw new Error(`no tab "${name}" in "${strip}"`);
  return found;
}

it('cuts sponsor segments out of every yt-dlp download by default once chosen', () => {
  act(() => tab('Cut out', 'Sponsor').click());
  expect(ytdlp.sponsorBlock).toEqual({ remove: ['sponsor'], mark: [] });
  act(() => tab('Mark', 'Intro').click());
  expect(ytdlp.sponsorBlock).toEqual({ remove: ['sponsor'], mark: ['intro'] });
});

it('offers a highlight only as a mark, since it is a point rather than a stretch', () => {
  expect(() => tab('Cut out', 'Highlight')).toThrow();
  expect(tab('Mark', 'Highlight')).toBeDefined();
});
