import { describe, expect, it } from 'vitest';
import { unusedProxyRanges } from './trustedProxies';

describe('unusedProxyRanges', () => {
  it('names the ranges wider than a proxy needs and nothing else', () => {
    expect(
      unusedProxyRanges([
        '192.168.0.0/16',
        ' 192.168.1.0/24 ',
        '10.10.0.0/28',
        '10.10.0.0/27',
        '172.18.0.5',
        '2001:db8::/124',
        '2001:db8::/48',
        '2001:db8::/32',
        '2001:db8::1',
        '',
      ]),
    ).toEqual(['192.168.0.0/16', '192.168.1.0/24', '10.10.0.0/27', '2001:db8::/48', '2001:db8::/32']);
  });

  it('takes a missing list', () => {
    expect(unusedProxyRanges(undefined)).toEqual([]);
  });
});
