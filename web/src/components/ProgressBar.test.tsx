// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import { ProgressBar } from './ProgressBar';

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

const edge = () => host.querySelector('.kl-bar-edge');

it('marks the front edge of a bar that is under way', () => {
  act(() => root.render(<ProgressBar percent={40} active />));
  expect(edge()).not.toBeNull();
});

it('draws no front edge on a full bar, which has no front any more', () => {
  act(() => root.render(<ProgressBar percent={100} active tone="ok" />));
  expect(edge()).toBeNull();
});
