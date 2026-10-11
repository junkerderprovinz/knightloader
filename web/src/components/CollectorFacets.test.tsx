// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import type { Task } from '../lib/api';
import { CollectorFacets, EMPTY_FACETS, matchesFacets, type FacetSelection } from './CollectorFacets';

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

const link = (id: string, hostName: string, name: string, pkg: string): Task =>
  ({ id, url: `https://${hostName}/${name}`, host: hostName, name, package: pkg, status: 'collected' }) as Task;

const TASKS = [
  link('a', 'one.example', 'film.mkv', 'Films'),
  link('b', 'one.example', 'film.part1.rar', 'Films'),
  link('c', 'two.example', 'song.mp3', ''),
];

/** The facets over three links, and the links they leave. */
function Panel() {
  const [picked, setPicked] = useState<FacetSelection>(EMPTY_FACETS);
  return (
    <>
      <CollectorFacets tasks={TASKS} selection={picked} onChange={setPicked} />
      <output>{TASKS.filter((x) => matchesFacets(x, picked)).map((x) => x.id).join(' ')}</output>
    </>
  );
}

const groups = () =>
  [...host.querySelectorAll('h3')].map((h) => [
    h.textContent,
    [...h.parentElement!.querySelectorAll('label')].map((l) => l.textContent),
  ]);
const facet = (name: string) => host.querySelector<HTMLButtonElement>(`[role="switch"][aria-label="${name}"]`)!;
const left = () => host.querySelector('output')!.textContent;
const clear = () => [...host.querySelectorAll('button')].find((b) => b.textContent === 'Clear')!;

it('counts the links by host, file type and package', async () => {
  await act(async () => root.render(<Panel />));
  expect(groups()).toEqual([
    ['Host', ['one.example2', 'two.example1']],
    ['File type', ['Archives1', 'Audio1', 'Video1']],
    ['Package', ['Films2', 'Ungrouped1']],
  ]);
});

it('narrows to what is switched on, either of two in one group and both across groups', async () => {
  await act(async () => root.render(<Panel />));
  expect(clear().disabled).toBe(true);

  await act(async () => facet('one.example').click());
  expect(left()).toBe('a b');
  await act(async () => facet('Video').click());
  expect(left()).toBe('a');
  await act(async () => facet('Archives').click());
  expect(left()).toBe('a b');

  await act(async () => clear().click());
  expect(left()).toBe('a b c');
  expect(facet('one.example').getAttribute('aria-checked')).toBe('false');
});
