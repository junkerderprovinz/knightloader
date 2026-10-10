// The glowing dots that mark what an update changed (GlimStone, "First start
// and what's new"). A changed component carries `data-new` with the id of its
// change, and a dot is drawn over the page at its corner. A change that is on
// another page puts a dot on every link of the navigation that leads there,
// and one whose element the page does not draw at the moment, a card that is
// hidden or has nothing to show, puts its dot on the page's own corner.
//
// The dots live in one layer in <body> rather than inside the components, so
// marking a change costs its component one attribute and no layout.
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useLocation } from 'react-router-dom';
import { basePath } from '../../lib/basePath';
import { useT } from '../../lib/i18n';
import type { WhatsNew } from '../../lib/useWhatsNew';
import { isAt, sentenceOf, trail, type Change } from '../../lib/whatsNew';
import { useTooltip } from '../ui';

/** The navigation: the rail, the phone's bar and every tab strip whose tabs are links. */
const NAV_LINKS = 'nav a[href], [data-nav-rail] a[href], [role="tablist"] a[href]';

// A page's entrance, a tab's slide and a card's arrival move what the dots
// stand on without changing the document, so the dots follow for this long
// after anything did.
const SETTLE_MS = 700;

// A page that loads its data draws its cards a moment after it is opened. Only
// after this long does a change count as not drawn there.
const ARRIVED_MS = 1200;

/** How far inside the corner of its element a dot's centre stands. */
const INSET = 4;
/** The same for a page's corner and for the edge a dot waits at. */
const EDGE_INSET = 12;
/** How far apart two dots stand that would share a corner. */
const APART = 20;

interface Target {
  key: string;
  el: Element;
  /** `change` stands on the changed element, `page` on the page that does not draw it, `way` on a link. */
  kind: 'change' | 'page' | 'way';
  changes: Change[];
}

const drawn = (el: Element): boolean => el.getClientRects().length > 0;

function navLinks(): Map<string, Element[]> {
  const base = basePath();
  const links = new Map<string, Element[]>();
  for (const a of document.querySelectorAll<HTMLAnchorElement>(NAV_LINKS)) {
    if (!drawn(a)) continue;
    let path = new URL(a.href).pathname;
    if (base !== '' && path.startsWith(base)) path = path.slice(base.length);
    path = path.replace(/\/+$/, '') || '/';
    links.set(path, [...(links.get(path) ?? []), a]);
  }
  return links;
}

const wayKeys = new WeakMap<Element, string>();
let wayCount = 0;

function wayKey(el: Element): string {
  let key = wayKeys.get(el);
  if (!key) wayKeys.set(el, (key = `way:${wayCount++}`));
  return key;
}

/** collect finds where each unseen change is marked: on its element, on its page or on the links that lead there. */
export function collect(unseen: Change[], here: string, arrived: boolean): Target[] {
  const marked = new Map<string, Element>();
  for (const el of document.querySelectorAll('[data-new]')) {
    const id = el.getAttribute('data-new') ?? '';
    if (!marked.has(id) && drawn(el)) marked.set(id, el);
  }
  const links = navLinks();
  const page = document.querySelector('[data-settings-content]') ?? document.querySelector('main');

  const targets: Target[] = [];
  const ways = new Map<Element, Change[]>();
  for (const change of unseen) {
    const el = marked.get(change.id);
    if (el) {
      targets.push({ key: `change:${change.id}`, el, kind: 'change', changes: [change] });
    } else if (isAt(change, here)) {
      if (arrived && page) targets.push({ key: `page:${change.id}`, el: page, kind: 'page', changes: [change] });
    } else {
      // The first address with a link on its way: a page hidden from the rail
      // is reached through its settings tile instead.
      for (const to of change.to) {
        const on = trail(to, here).flatMap((step) => links.get(step) ?? []);
        if (on.length === 0) continue;
        for (const link of on) ways.set(link, [...(ways.get(link) ?? []), change]);
        break;
      }
    }
  }
  for (const [el, changes] of ways) targets.push({ key: wayKey(el), el, kind: 'way', changes });
  return targets;
}

function same(a: Target[], b: Target[]): boolean {
  return (
    a.length === b.length &&
    a.every((x, i) => {
      const y = b[i];
      return (
        x.key === y.key &&
        x.el === y.el &&
        x.changes.length === y.changes.length &&
        x.changes.every((c, j) => c.id === y.changes[j].id)
      );
    })
  );
}

interface Clip {
  box: Element;
  /** It scrolls up and down, so what it cuts off can be brought into view. */
  scrolls: boolean;
}

// The boxes that cut off what lies outside them, by element. Looked up once,
// since the answer is the same on every frame a dot is placed.
const clips = new WeakMap<Element, Clip[]>();

function clipsOf(el: Element): Clip[] {
  let found = clips.get(el);
  if (!found) {
    found = [];
    for (let up = el.parentElement; up && up !== document.body; up = up.parentElement) {
      const { overflowX, overflowY } = getComputedStyle(up);
      if (overflowX !== 'visible' || overflowY !== 'visible') {
        found.push({ box: up, scrolls: overflowY === 'auto' || overflowY === 'scroll' });
      }
    }
    clips.set(el, found);
  }
  return found;
}

/**
 * spot is where the dot of an element stands, or null while its corner cannot
 * be seen. A corner that only has to be scrolled to keeps its dot, at the edge
 * it lies beyond, so a page the dots led to never arrives without one.
 */
function spot(target: Target, rtl: boolean, top: Element | null): { x: number; y: number } | null {
  const r = target.el.getBoundingClientRect();
  if (r.width === 0 && r.height === 0) return null;
  // A window covers the page, and only what is inside it can be pointed at.
  if (top !== null && !top.contains(target.el)) return null;
  const inset = target.kind === 'page' ? EDGE_INSET : INSET;
  const x = rtl ? r.left + inset : r.right - inset;
  let y = r.top + inset;
  for (const { box, scrolls } of clipsOf(target.el)) {
    const c = box.getBoundingClientRect();
    if (x < c.left || x > c.right) return null;
    if (y >= c.top && y <= c.bottom) continue;
    if (!scrolls) return null;
    y = y < c.top ? c.top + EDGE_INSET : c.bottom - EDGE_INSET;
  }
  return { x, y };
}

function Dot({
  target,
  news,
  register,
}: {
  target: Target;
  news: WhatsNew;
  register: (key: string, el: HTMLElement | null) => void;
}) {
  const { t } = useT();
  const way = target.kind === 'way';
  const heading = way ? t('whatsnew.way', { n: target.changes.length }) : '';
  const bubble: ReactNode = (
    <span className="flex flex-col gap-1">
      {way && <strong className="font-semibold">{heading}</strong>}
      {target.changes.map((c) => (
        <span key={c.id}>{sentenceOf(t, c)}</span>
      ))}
      {!way && <span className="text-carbon-textMuted">{t('whatsnew.seenHint')}</span>}
    </span>
  );
  const tip = useTooltip<HTMLButtonElement>(bubble);
  const { role: _tipRole, tabIndex: _tipTabIndex, ref: tipRef, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        ref={(el) => {
          tipRef.current = el;
          register(target.key, el);
        }}
        {...tipHoverProps}
        aria-label={way ? heading : sentenceOf(t, target.changes[0])}
        data-new-dot={target.kind}
        onClick={() => {
          // A dot on a link goes where the link goes; any other one has been read.
          if (way) (target.el as HTMLElement).click();
          else news.markSeen(target.changes.map((c) => c.id));
        }}
        className="pointer-events-auto absolute grid h-5 w-5 -translate-x-1/2 -translate-y-1/2 cursor-pointer place-items-center rounded-[var(--radius-pill)]"
      >
        {/* The glow breathes on the engine's pulse and holds still where the
            motion setting or the system asks for that. */}
        <span
          aria-hidden
          className="glim-live absolute h-4 w-4 rounded-[var(--radius-pill)] bg-statusOkSolid/60 blur-[3px]"
        />
        <span
          aria-hidden
          className="relative h-3 w-3 rounded-[var(--radius-pill)] bg-statusOkSolid shadow-[0_0_0_2px_var(--carbon-bg)]"
        />
      </button>
      {tip.node}
    </>
  );
}

/** NewDots draws a dot for every unseen change, where it is or on the way to it. */
export function NewDots({ news }: { news: WhatsNew }) {
  const here = useLocation().pathname.replace(/\/+$/, '') || '/';
  const [targets, setTargets] = useState<Target[]>([]);
  const layer = useRef<HTMLDivElement>(null);
  const dots = useRef(new Map<string, HTMLElement>());
  const live = useRef(targets);
  live.current = targets;
  const { unseen } = news;
  const on = news.dots && unseen.length > 0;

  const placeAll = () => {
    const rtl = document.documentElement.dir === 'rtl';
    const windows = document.querySelectorAll('[aria-modal="true"]');
    const top = windows.length > 0 ? windows[windows.length - 1] : null;
    const taken: { x: number; y: number }[] = [];
    for (const target of live.current) {
      const dot = dots.current.get(target.key);
      if (!dot) continue;
      const at = spot(target, rtl, top);
      dot.style.display = at ? '' : 'none';
      if (!at) continue;
      // Two changes on one corner stand side by side, so each can be pointed at.
      while (taken.some((p) => Math.abs(p.x - at.x) < APART && Math.abs(p.y - at.y) < APART)) {
        at.x += rtl ? APART : -APART;
      }
      taken.push(at);
      dot.style.left = `${at.x}px`;
      dot.style.top = `${at.y}px`;
    }
  };

  useEffect(() => {
    if (!on) {
      setTargets([]);
      return;
    }
    let arrived = false;
    let frame = 0;
    let until = 0;
    const refresh = () => {
      frame = 0;
      const next = collect(unseen, here, arrived);
      if (same(next, live.current)) placeAll();
      else setTargets(next);
      if (performance.now() < until) frame = requestAnimationFrame(refresh);
    };
    const wake = () => {
      until = performance.now() + SETTLE_MS;
      if (!frame) frame = requestAnimationFrame(refresh);
    };
    // The dots' own layer changes with every answer and must not ask again.
    const changed = new MutationObserver((records) => {
      if (records.some((r) => !layer.current?.contains(r.target))) wake();
    });
    changed.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ['class', 'hidden'],
    });
    document.addEventListener('scroll', wake, true);
    window.addEventListener('resize', wake);
    const arrival = window.setTimeout(() => {
      arrived = true;
      wake();
    }, ARRIVED_MS);
    wake();
    return () => {
      changed.disconnect();
      document.removeEventListener('scroll', wake, true);
      window.removeEventListener('resize', wake);
      window.clearTimeout(arrival);
      cancelAnimationFrame(frame);
    };
    // placeAll reads refs only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [on, unseen, here]);

  // Before paint, so a dot never shows at the layer's corner first.
  useLayoutEffect(placeAll);

  if (!on) return null;
  return createPortal(
    // Under the windows and the panels, which cover the page and its dots alike.
    <div ref={layer} className="pointer-events-none fixed left-0 top-0 z-30 h-0 w-0">
      {targets.map((target) => (
        <Dot
          key={target.key}
          target={target}
          news={news}
          register={(key, el) => {
            if (el) dots.current.set(key, el);
            else dots.current.delete(key);
          }}
        />
      ))}
    </div>,
    document.body,
  );
}
