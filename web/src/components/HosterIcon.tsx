import { useEffect, useState } from 'react';
import { hosterIconURL } from '../lib/api';
import { serviceMark } from '../lib/serviceMarks';

/**
 * RETRY_MS are the pauses before asking again after a failed icon. The
 * instance answers at once for an icon it has not fetched yet and fetches it
 * in the background, and an <img> cannot tell that answer from a host without
 * an icon, so every failure gets them. A real miss is answered from the
 * instance's cache, so asking again costs nothing.
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
 * mayHaveSiteIcon reports whether a bare hostname is a dotted name the
 * instance would look up. Task ids, buckets such as torrent-magnet and IP
 * addresses never get an icon, and asking would only log a failed load per
 * retry. A top-level domain always has a letter, which tells a name from an
 * IPv4 address.
 */
export function mayHaveSiteIcon(host: string): boolean {
  const labels = host.split('.');
  return (
    labels.length > 1 &&
    labels.every((label) => /^[a-z0-9-]+$/.test(label)) &&
    /[a-z]/.test(labels[labels.length - 1])
  );
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
  // Keyed by host: React keeps the retry state per element, not per URL.
  return <SiteIcon key={clean} host={clean} box={box} />;
}

function SiteIcon({ host, box }: { host: string; box: { width: number; height: number } }) {
  const [attempt, setAttempt] = useState(0);
  const [failed, setFailed] = useState(false);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    if (!failed || attempt >= RETRY_MS.length) return;
    const timer = window.setTimeout(() => {
      setAttempt((n) => n + 1);
      setFailed(false);
    }, RETRY_MS[attempt]);
    return () => window.clearTimeout(timer);
  }, [failed, attempt]);

  const src = hosterIconURL(host) + (attempt > 0 ? `&attempt=${attempt}` : '');
  // A retry keeps the monogram up until its image arrives.
  const showMonogram = failed || (attempt > 0 && !loaded);
  return (
    <span aria-hidden style={box} className="relative inline-flex shrink-0">
      {showMonogram && <Monogram host={host} box={box} />}
      {!failed && (
        <img
          key={attempt}
          src={src}
          alt=""
          aria-hidden
          loading="lazy"
          style={box}
          onLoad={() => setLoaded(true)}
          onError={() => setFailed(true)}
          className={`absolute inset-0 rounded-[var(--radius-control)] object-contain ${loaded ? '' : 'opacity-0'}`}
        />
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
