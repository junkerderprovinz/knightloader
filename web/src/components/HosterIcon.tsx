import { useEffect, useRef, useState, type CSSProperties } from 'react';
import { type HosterIconBatch, hosterIconURL, hosterIconsURL } from '../lib/api';
import { hueVars } from '../lib/appearance';
import { type LogoTile, logoTileOf, type Pixels, WHITE, withoutPlate } from '../lib/logoTile';
import { serviceMark } from '../lib/serviceMarks';

/** The colours a picker's tile lights up in, as CSS colours. */
export type Paint = Pick<LogoTile, 'tile' | 'ink'>;

/**
 * While the instance is still fetching an icon it says so with a pause to wait
 * and fetches in the background, a few dozen sites at a time, so a cold list
 * of a few hundred hosters takes a while to drain. A row asks again after the
 * pause the instance names, growing to RETRY_MAX_PAUSE_MS, and gives up once
 * it has waited RETRY_GIVE_UP_MS. A host without an icon is not asked again.
 */
export const RETRY_DEFAULT_MS = 5000;
export const RETRY_MAX_PAUSE_MS = 30_000;
export const RETRY_GIVE_UP_MS = 10 * 60_000;

function retryPause(retryAfterMs: number, attempt: number): number {
  return Math.max(retryAfterMs, Math.min(retryAfterMs * 1.5 ** attempt, RETRY_MAX_PAUSE_MS));
}

/**
 * hostOf reduces a host or full URL to a bare hostname, so the monogram does
 * not read "h" from "https" and one host shares one cached icon URL.
 */
function hostOf(raw: string): string {
  return raw
    .trim()
    .toLowerCase()
    .replace(/^https?:\/\//, '')
    .replace(/^[^@/]*@/, '')
    .split(/[/?#]/)[0]
    .split(':')[0]
    .replace(/^www\./, '');
}

/**
 * LOCAL_TLDS are the top-level names that exist only inside a network or in
 * examples. The instance refuses to fetch from them; localTLDs in
 * internal/app/app_hostericons.go is the same list.
 */
const LOCAL_TLDS = new Set([
  'alt',
  'arpa',
  'corp',
  'example',
  'home',
  'internal',
  'intranet',
  'invalid',
  'lan',
  'local',
  'localdomain',
  'localhost',
  'onion',
  'private',
  'test',
]);

/**
 * mayHaveSiteIcon reports whether a bare hostname is a public dotted name the
 * instance would look up. Task ids, buckets such as torrent-magnet, IP
 * addresses and names like nas.local never get an icon, and asking would only
 * cost a request per row. A top-level domain always has a letter, which tells
 * a name from an IPv4 address.
 */
export function mayHaveSiteIcon(host: string): boolean {
  const labels = host.split('.');
  const tld = labels[labels.length - 1];
  return (
    labels.length > 1 &&
    labels.every((label) => /^[a-z0-9-]+$/.test(label)) &&
    /[a-z]/.test(tld) &&
    !LOCAL_TLDS.has(tld)
  );
}

/**
 * An icon is the address an <img> shows, null for a host without one, or,
 * while the instance has no answer yet, the milliseconds to wait before asking
 * again.
 */
type Icon = string | null | number;

const icons = new Map<string, Promise<Icon>>();

/**
 * askForIcon asks once per host however many rows show it. A settled answer
 * is kept for the life of the page; an unsettled one is dropped, so the next
 * ask goes to the instance again.
 */
function askForIcon(host: string): Promise<Icon> {
  let icon = icons.get(host);
  if (!icon) {
    icon = new Promise<Icon>((resolve) => {
      waiting.set(host, resolve);
      if (!gathering) gathering = window.setTimeout(sendBatch, GATHER_MS);
    });
    icons.set(host, icon);
    void icon.then((settled) => {
      if (typeof settled === 'number') icons.delete(host);
    });
  }
  return icon;
}

/**
 * The hosts asked for within GATHER_MS go to the instance together, at most
 * BATCH to a request. A browser opens six connections to the instance, so a
 * request per tile would queue a screen of tiles, and every other request the
 * page makes, behind each other.
 */
const GATHER_MS = 16;
const BATCH = 64;
const waiting = new Map<string, (icon: Icon) => void>();
let gathering = 0;

function sendBatch() {
  gathering = 0;
  const hosts = [...waiting.keys()];
  for (let at = 0; at < hosts.length; at += BATCH) {
    const part = hosts.slice(at, at + BATCH);
    const answer = part.map((h) => [h, waiting.get(h)!] as const);
    void fetchIcons(part).then((found) => answer.forEach(([h, resolve]) => resolve(found(h))));
  }
  waiting.clear();
}

/** fetchIcons is a batch's answer per host. */
async function fetchIcons(hosts: string[]): Promise<(host: string) => Icon> {
  try {
    const r = await fetch(hosterIconsURL(hosts));
    if (!r.ok) throw new Error(String(r.status));
    const batch = (await r.json()) as HosterIconBatch;
    const pause = batch.retryAfter > 0 ? batch.retryAfter * 1000 : RETRY_DEFAULT_MS;
    return (host) => {
      if (batch.icons[host]) return batch.icons[host];
      // Too large to come inline, so the <img> loads it from the single route.
      if (batch.large.includes(host)) return hosterIconURL(host);
      if (batch.pending.includes(host)) return pause;
      // A host the instance left out of a long batch is asked again.
      return batch.none.includes(host) ? null : RETRY_DEFAULT_MS;
    };
  } catch {
    // The instance did not answer; that is worth another try, like a pending icon.
    return () => RETRY_DEFAULT_MS;
  }
}

/**
 * HosterIcon draws the mark bundled for a host or a domain above it, else the
 * icon the instance fetched from the site, else a monogram tile of the same
 * size, so names line up either way. On a brand tile (`lit`) a fetched icon
 * is read the way the generators read a bundled one, and `onPaint` hears its
 * colours.
 */
export function HosterIcon({
  host,
  size = 18,
  lit = false,
  onPaint,
}: {
  host: string;
  size?: number;
  lit?: boolean;
  onPaint?: (paint: Paint) => void;
}) {
  const clean = hostOf(host);
  const box = { width: size, height: size };
  const mark = serviceMark(clean);
  if (mark) return <Logo src={mark.src} mask={mark.mask} rest={mark.info.rest} box={box} lit={lit} />;
  if (!mayHaveSiteIcon(clean)) return <Monogram name={clean} box={box} />;
  // Keyed by host: React keeps the retry state per element, not per host.
  return <SiteIcon key={clean} host={clean} box={box} lit={lit} onPaint={onPaint} />;
}

/**
 * hostPaint is the colours a host's tile lights up in as far as the page
 * knows them without fetching: its bundled logo's, else its monogram's.
 */
export function hostPaint(host: string): Paint {
  const clean = hostOf(host);
  return serviceMark(clean)?.info ?? monogramPaint(clean);
}

/**
 * Logo draws a logo as its entry in a generated list says: in the text's
 * colour where it is drawn in one neutral ink, lifted or deepened where its
 * colour would vanish on one theme, and with `lit` the mask a lit tile
 * shows in its ink.
 */
export function Logo({
  src,
  mask,
  rest,
  box,
  lit,
}: {
  src: string;
  mask: string;
  rest?: LogoTile['rest'];
  box: { width: number; height: number };
  lit?: boolean;
}) {
  const ink = { '--logo-mask': `url("${mask}")` } as CSSProperties;
  const tone = rest === 'lift' ? 'glim-mark-lift' : rest === 'deepen' ? 'glim-mark-deepen' : '';
  return (
    <span aria-hidden style={box} className={`glim-logo ${tone}`}>
      {rest === 'ink' ? (
        <span className="glim-mark-rest glim-logo-ink" style={ink} />
      ) : (
        <img src={src} alt="" loading="lazy" className="glim-mark-rest block object-contain" />
      )}
      {lit && <span className="glim-mark-hover glim-logo-ink" style={ink} />}
    </span>
  );
}

interface ReadLogo {
  src: string;
  mask: string;
  info: LogoTile;
}

const read = new Map<string, Promise<ReadLogo | null>>();

/**
 * readIcon reads a fetched icon as a brand tile needs it, once per address,
 * or is null where the browser cannot draw it into a canvas.
 */
function readIcon(src: string): Promise<ReadLogo | null> {
  let logo = read.get(src);
  if (!logo) {
    logo = new Promise<HTMLImageElement>((resolve, reject) => {
      const image = new Image();
      image.onload = () => resolve(image);
      image.onerror = reject;
      image.src = src;
    })
      .then((image) => {
        // An SVG with a viewBox alone has no size of its own and is drawn square.
        const drawn = drawContained(image, image.naturalWidth || READ_SIZE, image.naturalHeight || READ_SIZE);
        if (!drawn) return null;
        const lifted = withoutPlate(drawn.pixels);
        const plain = lifted ? trimmed(lifted) : drawn;
        if (!plain) return null;
        const { info, lit } = logoTileOf(drawn.pixels, plain.pixels);
        return { src: plain.url, mask: (lit && urlOf(lit)) || plain.url, info };
      })
      .catch(() => null);
    read.set(src, logo);
  }
  return logo;
}

const READ_SIZE = 64;

/** drawContained draws a picture into a canvas of READ_SIZE, whole and centred. */
function drawContained(source: CanvasImageSource, width: number, height: number) {
  const canvas = document.createElement('canvas');
  canvas.width = canvas.height = READ_SIZE;
  const context = canvas.getContext('2d', { willReadFrequently: true });
  if (!context || !width || !height) return null;
  const scale = READ_SIZE / Math.max(width, height);
  const [w, h] = [width * scale, height * scale];
  context.drawImage(source, (READ_SIZE - w) / 2, (READ_SIZE - h) / 2, w, h);
  const { data } = context.getImageData(0, 0, READ_SIZE, READ_SIZE);
  return { pixels: { width: READ_SIZE, height: READ_SIZE, data }, url: canvas.toDataURL() };
}

/** trimmed is a logo lifted off its plate, drawn to the full size again. */
function trimmed(px: Pixels) {
  let [minX, minY, maxX, maxY] = [px.width, px.height, -1, -1];
  for (let i = 0; i < px.width * px.height; i++) {
    if (px.data[i * 4 + 3] === 0) continue;
    const [x, y] = [i % px.width, Math.floor(i / px.width)];
    [minX, minY, maxX, maxY] = [Math.min(minX, x), Math.min(minY, y), Math.max(maxX, x), Math.max(maxY, y)];
  }
  const canvas = canvasOf(px);
  if (!canvas || maxX < 0) return null;
  const crop = document.createElement('canvas');
  [crop.width, crop.height] = [maxX - minX + 1, maxY - minY + 1];
  crop.getContext('2d')?.drawImage(canvas, -minX, -minY);
  return drawContained(crop, crop.width, crop.height);
}

function canvasOf(px: Pixels): HTMLCanvasElement | null {
  const canvas = document.createElement('canvas');
  [canvas.width, canvas.height] = [px.width, px.height];
  const context = canvas.getContext('2d');
  if (!context) return null;
  context.putImageData(new ImageData(px.data, px.width, px.height), 0, 0);
  return canvas;
}

const urlOf = (px: Pixels) => canvasOf(px)?.toDataURL();

/**
 * SiteIcon fetches the icon rather than pointing an <img> at it, because an
 * <img> fails the same way for "no icon" and for "not fetched yet" and only
 * the second is worth asking again. Until the first answer the box stays
 * empty, so a cached icon does not flash a monogram first.
 */
function SiteIcon({
  host,
  box,
  lit,
  onPaint,
}: {
  host: string;
  box: { width: number; height: number };
  lit: boolean;
  onPaint?: (paint: Paint) => void;
}) {
  const self = useRef<HTMLSpanElement>(null);
  const [inView, setInView] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [src, setSrc] = useState<string | null>();
  const [logo, setLogo] = useState<ReadLogo | null>(null);
  const waited = useRef(0);
  const painted = useRef(onPaint);
  painted.current = onPaint;

  // Only rows in view or about to scroll into it ask, which a long hoster
  // list needs.
  useEffect(() => {
    const seen = new IntersectionObserver(
      (entries) => {
        if (!entries.some((e) => e.isIntersecting)) return;
        seen.disconnect();
        setInView(true);
      },
      { rootMargin: '200px' },
    );
    seen.observe(self.current!);
    return () => seen.disconnect();
  }, []);

  useEffect(() => {
    if (!inView) return;
    let live = true;
    let timer = 0;
    void askForIcon(host).then((icon) => {
      if (!live) return;
      if (typeof icon !== 'number') {
        setSrc(icon);
        return;
      }
      setSrc(null);
      const pause = retryPause(icon, attempt);
      if (waited.current + pause > RETRY_GIVE_UP_MS) return;
      waited.current += pause;
      timer = window.setTimeout(() => setAttempt((n) => n + 1), pause);
    });
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [inView, host, attempt]);

  useEffect(() => {
    if (!lit || !src) return;
    let live = true;
    void readIcon(src).then((found) => {
      if (!live || !found) return;
      setLogo(found);
      painted.current?.(found.info);
    });
    return () => {
      live = false;
    };
  }, [lit, src]);

  if (logo) return <Logo src={logo.src} mask={logo.mask} rest={logo.info.rest} box={box} lit />;
  return (
    <span ref={self} aria-hidden style={box} className="inline-flex shrink-0">
      {src ? (
        <img
          src={src}
          alt=""
          aria-hidden
          style={box}
          onError={() => setSrc(null)}
          className="rounded-[var(--radius-control)] object-contain"
        />
      ) : (
        src === null && <Monogram name={host} box={box} />
      )}
    </span>
  );
}

/**
 * Monogram stands in for a logo: the name's first letter on a tile tinted with
 * a colour of the palette picked by the name, so a list of hosters without
 * icons still reads as a set of marks, each host always in the same colour.
 */
export function Monogram({ name, box }: { name: string; box: { width: number; height: number } }) {
  return (
    <span
      aria-hidden
      style={{ ...box, ...hueVars(monogramHue(name)), fontSize: Math.max(9, Math.round(box.height * 0.55)) }}
      className="glim-monogram inline-flex shrink-0 items-center justify-center rounded-[var(--radius-control)]
        bg-[var(--item-hue-soft)] font-semibold uppercase leading-none text-carbon-text"
    >
      {name.charAt(0) || '?'}
    </span>
  );
}

function monogramHue(name: string): number {
  let sum = 0;
  for (const c of name) sum = (sum * 31 + c.charCodeAt(0)) >>> 0;
  return sum;
}

/**
 * monogramPaint lights a brand without a logo in a colour of its own, picked
 * by its name from around the colour wheel rather than from the palette, which
 * may hold the accent. White holds on every hue at this lightness.
 */
export function monogramPaint(name: string): Paint {
  return { tile: `hsl(${monogramHue(name) % 360} 55% 38%)`, ink: WHITE };
}
