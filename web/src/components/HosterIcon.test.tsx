// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { HosterIcon, mayHaveSiteIcon, RETRY_MS } from './HosterIcon';
import { serviceMark } from '../lib/serviceMarks';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
});

const img = () => host.querySelector('img');

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

it('shows a monogram for a host without an icon and asks again later', () => {
  vi.useFakeTimers();
  act(() => root.render(<HosterIcon host="rapidgator.net" />));
  expect(img()?.getAttribute('src')).toBe('/api/hosters/icon?host=rapidgator.net');

  act(() => img()!.dispatchEvent(new Event('error')));
  expect(img()).toBeNull();
  expect(host.textContent).toBe('r');

  act(() => vi.advanceTimersByTime(RETRY_MS[0]));
  expect(img()?.getAttribute('src')).toBe('/api/hosters/icon?host=rapidgator.net&attempt=1');
  // The monogram stays until the retried image arrives.
  expect(host.textContent).toBe('r');
  act(() => img()!.dispatchEvent(new Event('load')));
  expect(host.textContent).toBe('');
});

it('stops asking once the retries are spent', () => {
  vi.useFakeTimers();
  act(() => root.render(<HosterIcon host="rapidgator.net" />));
  for (const pause of RETRY_MS) {
    act(() => img()!.dispatchEvent(new Event('error')));
    act(() => vi.advanceTimersByTime(pause));
  }
  act(() => img()!.dispatchEvent(new Event('error')));
  act(() => vi.advanceTimersByTime(60_000));
  expect(img()).toBeNull();
  expect(host.textContent).toBe('r');
});

it('asks only for dotted names that end in a top-level domain', () => {
  for (const name of ['rapidgator.net', 'dl.free.fr', 'xn--80ak6aa92e.com', '1fichier.com']) {
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
  ]) {
    expect(mayHaveSiteIcon(name), name).toBe(false);
  }
});

it('draws the monogram for hosts that cannot have an icon without asking the instance', () => {
  for (const raw of ['torrent-magnet', '3f9a0c41d2e8b7a6', '127.0.0.1:8080', 'http://[::1]/a', '2001:db8::1']) {
    act(() => root.render(<HosterIcon host={raw} />));
    expect(img(), raw).toBeNull();
  }
  expect(host.textContent).toBe('2');
});
