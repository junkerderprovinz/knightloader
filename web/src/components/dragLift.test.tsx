// @vitest-environment jsdom
import { act, useRef } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it } from 'vitest';

import { useDragArmed } from './dragLift';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function List({ held, tone }: { held: boolean; tone: string }) {
  const list = useRef<HTMLUListElement>(null);
  useDragArmed(list, held);
  return (
    <ul ref={list} className={tone}>
      <li>a</li>
      <li>b</li>
    </ul>
  );
}

it('keeps the list trembling while an item is held, also when the list rewrites its class', () => {
  const host = document.createElement('div');
  const root = createRoot(host);
  const ul = () => host.querySelector('ul')!;

  act(() => root.render(<List held={false} tone="one" />));
  expect(ul().classList.contains('glim-drag-armed')).toBe(false);

  act(() => root.render(<List held tone="one" />));
  expect(ul().classList.contains('glim-drag-armed')).toBe(true);

  act(() => root.render(<List held tone="two" />));
  expect(ul().className).toBe('two glim-drag-armed');

  act(() => root.render(<List held={false} tone="two" />));
  expect(ul().classList.contains('glim-drag-armed')).toBe(false);
  act(() => root.unmount());
});
