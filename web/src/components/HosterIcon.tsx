import { useEffect, useRef, useState } from 'react';
import { type HosterIconBatch, hosterIconURL, hosterIconsURL } from '../lib/api';
import { hueVars } from '../lib/appearance';
import { serviceMark } from '../lib/serviceMarks';

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
 * size, so names line up either way.
 */
export function HosterIcon({ host, size = 18 }: { host: string; size?: number }) {
  const clean = hostOf(host);
  const box = { width: size, height: size };
  const mark = serviceMark(clean);
  if (mark) {
    return (
      <img
        src={mark.src}
        alt=""
        aria-hidden
        loading="lazy"
        style={box}
        className={`inline-block shrink-0 rounded-[var(--radius-control)] object-contain ${
          mark.plate ? 'bg-white p-px' : ''
        }`}
      />
    );
  }
  if (!mayHaveSiteIcon(clean)) return <Monogram name={clean} box={box} />;
  // Keyed by host: React keeps the retry state per element, not per host.
  return <SiteIcon key={clean} host={clean} box={box} />;
}

/**
 * SiteIcon fetches the icon rather than pointing an <img> at it, because an
 * <img> fails the same way for "no icon" and for "not fetched yet" and only
 * the second is worth asking again. Until the first answer the box stays
 * empty, so a cached icon does not flash a monogram first.
 */
function SiteIcon({ host, box }: { host: string; box: { width: number; height: number } }) {
  const self = useRef<HTMLSpanElement>(null);
  const [inView, setInView] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [src, setSrc] = useState<string | null>();
  const waited = useRef(0);

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
  let sum = 0;
  for (const c of name) sum = (sum * 31 + c.charCodeAt(0)) >>> 0;
  return (
    <span
      aria-hidden
      style={{ ...box, ...hueVars(sum), fontSize: Math.max(9, Math.round(box.height * 0.55)) }}
      className="inline-flex shrink-0 items-center justify-center rounded-[var(--radius-control)]
        bg-[var(--item-hue-soft)] font-semibold uppercase leading-none text-carbon-text"
    >
      {name.charAt(0) || '?'}
    </span>
  );
}
