import { describe, expect, it } from 'vitest';

import { addressOf, hostOf, newChannel, nextId, parseHeaders, formatHeaders, usableAddress } from './eventtargets';

describe('a new channel', () => {
  it('starts switched off, on failures, with the block its kind reads', () => {
    expect(newChannel('3', 'ntfy', 'ntfy')).toEqual({ id: '3', kind: 'ntfy', name: 'ntfy', enabled: false, on: 'failure', ntfy: { url: '' } });
    expect(newChannel('1', 'smtp', 'Mail').smtp).toEqual({ host: '', from: '', to: '', tls: 'starttls' });
  });

  it('takes the lowest free id', () => {
    expect(nextId([newChannel('1', 'slack', ''), newChannel('3', 'slack', '')])).toBe('2');
  });
});

describe('the address a channel sends to', () => {
  it('is the homeserver for Matrix and the mail server for e-mail', () => {
    expect(addressOf({ ...newChannel('1', 'matrix', ''), matrix: { homeserver: 'https://m.example', room: '!r', token: 't' } })).toBe('https://m.example');
    expect(addressOf({ ...newChannel('1', 'smtp', ''), smtp: { host: 'smtp.example', from: 'a', to: 'b' } })).toBe('smtp.example');
    expect(addressOf({ ...newChannel('1', 'gotify', ''), gotify: { url: 'https://g.example' } })).toBe('https://g.example');
  });

  it('shows only its host, since a token may sit in the query', () => {
    expect(hostOf('https://ntfy.example/topic?auth=secret')).toBe('ntfy.example');
  });

  it('may hold placeholders', () => {
    expect(usableAddress('https://%%instance%%.example/hook')).toBe(true);
    expect(usableAddress('https://x.example/%%open')).toBe(false);
    expect(usableAddress('ftp://x.example/')).toBe(false);
  });
});

describe('headers', () => {
  it('round-trip through the text box', () => {
    const h = { Authorization: 'Bearer a:b', 'X-Title': ' spaced' };
    expect(parseHeaders(formatHeaders(h))).toEqual(h);
    expect(parseHeaders('no colon here')).toBeNull();
  });
});
