// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it } from 'vitest';

import { LoadingCard } from './ui';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

interface NodeFs {
  readFileSync(path: string, encoding: 'utf8'): string;
}

describe('the loading card', () => {
  it('waits before it shows, so a quick answer never flashes it', () => {
    const host = document.createElement('div');
    const root = createRoot(host);
    act(() => root.render(<LoadingCard label="Loading" />));
    expect(host.firstElementChild!.classList.contains('glim-busy-wait')).toBe(true);
    act(() => root.unmount());

    // Vitest hands a test every stylesheet empty, so the rule is read off the disk.
    const fs = (globalThis as unknown as { process: { getBuiltinModule(id: 'node:fs'): NodeFs } }).process.getBuiltinModule(
      'node:fs',
    );
    const css = fs.readFileSync('src/index.css', 'utf8');
    expect(css).toMatch(/@keyframes glim-busy-show\s*\{\s*from\s*\{\s*visibility:\s*hidden;?\s*\}\s*\}/);
    expect(css).toMatch(/\.glim-busy-wait\s*\{[^}]*animation:\s*glim-busy-show 0s var\(--busy-delay, 400ms\) both/);
  });
});
