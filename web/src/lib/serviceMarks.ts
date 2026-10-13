// Marks that ship with the page. HosterIcon draws one when a host has it and
// asks the instance's own icon fetch (internal/app/app_hostericons.go) only for
// the rest. Fetching from several hundred hoster sites per instance fails too
// often to be the only source: sites block it, sit behind challenge pages or
// are briefly down.
//
// The hoster logos in public/hosters were collected from the sites and are
// rebuilt with scripts/gen-hoster-logos.mjs. The captcha services are kept by
// hand, because their sites give a fetch nothing and Dashboard Icons has none
// of them, so each is the service's own favicon or logo. A captcha mark drawn
// in dark ink sits on a light plate, or it would vanish on the dark theme; the
// script bakes that plate into a hoster logo.
import twoCaptcha from '../assets/marks/2captcha.svg';
import nineKw from '../assets/marks/9kw.png';
import antiCaptcha from '../assets/marks/anticaptcha.png';
import capMonster from '../assets/marks/capmonster.svg';
import capSolver from '../assets/marks/capsolver.png';
import deathByCaptcha from '../assets/marks/deathbycaptcha.png';
import { withBase } from './basePath';
import { HOSTER_LOGOS } from './hosterLogos';

export interface ServiceMark {
  src: string;
  plate?: boolean;
}

// Keyed by the service's own domain, which also covers the dashboard subdomain
// a catalogue entry links to (dash.capmonster.cloud).
const MARKS: Record<string, ServiceMark> = {
  '2captcha.com': { src: twoCaptcha },
  'anti-captcha.com': { src: antiCaptcha },
  'capmonster.cloud': { src: capMonster },
  'capsolver.com': { src: capSolver, plate: true },
  '9kw.eu': { src: nineKw, plate: true },
  'deathbycaptcha.com': { src: deathByCaptcha },
};

/** serviceMark is the bundled mark for a bare hostname or the nearest domain above it. */
export function serviceMark(host: string): ServiceMark | undefined {
  for (let h = host; h.includes('.'); h = h.slice(h.indexOf('.') + 1)) {
    const mark = MARKS[h];
    if (mark) return mark;
    if (HOSTER_LOGOS.has(h)) return { src: withBase(`/hosters/${h}.png`) };
  }
  return undefined;
}
