// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { Dropdown } from './Dropdown';

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
});

const FRUIT = [
  { value: 'apple', label: 'Apple' },
  { value: 'banana', label: 'Banana' },
  { value: 'blueberry', label: 'Blueberry' },
  { value: 'cherry', label: 'Cherry' },
];

async function show(value = 'banana'): Promise<HTMLButtonElement> {
  await act(async () => root.render(<Dropdown value={value} options={FRUIT} onChange={() => {}} label="Fruit" />));
  return host.querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]')!;
}

const key = (el: Element, name: string) =>
  act(async () => {
    el.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: name }));
  });

const focused = () => document.activeElement?.textContent;

describe('a closed dropdown', () => {
  it('opens on its first option with Home', async () => {
    const trigger = await show();
    await key(trigger, 'Home');
    expect(document.querySelector('[role="menu"]')).not.toBeNull();
    expect(focused()).toBe('Apple');
  });

  it('opens on its last option with End', async () => {
    const trigger = await show();
    await key(trigger, 'End');
    expect(focused()).toBe('Cherry');
  });

  it('opens on the value in force with an arrow', async () => {
    const trigger = await show();
    await key(trigger, 'ArrowDown');
    expect(focused()).toBe('Banana');
  });
});

describe('an open menu', () => {
  it('moves to the next option that starts with a typed letter, wrapping round', async () => {
    const trigger = await show('apple');
    await key(trigger, 'ArrowDown');
    await key(document.activeElement!, 'b');
    expect(focused()).toBe('Banana');
    await key(document.activeElement!, 'B');
    expect(focused()).toBe('Blueberry');
    await key(document.activeElement!, 'b');
    expect(focused()).toBe('Banana');
  });

  it('stays put for a letter no option starts with', async () => {
    const trigger = await show('cherry');
    await key(trigger, 'ArrowDown');
    await key(document.activeElement!, 'z');
    expect(focused()).toBe('Cherry');
  });
});
