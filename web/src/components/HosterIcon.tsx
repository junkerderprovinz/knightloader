import { useState } from 'react';
import { hosterIconURL } from '../lib/api';

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
 * HosterIcon draws a host's favicon, or a monogram tile of the same size when
 * there is none, so names line up either way.
 */
export function HosterIcon({ host, size = 18 }: { host: string; size?: number }) {
  const [failed, setFailed] = useState(false);
  const box = { width: size, height: size };
  const clean = hostOf(host);

  if (failed || !clean) {
    return (
      <span
        aria-hidden
        style={box}
        className="inline-flex shrink-0 items-center justify-center rounded-[var(--radius-control)]
          bg-carbon-surface3 text-[11px] font-semibold uppercase text-carbon-textMuted"
      >
        {clean.charAt(0) || '?'}
      </span>
    );
  }
  return (
    // Keyed by host: React keeps the failed state per element, not per URL.
    <img
      key={clean}
      src={hosterIconURL(clean)}
      alt=""
      aria-hidden
      loading="lazy"
      style={box}
      onError={() => setFailed(true)}
      className="inline-block shrink-0 rounded-[var(--radius-control)] object-contain"
    />
  );
}
