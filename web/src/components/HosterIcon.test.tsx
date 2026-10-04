// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { HosterIcon, mayHaveSiteIcon, RETRY_MS } from './HosterIcon';
import { serviceMark } from '../lib/serviceMarks';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let inView: boolean;
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
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
  URL.createObjectURL = vi.fn(() => 'blob:icon');
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

/** answers stubs fetch with one status per request, the last one repeating. */
function answers(...statuses: number[]) {
  const fetch = vi.fn(async () => {
    const status = statuses.length > 1 ? statuses.shift()! : statuses[0];
    return { status, blob: async () => new Blob(status === 200 ? ['icon'] : []) } as Response;
  });
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

const settle = () => act(() => vi.advanceTimersByTimeAsync(0));

it('draws a captcha service from its bundled mark without asking the instance', () => {
  act(() => root.render(<HosterIcon host="https://dash.capmonster.cloud/" />));
  expect(img()?.getAttribute('src')).toBe(serviceMark('capmonster.cloud')?.src);
  expect(img()?.getAttribute('src')).not.toContain('/api/hosters/icon');
});

it('puts a mark drawn in dark ink on a light plate', () => {
  act(() => root.render(<HosterIcon host="https://www.9kw.eu/userapi.html" />));
  expect(img()?.className).toContain('bg-white');
  act(() => root.render(<HosterIcon host="https://2captcha.com/enterpage" />));
  expect(img()?.className).not.toContain('bg-white');
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
  expect(serviceMark('rapidgator.net')).toBeUndefined();
});

it('draws the icon the instance has', async () => {
  vi.useFakeTimers();
  const fetch = answers(200);
  act(() => root.render(<HosterIcon host="https://rapidgator.net/file/1" />));
  await settle();
  expect(fetch).toHaveBeenCalledWith('/api/hosters/icon?host=rapidgator.net');
  expect(img()?.getAttribute('src')).toBe('blob:icon');
});

it('takes a 204 as no icon and does not ask again', async () => {
  vi.useFakeTimers();
  const fetch = answers(204);
  act(() =>
    root.render(
      <>
        <HosterIcon host="nitroflare.com" />
        <HosterIcon host="https://nitroflare.com/view/2" />
      </>,
    ),
  );
  await settle();
  expect(img()).toBeNull();
  expect(host.textContent).toBe('nn');

  await act(() => vi.advanceTimersByTimeAsync(RETRY_MS.reduce((a, b) => a + b) + 1000));
  expect(fetch).toHaveBeenCalledTimes(1);
});

it('shows a monogram while the instance fetches the icon and asks again later', async () => {
  vi.useFakeTimers();
  const fetch = answers(503, 200);
  act(() => root.render(<HosterIcon host="katfile.com" />));
  await settle();
  expect(img()).toBeNull();
  expect(host.textContent).toBe('k');

  await act(() => vi.advanceTimersByTimeAsync(RETRY_MS[0]));
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(img()?.getAttribute('src')).toBe('blob:icon');
});

it('stops asking once the retries are spent', async () => {
  vi.useFakeTimers();
  const fetch = answers(503);
  act(() => root.render(<HosterIcon host="ddownload.com" />));
  await settle();
  for (const pause of RETRY_MS) await act(() => vi.advanceTimersByTimeAsync(pause));
  await act(() => vi.advanceTimersByTimeAsync(60_000));
  expect(fetch).toHaveBeenCalledTimes(RETRY_MS.length + 1);
  expect(img()).toBeNull();
  expect(host.textContent).toBe('d');
});

it('asks only once the row is scrolled into view', async () => {
  vi.useFakeTimers();
  inView = false;
  const fetch = answers(200);
  act(() => root.render(<HosterIcon host="turbobit.net" />));
  await settle();
  expect(fetch).not.toHaveBeenCalled();
  expect(host.textContent).toBe('');

  act(() => observers.forEach((fire) => fire()));
  await settle();
  expect(img()?.getAttribute('src')).toBe('blob:icon');
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
