import { useState } from 'react';
import { hosterIconURL } from '../lib/api';

/**
 * The little site icon in front of a hoster's name (jdp, 2026-09-05: "Bei
 * allen Hostern bzw. Accounts soll das logo mit in der liste sein. wie bei
 * JD"), with a monogram for every host that has none.
 *
 * The monogram is not a placeholder waiting for a real icon - it IS the answer
 * for a host whose favicon cannot be fetched, and it has to look deliberate:
 * the same rounded tile at the same size, in the furniture shade, with the
 * host's own first letter. A row that falls back to nothing would leave the
 * name jumping left by 20px depending on whether some other server answered.
 *
 * `key={host}` on the image matters: a table re-rendering with different rows
 * would otherwise keep one <img> element and only swap its src, and React
 * keeps the failed state of the element, not of the URL - so one host without
 * an icon would make every host after it draw a monogram too.
 */
/**
 * hostOf reduces whatever a row happens to carry to a bare hostname.
 *
 * The server normalises this too, and has to: the string reaches it from
 * stored settings, and it ends up in a URL and a filename. This is not that
 * check. It is here because a caller passes a full URL (a catalogue service's
 * own "where do I get a key" link) and two things on this side read the value
 * directly: the monogram, which would otherwise print "h" from "https" for
 * every host without an icon, and the request URL, which would otherwise carry
 * a different query string per row for the same host and lose the browser's
 * own cache along with it.
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
          bg-carbon-surface3 text-[10px] font-semibold uppercase text-carbon-textMuted"
      >
        {clean.charAt(0) || '?'}
      </span>
    );
  }
  return (
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
