// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { appAddress, basePath, sendApiCallsUnderBase, settleUnderBase, socketURL, withBase } from './basePath';
import { buildBookmarklet, quickAddUrl } from './browserTools';
import { captchaWidgetUrl, hosterIconURL, taskFileURL, type CaptchaChallenge, type SelfTestRequestView } from './api';
import { proxyVerdicts } from './selftest';

/** servedAt puts the <base> element into the page the way the server does. */
function servedAt(href: string) {
  document.head.querySelector('base')?.remove();
  const el = document.createElement('base');
  el.setAttribute('href', href);
  document.head.prepend(el);
}

const plainFetch = window.fetch;

beforeEach(() => {
  history.replaceState(null, '', '/');
});

afterEach(() => {
  document.head.querySelector('base')?.remove();
  window.fetch = plainFetch;
});

describe('at the root', () => {
  it('builds every URL exactly as without a base path', () => {
    servedAt('/');
    expect(basePath()).toBe('');
    expect(withBase('/api/tasks')).toBe('/api/tasks');
    expect(taskFileURL('t1')).toBe('/api/tasks/t1/file');
    expect(socketURL('/api/ws')).toBe(`ws://${location.host}/api/ws`);
    expect(appAddress()).toBe(location.origin);
  });

  it('leaves fetch alone', () => {
    servedAt('/');
    const stub = vi.fn(async () => new Response('[]'));
    window.fetch = stub;
    sendApiCallsUnderBase();
    expect(window.fetch).toBe(stub);
  });

  it('reads a page without a <base> element, as the dev server serves it, as the root', () => {
    expect(basePath()).toBe('');
  });
});

describe('under /kl', () => {
  beforeEach(() => servedAt('/kl/'));

  it('puts the prefix in front of links, sockets and the instance address', () => {
    expect(basePath()).toBe('/kl');
    expect(taskFileURL('t 1')).toBe('/kl/api/tasks/t%201/file');
    expect(taskFileURL('t1', '/api/instances/nas')).toBe('/kl/api/instances/nas/tasks/t1/file');
    expect(hosterIconURL('example.com')).toBe('/kl/api/hosters/icon?host=example.com');
    expect(captchaWidgetUrl({ id: 'c1' } as CaptchaChallenge, '')).toBe('/kl/api/captcha/c1/widget?');
    expect(socketURL('/api/ws')).toBe(`ws://${location.host}/kl/api/ws`);
    expect(appAddress()).toBe(`${location.origin}/kl`);
  });

  it('sends API calls under the prefix once, and nothing else', async () => {
    const stub = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response('[]'));
    window.fetch = stub;
    sendApiCallsUnderBase();

    await fetch('/api/tasks', { method: 'POST' });
    await fetch(taskFileURL('t1'), { method: 'HEAD' });
    await fetch('https://example.com/api/tasks');

    expect(stub.mock.calls.map((c) => c[0])).toEqual([
      '/kl/api/tasks',
      '/kl/api/tasks/t1/file',
      'https://example.com/api/tasks',
    ]);
    expect(stub.mock.calls[0][1]).toEqual({ method: 'POST' });
  });

  it('moves a page opened on the bare address under the prefix', () => {
    history.replaceState(null, '', '/settings/network?x=1#top');
    settleUnderBase();
    expect(location.pathname + location.search + location.hash).toBe('/kl/settings/network?x=1#top');

    settleUnderBase();
    expect(location.pathname).toBe('/kl/settings/network');
  });

  it('bakes the prefix into the bookmarklet and the quick add page', () => {
    expect(decodeURIComponent(buildBookmarklet(appAddress()))).toContain(`'${location.origin}/kl/quickadd?url='`);
    expect(quickAddUrl('https://example.com/kl/', { url: 'https://files.example/a' })).toBe(
      'https://example.com/kl/quickadd?url=https%3A%2F%2Ffiles.example%2Fa',
    );
    expect(quickAddUrl('https://example.com', {})).toBe('https://example.com/quickadd');
  });
});

describe('the path prefix check', () => {
  const view = (over: Partial<SelfTestRequestView>): SelfTestRequestView => ({
    host: 'example.com',
    forwardedHost: '',
    forwardedProto: '',
    tls: false,
    path: '/api/selftest/request',
    forwardedPrefix: '',
    basePath: '',
    forwardedForHops: 0,
    now: new Date().toISOString(),
    zone: 'UTC',
    zoneOffsetSeconds: 0,
    zoneReadable: true,
    deployment: 'container',
    ...over,
  });
  const prefixRow = (v: SelfTestRequestView) =>
    proxyVerdicts(v, { host: 'example.com', protocol: 'http:' }).find((r) => r.id === 'prefix')!;

  it('passes at the root and under a prefix the proxy agrees with', () => {
    expect(prefixRow(view({}))).toMatchObject({ status: 'pass', code: 'proxy.prefix.root' });
    expect(prefixRow(view({ basePath: '/kl' }))).toMatchObject({
      status: 'pass',
      code: 'proxy.prefix.underPath',
      params: { path: '/kl' },
    });
    expect(prefixRow(view({ basePath: '/kl', forwardedPrefix: '/kl' })).status).toBe('pass');
  });

  it('fails when the proxy names a prefix this instance does not serve under', () => {
    expect(prefixRow(view({ basePath: '/kl', forwardedPrefix: '/dl' }))).toMatchObject({
      status: 'fail',
      code: 'proxy.prefix.mismatch',
      params: { path: '/dl', base: '/kl' },
    });
    expect(prefixRow(view({ forwardedPrefix: '/api' }))).toMatchObject({
      status: 'fail',
      params: { path: '/api', base: '/' },
    });
  });
});
