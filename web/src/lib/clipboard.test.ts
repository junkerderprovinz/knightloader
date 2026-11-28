import { expect, it } from 'vitest';

// The browser hands out navigator.clipboard only in a secure context, and the
// container is mostly opened over plain http on the local network. A button
// that writes to it directly does nothing there.

const sources = import.meta.glob(['../**/*.{ts,tsx}', '!../**/*.test.{ts,tsx}', '!./clipboard.ts'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

it('goes through copyToClipboard everywhere, so copying works over plain http too', () => {
  const direct = Object.keys(sources).filter((path) => /navigator\.clipboard\??\.writeText/.test(sources[path]!));
  expect(direct).toEqual([]);
});
