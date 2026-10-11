// @vitest-environment jsdom
import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter, NavLink, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../../lib/i18n';
import type { NewsTab, WhatsNew } from '../../lib/useWhatsNew';
import type { Change } from '../../lib/whatsNew';
import { NewDots, collect } from './NewDots';
import { ReleaseNotes } from './ReleaseNotes';
import { WhatsNewWindow } from './WhatsNewWindow';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const HEAD: Change = { id: 'overview-head', to: ['/'], text: 'whatsnew.change.pageActions' };
const RING: Change = { id: 'seed-ring', to: ['/settings/torrents'], text: 'whatsnew.change.seedRing' };
const ADD: Change = { id: 'accounts-add', to: ['/accounts', '/settings/accounts'], text: 'whatsnew.change.pairingSteps' };
const SEARCH: Change = { id: 'settings-search', to: ['/settings'], text: 'whatsnew.change.pairingSteps' };

// jsdom has no ResizeObserver, which the window's tab strip measures with.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;
let rects: typeof Element.prototype.getClientRects;

beforeEach(() => {
  // jsdom lays nothing out; everything in the document counts as drawn.
  rects = Element.prototype.getClientRects;
  Element.prototype.getClientRects = () => [{}] as unknown as DOMRectList;
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  Element.prototype.getClientRects = rects;
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function news(changes: Change[], seen: string[] = [], over: Partial<WhatsNew> = {}): WhatsNew {
  const done = new Set(seen);
  return {
    version: 'v1.9.0',
    changes,
    seen: done,
    unseen: changes.filter((c) => !done.has(c.id)),
    dots: true,
    markSeen: vi.fn(),
    showDots: vi.fn(),
    ...over,
  };
}

/** The app's frame as far as the dots read it: a rail, a tab strip of settings tiles, a page. */
function Frame({ at, children, page }: { at: string; children?: ReactNode; page?: ReactNode }) {
  return (
    <MemoryRouter initialEntries={[at]}>
      <I18nProvider>
        <nav>
          <NavLink to="/">Overview</NavLink>
          <NavLink to="/accounts">Accounts</NavLink>
          <NavLink to="/settings">Settings</NavLink>
        </nav>
        <div role="tablist">
          <a href="/settings/torrents" data-tab-id="torrents">
            Torrents
          </a>
          <a href="/settings/accounts" data-tab-id="accounts">
            Accounts
          </a>
        </div>
        <main>
          {page}
          <Here />
        </main>
        {children}
      </I18nProvider>
    </MemoryRouter>
  );
}

function Here() {
  return <output data-here>{useLocation().pathname}</output>;
}

const link = (path: string) => host.querySelector(`a[href="${path}"]`)!;

/**
 * layOut gives the page the boxes its `data-box` attributes name (left, top,
 * width, height) and lets `data-scrolls` scroll sideways, since jsdom has no
 * layout. Whatever names no box fills the window.
 */
function layOut() {
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    const [x, y, w, h] = (this.getAttribute('data-box') ?? '0,0,1000,800').split(',').map(Number);
    return new DOMRect(x, y, w, h);
  });
  const computed = globalThis.getComputedStyle;
  vi.spyOn(globalThis, 'getComputedStyle').mockImplementation((el, pseudo) => {
    const style = computed(el, pseudo);
    style.overflowX = el.hasAttribute('data-scrolls') ? 'auto' : 'visible';
    style.overflowY = 'visible';
    return style;
  });
}

describe('where a change is marked', () => {
  it('stands on the element that carries its marker', async () => {
    await act(async () => root.render(<Frame at="/" page={<h2 data-new="overview-head">Head</h2>} />));
    const [target] = collect([HEAD], '/', false);
    expect(target.kind).toBe('change');
    expect(target.el).toBe(host.querySelector('[data-new="overview-head"]'));
  });

  it('puts a change on another page on the rail entry that leads there', async () => {
    await act(async () => root.render(<Frame at="/downloads" />));
    const targets = collect([HEAD], '/downloads', true);
    expect(targets.map((t) => [t.kind, t.el])).toEqual([['way', link('/')]]);
  });

  it('leads through the settings to the tile of the page', async () => {
    await act(async () => root.render(<Frame at="/" />));
    expect(collect([RING], '/', true).map((t) => t.el)).toEqual([link('/settings'), link('/settings/torrents')]);
    expect(collect([RING], '/settings/look', true).map((t) => t.el)).toEqual([link('/settings/torrents')]);
  });

  it('gathers every change behind one link on one dot', async () => {
    await act(async () => root.render(<Frame at="/downloads" />));
    const other: Change = { ...HEAD, id: 'overview-needs' };
    const [target, ...more] = collect([HEAD, other], '/downloads', true);
    expect(more).toEqual([]);
    expect(target.changes).toEqual([HEAD, other]);
  });

  it('takes the first address that has a link on its way', async () => {
    await act(async () => root.render(<Frame at="/" />));
    expect(collect([ADD], '/', true).map((t) => t.el)).toEqual([link('/accounts')]);
    link('/accounts').remove();
    expect(collect([ADD], '/', true).map((t) => t.el)).toEqual([link('/settings'), link('/settings/accounts')]);
  });

  it('marks the page once it has had time to draw and still lacks the element', async () => {
    await act(async () => root.render(<Frame at="/" />));
    expect(collect([HEAD], '/', false)).toEqual([]);
    const [target] = collect([HEAD], '/', true);
    expect(target.kind).toBe('page');
    expect(target.el).toBe(host.querySelector('main'));
  });

  it('marks every page of a section for a change the section names', async () => {
    await act(async () => root.render(<Frame at="/settings/torrents" />));
    expect(collect([SEARCH], '/settings/torrents', true).map((t) => t.kind)).toEqual(['page']);
    expect(collect([SEARCH], '/', true).map((t) => t.el)).toEqual([link('/settings')]);
  });
});

describe('the dots', () => {
  const dots = () => [...document.querySelectorAll<HTMLButtonElement>('[data-new-dot]')];

  async function draw(at: string, shown: WhatsNew, page?: ReactNode) {
    vi.useFakeTimers();
    await act(async () =>
      root.render(
        <Frame at={at} page={page}>
          <NewDots news={shown} />
        </Frame>,
      ),
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
  }

  it('count a change as seen on a click', async () => {
    const shown = news([HEAD]);
    await draw('/', shown, <h2 data-new="overview-head">Head</h2>);
    expect(dots().map((d) => d.dataset.newDot)).toEqual(['change']);
    await act(async () => dots()[0].click());
    expect(shown.markSeen).toHaveBeenCalledWith(['overview-head']);
  });

  it('follow the link on a click on the way', async () => {
    const shown = news([RING]);
    await draw('/', shown);
    const way = dots().find((d) => d.getAttribute('aria-label') === 'Changes this way: 1')!;
    await act(async () => way.click());
    expect(host.querySelector('[data-here]')!.textContent).toBe('/settings');
    expect(shown.markSeen).not.toHaveBeenCalled();
  });

  it('stand on the page for a change it does not draw, once the page has had time to', async () => {
    const shown = news([HEAD]);
    await draw('/', shown);
    expect(dots()).toEqual([]);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(dots().map((d) => d.dataset.newDot)).toEqual(['page']);
    await act(async () => dots()[0].click());
    expect(shown.markSeen).toHaveBeenCalledWith(['overview-head']);
  });

  it('are gone for a change that was seen, and from the links that led to it', async () => {
    await draw('/', news([RING], ['seed-ring']));
    expect(dots()).toEqual([]);
  });

  it('are not drawn while the switch is off', async () => {
    await draw('/', news([RING], [], { dots: false }));
    expect(dots()).toEqual([]);
  });

  it('stand over a window that holds the changed element', async () => {
    layOut();
    await draw(
      '/',
      news([HEAD]),
      <div role="dialog" aria-modal="true" data-box="100,100,400,300">
        <h2 data-new="overview-head" data-box="120,120,200,30">
          Head
        </h2>
      </div>,
    );
    const [dot] = dots();
    expect(dot.style.display).toBe('');
    expect(dot.parentElement!.style.zIndex).toBe('55');
  });

  it('stay under a window that covers the changed element', async () => {
    layOut();
    await draw(
      '/',
      news([HEAD]),
      <>
        <h2 data-new="overview-head" data-box="120,120,200,30">
          Head
        </h2>
        <div role="dialog" aria-modal="true" data-box="100,100,400,300" />
      </>,
    );
    expect(dots().map((d) => d.style.display)).toEqual(['none']);
  });

  it('keep to the edge of a box the change has to be scrolled sideways to', async () => {
    layOut();
    await draw(
      '/',
      news([HEAD]),
      <div data-box="0,0,300,200" data-scrolls>
        <div data-new="overview-head" data-box="0,10,900,30">
          Row
        </div>
      </div>,
    );
    const [dot] = dots();
    expect([dot.style.display, dot.style.left, dot.style.top]).toEqual(['', '288px', '14px']);
  });
});

describe('the window', () => {
  function Shown({ shown, notes, tab }: { shown: WhatsNew; notes: string; tab: NewsTab }) {
    return (
      <MemoryRouter initialEntries={['/downloads']}>
        <I18nProvider>
          <Routes>
            <Route path="*" element={<Here />} />
          </Routes>
          <WhatsNewWindow news={shown} notes={notes} tab={tab} onTab={onTab} onClose={onClose} />
        </I18nProvider>
      </MemoryRouter>
    );
  }
  const onTab = vi.fn();
  const onClose = vi.fn();
  const button = (name: string) => [...host.querySelectorAll('button')].find((b) => b.textContent === name)!;

  beforeEach(() => {
    onTab.mockClear();
    onClose.mockClear();
  });

  it('shows the release notes of the running version', async () => {
    await act(async () => root.render(<Shown shown={news([HEAD])} notes={'## Fixed\n\n- A **bold** fix.'} tab="notes" />));
    expect(host.querySelector('[role="dialog"]')!.textContent).toContain('New in 1.9.0');
    expect(host.querySelector('h3')!.textContent).toBe('Fixed');
    expect(host.querySelector('li strong')!.textContent).toBe('bold');
  });

  it('says so when the build carries no notes, and links to GitHub', async () => {
    await act(async () => root.render(<Shown shown={news([HEAD])} notes="" tab="notes" />));
    expect(host.textContent).toContain('KnightLoader could not load the release notes here.');
    expect(host.querySelector('a[href$="/releases/tag/v1.9.0"]')).not.toBeNull();
  });

  it('lists every change by page and dims the ones that were seen', async () => {
    await act(async () => root.render(<Shown shown={news([HEAD, RING], ['seed-ring'])} notes="" tab="changes" />));
    expect([...host.querySelectorAll('h3')].map((h) => h.textContent)).toEqual(['Overview', 'Settings · Torrents']);
    expect([...host.querySelectorAll('li')].map((li) => li.dataset.seen)).toEqual(['false', 'true']);
  });

  it('marks all as seen and shows all again', async () => {
    const shown = news([HEAD, RING], ['seed-ring']);
    await act(async () => root.render(<Shown shown={shown} notes="" tab="changes" />));
    await act(async () => button('Mark all as seen').click());
    expect(shown.markSeen).toHaveBeenLastCalledWith(['overview-head', 'seed-ring']);
    await act(async () => button('Show all again').click());
    expect(shown.markSeen).toHaveBeenLastCalledWith(['overview-head', 'seed-ring'], false);
  });

  it('goes to the page of a change and closes', async () => {
    await act(async () => root.render(<Shown shown={news([RING])} notes="" tab="changes" />));
    await act(async () => button('Go there').click());
    expect(host.querySelector('[data-here]')!.textContent).toBe('/settings/torrents');
    expect(onClose).toHaveBeenCalled();
  });

  it('lists a change of a whole section under the section and goes there', async () => {
    await act(async () => root.render(<Shown shown={news([SEARCH])} notes="" tab="changes" />));
    expect(host.querySelector('h3')!.textContent).toBe('Settings');
    await act(async () => button('Go there').click());
    expect(host.querySelector('[data-here]')!.textContent).toBe('/settings');
  });

  it('has no list where the version marked nothing', async () => {
    await act(async () => root.render(<Shown shown={news([])} notes="Notes." tab="changes" />));
    expect(host.querySelector('[role="tablist"]')).toBeNull();
    expect(host.textContent).toContain('Notes.');
  });
});

describe('a release body', () => {
  it('follows a link to a website only', async () => {
    await act(async () =>
      root.render(<ReleaseNotes text={'See [the log](https://example.org/log) and [this](javascript:alert(1)).'} />),
    );
    expect([...host.querySelectorAll('a')].map((a) => a.getAttribute('href'))).toEqual(['https://example.org/log']);
    expect(host.textContent).toContain('this');
  });

  it('draws a list, a heading and code', async () => {
    await act(async () => root.render(<ReleaseNotes text={'# Title\n- one `wg0`\n- two\n\nA paragraph.'} />));
    expect(host.querySelector('h3')!.textContent).toBe('Title');
    expect([...host.querySelectorAll('li')].map((li) => li.textContent)).toEqual(['one wg0', 'two']);
    expect(host.querySelector('code')!.textContent).toBe('wg0');
    expect(host.querySelector('p')!.textContent).toBe('A paragraph.');
  });
});
