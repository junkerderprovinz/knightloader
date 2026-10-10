// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../../../lib/i18n';
import { PairingSteps } from './PairingSteps';

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
  vi.unstubAllGlobals();
  delete document.documentElement.dataset.motion;
});

function draw() {
  act(() =>
    root.render(
      <I18nProvider>
        <PairingSteps hues={[0, 1, 2]} />
      </I18nProvider>,
    ),
  );
}

const pictures = () => [...host.querySelectorAll('svg[data-step]')];
const moving = () => host.querySelectorAll('animate, animateMotion, animateTransform').length;

describe('the step pictures', () => {
  it('play one scene over three cards, with the real controls and one pointer', () => {
    draw();
    expect(pictures().map((svg) => svg.getAttribute('data-step'))).toEqual(['1', '2', '3']);
    expect(pictures()[0].textContent).toContain('Generate phrase');
    expect(pictures()[0].textContent).toContain('Copied');
    expect(pictures()[1].textContent).toContain('Enter phrase');
    expect(pictures()[1].textContent).toContain('Paste');
    expect(pictures()[2].textContent).toContain('Paired');
    expect(moving()).toBeGreaterThan(0);
    // The pointer stands above the grid, not inside one of the pictures.
    expect(host.querySelectorAll('svg[viewBox="0 0 14 19"]')).toHaveLength(1);
  });

  it('show the end frame at the motion level off', () => {
    document.documentElement.dataset.motion = 'off';
    draw();
    expect(moving()).toBe(0);
    expect(host.querySelector('svg[viewBox="0 0 14 19"]')).toBeNull();
    // The copy button reads Copy again, the words are in and the pair is paired.
    expect(pictures()[0].textContent).not.toContain('Copied');
    expect(pictures()[1].textContent).toContain('orbit');
    expect(pictures()[2].textContent).toContain('Paired');
    expect(pictures()[2].querySelectorAll('[opacity="0"]')).toHaveLength(0);
  });

  it('show the end frame when the system asks for less motion', () => {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: query.includes('reduce'),
      addEventListener() {},
      removeEventListener() {},
    }));
    draw();
    expect(moving()).toBe(0);
  });

  it('stop when the motion level is switched off while they play', async () => {
    draw();
    expect(moving()).toBeGreaterThan(0);
    await act(async () => {
      document.documentElement.dataset.motion = 'off';
      // The level is watched by a MutationObserver, which reports after the task.
      await new Promise((done) => setTimeout(done, 0));
    });
    expect(moving()).toBe(0);
  });
});
