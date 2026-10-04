import { useEffect, useRef, useState } from 'react';
import { hosterIconURL } from '../lib/api';
import { serviceMark } from '../lib/serviceMarks';

/**
 * RETRY_MS are the pauses before asking again while the instance is still
 * fetching an icon. It answers 202 at once and fetches in the background; a
 * host without an icon gets a 204 and is not asked again.
 */
export const RETRY_MS = [3000, 10000, 30000];

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
 * An icon is an object URL, null for a host without one, or undefined while
 * the instance has no answer yet.
 */
type Icon = string | null | undefined;

const icons = new Map<string, Promise<Icon>>();

/**
 * askForIcon makes one request per host however many rows show it. A settled
 * answer is kept for the life of the page; an unsettled one is dropped, so the
 * next ask goes to the instance again.
 */
function askForIcon(host: string): Promise<Icon> {
  let icon = icons.get(host);
  if (!icon) {
    icon = fetchIcon(host);
    icons.set(host, icon);
    void icon.then((settled) => {
      if (settled === undefined) icons.delete(host);
    });
  }
  return icon;
}

async function fetchIcon(host: string): Promise<Icon> {
  try {
    const r = await fetch(hosterIconURL(host));
    if (r.status === 202) return undefined;
    const body = r.status === 200 ? await r.blob() : null;
    return body?.size ? URL.createObjectURL(body) : null;
  } catch {
    // The instance did not answer; that is worth another try, like a 202.
    return undefined;
  }
}

/**
 * HosterIcon draws a service's bundled mark, else the host's favicon, else a
 * monogram tile of the same size, so names line up either way.
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
        style={box}
        className={`inline-block shrink-0 rounded-[var(--radius-control)] object-contain ${
          mark.plate ? 'bg-white p-px' : ''
        }`}
      />
    );
  }
  if (!mayHaveSiteIcon(clean)) return <Monogram host={clean} box={box} />;
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

  // Only rows scrolled into view ask, which a long hoster list needs.
  useEffect(() => {
    const seen = new IntersectionObserver((entries) => {
      if (!entries.some((e) => e.isIntersecting)) return;
      seen.disconnect();
      setInView(true);
    });
    seen.observe(self.current!);
    return () => seen.disconnect();
  }, []);

  useEffect(() => {
    if (!inView) return;
    let live = true;
    let timer = 0;
    void askForIcon(host).then((icon) => {
      if (!live) return;
      setSrc(icon ?? null);
      if (icon === undefined && attempt < RETRY_MS.length) {
        timer = window.setTimeout(() => setAttempt((n) => n + 1), RETRY_MS[attempt]);
      }
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
        src === null && <Monogram host={host} box={box} />
      )}
    </span>
  );
}

function Monogram({ host, box }: { host: string; box: { width: number; height: number } }) {
  return (
    <span
      aria-hidden
      style={box}
      className="inline-flex shrink-0 items-center justify-center rounded-[var(--radius-control)]
        bg-carbon-surface3 text-[11px] font-semibold uppercase text-carbon-textMuted"
    >
      {host.charAt(0) || '?'}
    </span>
  );
}
