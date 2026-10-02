// Marks for the captcha services, which ship with the page because the
// instance's own icon fetch (internal/app/app_hostericons.go) finds nothing on
// their sites. Dashboard Icons has none of them, so each is the service's own
// favicon or logo. A mark drawn in dark ink sits on a light plate, or it would
// vanish on the dark theme.
import twoCaptcha from '../assets/marks/2captcha.svg';
import nineKw from '../assets/marks/9kw.png';
import antiCaptcha from '../assets/marks/anticaptcha.png';
import capMonster from '../assets/marks/capmonster.svg';
import capSolver from '../assets/marks/capsolver.png';
import deathByCaptcha from '../assets/marks/deathbycaptcha.png';

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

/** serviceMark is the bundled mark for a bare hostname or any domain above it. */
export function serviceMark(host: string): ServiceMark | undefined {
  for (let h = host; h.includes('.'); h = h.slice(h.indexOf('.') + 1)) {
    const mark = MARKS[h];
    if (mark) return mark;
  }
  return undefined;
}
