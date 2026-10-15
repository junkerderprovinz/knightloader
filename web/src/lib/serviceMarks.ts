// Marks that ship with the page. HosterIcon draws one when a host has it and
// asks the instance's own icon fetch (internal/app/app_hostericons.go) only for
// the rest. Fetching from several hundred hoster sites per instance fails too
// often to be the only source: sites block it, sit behind challenge pages or
// are briefly down.
//
// The logos in public/hosters are rebuilt with scripts/gen-hoster-logos.mjs,
// from the hoster logos collected from the sites and the captcha and debrid
// services' marks kept by hand in assets/marks.
import { withBase } from './basePath';
import { HOSTER_LOGOS } from './hosterLogos';
import type { LogoTile } from './logoTile';

export interface ServiceMark {
  src: string;
  /** The picture a lit tile shows the mark by, in its ink. */
  mask: string;
  info: LogoTile;
}

/** serviceMark is the bundled mark for a bare hostname or the nearest domain above it. */
export function serviceMark(host: string): ServiceMark | undefined {
  for (let h = host; h.includes('.'); h = h.slice(h.indexOf('.') + 1)) {
    const info = HOSTER_LOGOS.get(h);
    if (info) {
      return {
        src: withBase(`/hosters/${h}.png`),
        mask: withBase(`/hosters/${h}${info.lit ? '.lit' : ''}.png`),
        info,
      };
    }
  }
  return undefined;
}
