// Renders one component on node, for the checks that need a screen to behave
// rather than a function to return: hooks kept by call order, effects run
// after each render and undone on unmount, and each render reduced to its
// words and its elements. The component is compiled with the Babel that Expo
// already installs, and its imports come from the stand-ins the check hands
// over.
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { basename } from 'node:path';

const require = createRequire(import.meta.url);
const babel = require('@babel/core');

let rendering = null;

const same = (a, b) => a && b && a.length === b.length && a.every((x, i) => Object.is(x, b[i]));
const cell = (make) => {
  const m = rendering;
  const i = m.at++;
  if (!(i in m.cells)) m.cells[i] = make(m);
  return m.cells[i];
};

export const React = {
  useState(initial) {
    const s = cell((m) => {
      const state = { value: typeof initial === 'function' ? initial() : initial };
      state.set = (next) => {
        const value = typeof next === 'function' ? next(state.value) : next;
        if (Object.is(value, state.value)) return;
        state.value = value;
        m.dirty = true;
      };
      return state;
    });
    return [s.value, s.set];
  },
  useRef: (initial) => cell(() => ({ current: initial })),
  useCallback(f, deps) {
    const c = cell(() => ({}));
    if (!same(c.deps, deps)) Object.assign(c, { f, deps });
    return c.f;
  },
  useEffect(effect, deps) {
    const c = cell(() => ({}));
    if (deps && same(c.deps, deps)) return;
    c.deps = deps;
    rendering.pending.push(() => {
      c.undo?.();
      c.undo = effect() ?? undefined;
    });
  },
};

const element = (type, props) => ({ type, props });

/** Compiles a .tsx file and returns its exports, its imports taken from `modules`. */
export function compile(path, modules) {
  const all = { react: React, 'react/jsx-runtime': { jsx: element, jsxs: element, Fragment: 'Fragment' }, ...modules };
  const { code } = babel.transformSync(readFileSync(path, 'utf8'), {
    filename: basename(path),
    babelrc: false,
    configFile: false,
    presets: [[require.resolve('@babel/preset-typescript'), { isTSX: true, allExtensions: true }]],
    plugins: [
      [require.resolve('@babel/plugin-transform-react-jsx'), { runtime: 'automatic' }],
      require.resolve('@babel/plugin-transform-modules-commonjs'),
    ],
  });
  const mod = { exports: {} };
  new Function('require', 'module', 'exports', code)(
    (name) => {
      if (!(name in all)) throw new Error(`${basename(path)} imports ${name}, which this check has no stand-in for`);
      return all[name];
    },
    mod,
    mod.exports,
  );
  return mod.exports;
}

function draw(node, out) {
  if (node === null || node === undefined || typeof node === 'boolean') return out;
  if (typeof node === 'string' || typeof node === 'number') {
    out.words.push(String(node));
    return out;
  }
  if (Array.isArray(node)) {
    for (const child of node) draw(child, out);
    return out;
  }
  out.elements.push(node);
  if (typeof node.props.label === 'string') out.words.push(node.props.label);
  // A row hands its switch over as a prop rather than as a child.
  for (const [name, value] of Object.entries(node.props)) {
    if (name === 'children' || isElement(value) || (Array.isArray(value) && value.some(isElement))) draw(value, out);
  }
  return out;
}

const isElement = (v) => typeof v === 'object' && v !== null && 'type' in v && 'props' in v;

/**
 * Renders `view` until its effects settle. `settle` lets pending promises run
 * and renders again; `shown` is the last render's words and elements.
 */
export function mount(view) {
  const m = { cells: [], at: 0, pending: [], dirty: false, shown: null };
  const render = () => {
    do {
      m.dirty = false;
      m.at = 0;
      rendering = m;
      try {
        m.shown = draw(view(), { words: [], elements: [] });
      } finally {
        rendering = null;
      }
      const run = m.pending;
      m.pending = [];
      for (const f of run) f();
    } while (m.dirty);
  };
  render();
  return {
    async settle() {
      for (let i = 0; i < 10; i++) {
        await new Promise((r) => setTimeout(r));
        render();
      }
      return m.shown;
    },
    shown: () => m.shown,
    unmount() {
      for (const c of m.cells) c?.undo?.();
    },
  };
}
