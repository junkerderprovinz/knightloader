// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import {
  HosterIcon,
  mayHaveSiteIcon,
  RETRY_DEFAULT_MS,
  RETRY_GIVE_UP_MS,
  RETRY_MAX_PAUSE_MS,
} from './HosterIcon';
import { HOSTER_LOGOS } from '../lib/hosterLogos';
import { serviceMark } from '../lib/serviceMarks';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let inView: boolean;
let retryAfter: string;
const observers = new Set<() => void>();

// Every row starts in view unless a test says otherwise; scrolled() brings the
// rest in.
class FakeIntersectionObserver {
  private readonly fire: () => void;
  constructor(callback: (entries: { isIntersecting: boolean }[]) => void) {
    this.fire = () => callback([{ isIntersecting: true }]);
  }
  observe() {
    if (inView) queueMicrotask(this.fire);
    else observers.add(this.fire);
  }
  disconnect() {
    observers.delete(this.fire);
  }
}

beforeEach(() => {
  inView = true;
  retryAfter = '5';
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  observers.clear();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const img = () => host.querySelector('img');

/**
 * answers stubs fetch with one status per request, the last one repeating,
 * which every host of that request's batch gets: 200 for an icon, 204 for
 * none and 202 for one still being fetched, with the instance's pause unless
 * a test sets retryAfter.
 */
function answers(...statuses: number[]) {
  const fetch = vi.fn(async (url: string) => {
    const status = statuses.length > 1 ? statuses.shift()! : statuses[0];
    const hosts = new URL(url, 'http://kl').searchParams.getAll('host');
    const batch = {
      icons: status === 200 ? Object.fromEntries(hosts.map((h) => [h, ICON])) : {},
      large: [],
      none: status === 204 ? hosts : [],
      pending: status === 202 ? hosts : [],
      retryAfter: Number(retryAfter) || 0,
    };
    return new Response(JSON.stringify(batch), { status: 200 });
  });
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

const ICON = 'data:image/png;base64,aWNvbg==';

/**
 * settle lets a row that has come into view ask, and then the hosts asked
 * together go out as one batch.
 */
async function settle() {
  await act(() => vi.advanceTimersByTimeAsync(0));
  await act(() => vi.advanceTimersByTimeAsync(20));
}

/**
 * wait lets time pass a second per act, so React renders the ask a timer
 * schedules before the next timer is due.
 */
async function wait(ms: number) {
  for (let left = ms; left > 0; left -= 1000) {
    await act(() => vi.advanceTimersByTimeAsync(Math.min(1000, left)));
  }
}

it('draws a captcha service from its bundled mark without asking the instance', () => {
  act(() => root.render(<HosterIcon host="https://dash.capmonster.cloud/" />));
  expect(img()?.getAttribute('src')).toBe(serviceMark('capmonster.cloud')?.src);
  expect(img()?.getAttribute('src')).not.toContain('/api/hosters/icon');
});

it('draws a mark of one dark ink in the text colour rather than on a plate', () => {
  act(() => root.render(<HosterIcon host="https://www.9kw.eu/userapi.html" />));
  expect(img()).toBeNull();
  const ink = host.querySelector<HTMLElement>('.glim-logo-ink.glim-mark-rest');
  expect(ink?.style.getPropertyValue('--logo-mask')).toBe('url("/hosters/9kw.eu.png")');
  act(() => root.render(<HosterIcon host="https://2captcha.com/enterpage" />));
  expect(img()?.getAttribute('src')).toBe('/hosters/2captcha.com.png');
});

it('brings the mask a lit tile shows only onto a tile', () => {
  const lit = [...HOSTER_LOGOS].find(([, info]) => info.lit && info.rest !== 'ink')![0];
  act(() => root.render(<HosterIcon host={lit} />));
  expect(host.querySelector('.glim-mark-hover')).toBeNull();
  act(() => root.render(<HosterIcon host={lit} lit />));
  const mask = host.querySelector<HTMLElement>('.glim-mark-hover.glim-logo-ink');
  expect(mask?.style.getPropertyValue('--logo-mask')).toBe(`url("/hosters/${lit}.lit.png")`);
});

it('knows every captcha service by the host of its catalogue link', () => {
  for (const link of [
    'https://2captcha.com/enterpage',
    'https://anti-captcha.com/clients/settings/apisetup',
    'https://dash.capmonster.cloud/',
    'https://dashboard.capsolver.com/',
    'https://www.9kw.eu/userapi.html',
    'https://deathbycaptcha.com/user-pay',
  ]) {
    expect(serviceMark(new URL(link).hostname), link).toBeDefined();
  }
  expect(serviceMark('katfile.com')).toBeUndefined();
});

it('knows a debrid service whose site offers no sharp icon by the host of its catalogue link', () => {
  for (const link of [
    'https://alldebrid.com/apikeys',
    'https://bestdebrid.com/profile',
    'https://members.cocoleech.com/settings',
    'https://www.debriditalia.com/',
    'https://www.deepbrid.com/devices',
    'https://fakirdebrid.net/api/login.php',
    'https://linksnappy.com/myaccount',
    'https://multiup.io/en/login',
    'https://neodebrid.com/login',
    'https://premium.rpnet.biz/account',
    'https://www.zevera.com/account',
  ]) {
    expect(serviceMark(new URL(link).hostname.replace(/^www\./, '')), link).toBeDefined();
  }
});

it('draws a hoster from its bundled logo without asking the instance', () => {
  const fetch = answers(200);
  act(() => root.render(<HosterIcon host="https://rapidgator.net/file/1" />));
  expect(img()?.getAttribute('src')).toBe('/hosters/rapidgator.net.png');
  expect(img()?.getAttribute('loading')).toBe('lazy');
  expect(fetch).not.toHaveBeenCalled();
});

it('takes a bundled logo from the domain above when the subdomain has none', () => {
  expect(serviceMark('hdmusic.takefile.link')?.src).toBe(serviceMark('takefile.link')?.src);
  expect(serviceMark('files.takefile.link')?.src).not.toBe(serviceMark('takefile.link')?.src);
  expect(serviceMark('www.rapidgator.net')?.src).toBe(serviceMark('rapidgator.net')?.src);
});

it('draws the icon the instance has', async () => {
  vi.useFakeTimers();
  const fetch = answers(200);
  act(() => root.render(<HosterIcon host="https://rapidfileshare.net/file/1" />));
  await settle();
  expect(fetch).toHaveBeenCalledWith('/api/hosters/icons?host=rapidfileshare.net');
  expect(img()?.getAttribute('src')).toBe(ICON);
});

it('takes a 204 as no icon and does not ask again', async () => {
  vi.useFakeTimers();
  const fetch = answers(204);
  act(() =>
    root.render(
      <>
        <HosterIcon host="novafile.org" />
        <HosterIcon host="https://novafile.org/view/2" />
      </>,
    ),
  );
  await settle();
  expect(img()).toBeNull();
  expect(host.textContent).toBe('nn');

  await wait(RETRY_GIVE_UP_MS);
  expect(fetch).toHaveBeenCalledTimes(1);
});

it('shows a monogram while the instance fetches the icon and asks again later', async () => {
  vi.useFakeTimers();
  const fetch = answers(202, 200);
  act(() => root.render(<HosterIcon host="katfile.com" />));
  await settle();
  expect(img()).toBeNull();
  expect(host.textContent).toBe('k');

  await act(() => vi.advanceTimersByTimeAsync(5000));
  await settle();
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(img()?.getAttribute('src')).toBe(ICON);
});

it('waits as long as the instance asks before asking again', async () => {
  vi.useFakeTimers();
  retryAfter = '20';
  const fetch = answers(202, 200);
  act(() => root.render(<HosterIcon host="uploadgig.com" />));
  await settle();

  await act(() => vi.advanceTimersByTimeAsync(19_900));
  await settle();
  expect(fetch).toHaveBeenCalledTimes(1);
  await act(() => vi.advanceTimersByTimeAsync(100));
  await settle();
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(img()?.getAttribute('src')).toBe(ICON);
});

it('keeps asking while a long queue of icons drains on the instance', async () => {
  vi.useFakeTimers();
  const fetch = answers(...Array<number>(20).fill(202), 200);
  act(() => root.render(<HosterIcon host="fikper.com" />));
  await settle();
  expect(host.textContent).toBe('f');

  await wait(RETRY_GIVE_UP_MS);
  expect(fetch).toHaveBeenCalledTimes(21);
  expect(img()?.getAttribute('src')).toBe(ICON);
});

it('asks less often the longer an icon takes and gives up in the end', async () => {
  vi.useFakeTimers();
  const fetch = answers(202);
  act(() => root.render(<HosterIcon host="depositfiles.com" />));
  await settle();

  await wait(60_000);
  expect(fetch.mock.calls.length).toBeLessThanOrEqual(7);
  await wait(RETRY_GIVE_UP_MS);
  const asked = fetch.mock.calls.length;
  expect(asked).toBeLessThanOrEqual(RETRY_GIVE_UP_MS / RETRY_MAX_PAUSE_MS + 5);
  await wait(RETRY_GIVE_UP_MS);
  expect(fetch).toHaveBeenCalledTimes(asked);
  expect(img()).toBeNull();
  expect(host.textContent).toBe('d');
});

it('asks again after the instance did not answer at all', async () => {
  vi.useFakeTimers();
  const fetch = answers(200);
  fetch.mockRejectedValueOnce(new TypeError('Failed to fetch'));
  act(() => root.render(<HosterIcon host="mixshared.com" />));
  await settle();
  expect(host.textContent).toBe('m');

  await act(() => vi.advanceTimersByTimeAsync(RETRY_DEFAULT_MS));
  await settle();
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(img()?.getAttribute('src')).toBe(ICON);
});

it('asks only once the row is scrolled into view', async () => {
  vi.useFakeTimers();
  inView = false;
  const fetch = answers(200);
  act(() => root.render(<HosterIcon host="tezfiles.com" />));
  await settle();
  expect(fetch).not.toHaveBeenCalled();
  expect(host.textContent).toBe('');

  act(() => observers.forEach((fire) => fire()));
  await settle();
  expect(img()?.getAttribute('src')).toBe(ICON);
});

it('asks only for public dotted names', () => {
  for (const name of ['rapidgator.net', 'dl.free.fr', 'xn--80ak6aa92e.com', '1fichier.com', 'files.example.com']) {
    expect(mayHaveSiteIcon(name), name).toBe(true);
  }
  for (const name of [
    '',
    'localhost',
    'torrent-magnet',
    'torrent-upload',
    '3f9a0c41d2e8b7a6',
    '127.0.0.1',
    '127.0.0.81',
    '8.8.8.8',
    '[2001',
    '.example.com',
    'example.com.',
    'my_host.lan',
    'nas.local',
    'host.lan',
    'x.home.arpa',
    'a.example',
    'ci.test',
    'svc.internal',
    'box.localhost',
    'nope.invalid',
  ]) {
    expect(mayHaveSiteIcon(name), name).toBe(false);
  }
});

it('draws the monogram for hosts that cannot have an icon without asking the instance', () => {
  const fetch = answers(200);
  for (const raw of [
    'torrent-magnet',
    '3f9a0c41d2e8b7a6',
    '127.0.0.1:8080',
    'http://nas.local:5000/share',
    'http://[::1]/a',
    '2001:db8::1',
  ]) {
    act(() => root.render(<HosterIcon host={raw} />));
    expect(img(), raw).toBeNull();
  }
  expect(host.textContent).toBe('2');
  expect(fetch).not.toHaveBeenCalled();
});

it('asks for the icons of a screen of rows together', async () => {
  vi.useFakeTimers();
  const fetch = answers(200);
  act(() =>
    root.render(
      <>
        {Array.from({ length: 70 }, (_, i) => (
          <HosterIcon key={i} host={`host${i}.com`} />
        ))}
      </>,
    ),
  );
  await settle();
  // Two batches, the second for what the first had no room for.
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(host.querySelectorAll('img')).toHaveLength(70);
});

it('loads an icon too large for the batch from its own address', async () => {
  vi.useFakeTimers();
  vi.stubGlobal(
    'fetch',
    vi.fn(async () =>
      new Response(JSON.stringify({ icons: {}, large: ['pixiv.net'], none: [], pending: [], retryAfter: 5 }), { status: 200 }),
    ),
  );
  act(() => root.render(<HosterIcon host="pixiv.net" />));
  await settle();
  expect(img()?.getAttribute('src')).toBe('/api/hosters/icon?host=pixiv.net');
});
