// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { TestButton } from './TestButton';
import type { ButtonVerdict } from './ui';

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

const words = { ok: 'Connected', fail: 'Not connected' };
const button = () => host.querySelector('button')!;
const press = () => act(async () => button().click());

function render(run: () => Promise<ButtonVerdict | null>, resetKey = 'a') {
  act(() => root.render(<TestButton label="Test" busyLabel="Testing" words={words} run={run} resetKey={resetKey} />));
}

describe('a button that tests something', () => {
  it('says the answer on itself in a word and in the success colour', async () => {
    render(async () => 'ok');
    expect(button().textContent).toBe('Test');
    await press();
    expect(button().textContent).toBe('Connected');
    expect(button().className).toContain('bg-statusOkSolid');
    expect(host.querySelector('[role="status"]')!.textContent).toBe('Connected');
  });

  it('turns red and shakes when the test fails', async () => {
    render(async () => 'fail');
    await press();
    expect(button().textContent).toBe('Not connected');
    expect(button().className).toContain('bg-statusFailSolid');
    expect([...button().classList].some((c) => c.endsWith('-shake'))).toBe(true);
  });

  it('counts a test that threw as failed', async () => {
    render(async () => {
      throw new Error('unreachable');
    });
    await press();
    expect(button().textContent).toBe('Not connected');
  });

  it('shows its busy word and takes no second press while the test runs', async () => {
    let finish: (v: ButtonVerdict) => void = () => {};
    render(() => new Promise<ButtonVerdict>((resolve) => (finish = resolve)));
    await press();
    expect(button().textContent).toBe('Testing');
    expect(button().disabled).toBe(true);
    await act(async () => finish('ok'));
    expect(button().textContent).toBe('Connected');
  });

  it('goes back to its own word once what it tested changes', async () => {
    const run = async (): Promise<ButtonVerdict> => 'ok';
    render(run);
    await press();
    expect(button().textContent).toBe('Connected');
    render(run, 'b');
    expect(button().textContent).toBe('Test');
    expect(button().className).not.toContain('bg-statusOkSolid');
  });

  it('keeps its own word when the test has no answer to show', async () => {
    render(async () => null);
    await press();
    expect(button().textContent).toBe('Test');
  });
});
