// Picking a motion level shapes the next arrival of a page's cards and does not
// replay the one on screen.
//
// Every card on a page arrives through useArrival in Moving.tsx. The motion
// numbers change object with the level, so an arrival keyed on them starts
// again the moment somebody taps Wild in the settings: the whole settings page,
// the card under the finger included, drops to nothing and flies in again, and
// so does every screen mounted underneath. What the level may change is the
// next arrival, and Off has to land a card that is still waiting or in flight,
// or a screen mounted out of view stays blank until it is shown again.
//
// The check compiles Moving.tsx with the Babel that Expo already installs and
// renders a page of cards on a small stand-in for React, React Native and the
// navigator, so the hooks, effects and springs are the real code's. The
// numbers come from motion.ts, the same function the provider calls.
//
// Run by hand and by CI, from mobile/: `node check-arrival-level.mjs`
import { readFileSync } from 'node:fs';
import { createRequire, registerHooks } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

// The app imports its own modules without an extension, which Metro resolves
// and Node does not, so a relative import without one is tried as a .ts file.
// Only inside src/, since Babel's own files require their neighbours without an
// extension too.
const src = new URL('./src/', import.meta.url).href;
registerHooks({
  resolve(specifier, context, next) {
    const ours = context.parentURL?.startsWith(src);
    if (ours && /^\.\.?\//.test(specifier) && !/\.\w+$/.test(specifier)) return next(`${specifier}.ts`, context);
    return next(specifier, context);
  },
});

const here = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const problems = [];
const fail = (why) => problems.push(why);

const { motionNumbers } = await import(new URL('./src/theme/motion.ts', import.meta.url).href);
const motionNative = await import(new URL('./src/theme/motionNative.ts', import.meta.url).href);
const { arrivalDelay, springOf } = motionNative;

// A stand-in for React: one tree, hooks kept per component by its place in the
// tree, effects run after each render and again for as long as state changes.
let hook = null;
let pending = [];
let dirty = false;
const same = (a, b) => a.length === b.length && a.every((x, i) => Object.is(x, b[i]));
function cell(make) {
  const i = hook.at++;
  if (!(i in hook.inst.cells)) hook.inst.cells[i] = make();
  return hook.inst.cells[i];
}
const React = {
  createContext(defaultValue) {
    const context = { defaultValue };
    context.Provider = { provides: context };
    return context;
  },
  useContext: (context) => (hook.ctx.has(context) ? hook.ctx.get(context) : context.defaultValue),
  useRef: (initial) => cell(() => ({ current: initial })),
  useState(initial) {
    const s = cell(() => {
      const state = { value: typeof initial === 'function' ? initial() : initial };
      state.set = (next) => {
        const value = typeof next === 'function' ? next(state.value) : next;
        if (Object.is(value, state.value)) return;
        state.value = value;
        dirty = true;
      };
      return state;
    });
    return [s.value, s.set];
  },
  useMemo(make, deps) {
    const c = cell(() => ({}));
    if (!c.deps || !same(c.deps, deps)) {
      c.value = make();
      c.deps = deps;
    }
    return c.value;
  },
  useEffect(effect, deps) {
    const c = cell(() => ({}));
    if (c.deps && same(c.deps, deps)) return;
    c.deps = deps;
    pending.push(() => {
      c.cleanup?.();
      c.cleanup = effect();
    });
  },
};
const element = (type, props) => ({ type, props });

// Animated values and springs that only record: a test lands a flight by hand.
let started = [];
class AnimatedValue {
  constructor(value) {
    this.value = value;
    this.running = null;
  }
  setValue(value) {
    this.stopAnimation();
    this.value = value;
  }
  stopAnimation() {
    this.running?.stop();
  }
  interpolate(config) {
    return { of: this, config };
  }
}
const Animated = {
  Value: AnimatedValue,
  View: 'Animated.View',
  spring(value, config) {
    const run = {
      value,
      config,
      start() {
        value.running?.stop();
        value.running = run;
        started.push(run);
      },
      stop() {
        if (value.running === run) value.running = null;
      },
    };
    return run;
  },
  timing: () => ({ start() {} }),
  sequence: () => ({ start() {} }),
};
function land() {
  for (const run of started) {
    if (run.value.running !== run) continue;
    run.value.value = run.config.toValue;
    run.value.running = null;
  }
}

const NavigationContext = React.createContext(undefined);
let motion = motionNumbers('wild', false);
const modules = {
  react: React,
  'react/jsx-runtime': { jsx: element, jsxs: element },
  'react-native': {
    Animated,
    FlatList: 'FlatList',
    ScrollView: 'ScrollView',
    View: 'View',
    StyleSheet: { create: (styles) => styles },
  },
  '@react-navigation/native': { NavigationContext },
  '../theme/MotionContext': { useMotion: () => ({ n: motion }) },
  '../theme/motionNative': motionNative,
};

const babel = require('@babel/core');
const { code } = babel.transformSync(readFileSync(join(here, 'src', 'components', 'Moving.tsx'), 'utf8'), {
  filename: 'Moving.tsx',
  babelrc: false,
  configFile: false,
  presets: [[require.resolve('@babel/preset-typescript'), { isTSX: true, allExtensions: true }]],
  plugins: [
    [require.resolve('@babel/plugin-transform-react-jsx'), { runtime: 'automatic' }],
    require.resolve('@babel/plugin-transform-modules-commonjs'),
  ],
});
const moving = { exports: {} };
new Function('require', 'module', 'exports', code)(
  (name) => {
    if (!(name in modules)) throw new Error(`Moving.tsx imports ${name}, which this check has no stand-in for`);
    return modules[name];
  },
  moving,
  moving.exports,
);
const { MovingScroll, Arrive } = moving.exports;

/** A navigator for one screen, focused or not, that can send the screen away and back. */
function screen(focused) {
  const listeners = { focus: new Set(), blur: new Set() };
  return {
    isFocused: () => focused,
    addListener(event, f) {
      listeners[event].add(f);
      return () => listeners[event].delete(f);
    },
    emit(event) {
      focused = event === 'focus';
      for (const f of [...listeners[event]]) f();
    },
  };
}

/** A page of three cards on `nav`. `render()` draws it again and returns the cards' moving styles. */
function page(nav) {
  started = [];
  const instances = new Map();
  const root = element(NavigationContext.Provider, {
    value: nav,
    children: element(MovingScroll, { children: [0, 1, 2].map((i) => element(Arrive, { children: `card ${i}` })) }),
  });
  let cards = [];
  function draw(node, path, ctx) {
    if (node === null || typeof node !== 'object') return;
    if (Array.isArray(node)) {
      node.forEach((child, i) => draw(child, `${path}.${i}`, ctx));
      return;
    }
    const { type, props } = node;
    if (typeof type === 'function') {
      const key = `${path}:${type.name}`;
      if (!instances.has(key)) instances.set(key, { cells: [] });
      const outer = hook;
      hook = { inst: instances.get(key), at: 0, ctx };
      const out = type(props);
      hook = outer;
      draw(out, key, ctx);
      return;
    }
    if (type.provides) {
      draw(props.children, path, new Map(ctx).set(type.provides, props.value));
      return;
    }
    const arrival = Array.isArray(props.style) ? props.style[1] : null;
    if (type === 'Animated.View' && arrival?.opacity) cards.push(arrival);
    draw(props.children, `${path}/${type}`, ctx);
  }
  return function render() {
    do {
      dirty = false;
      cards = [];
      draw(root, 'root', new Map());
      const run = pending;
      pending = [];
      for (const f of run) f();
    } while (dirty);
    return cards;
  };
}
const shown = (cards) => cards.map((c) => c.opacity.of.value);
const flying = (cards) => cards.some((c) => c.opacity.of.running);

// A level change on a page that has arrived.
{
  motion = motionNumbers('wild', false);
  const nav = screen(true);
  const render = page(nav);
  let cards = render();
  if (started.length !== cards.length) {
    fail(`a focused page of ${cards.length} cards started ${started.length} arrivals at Wild, so this check cannot see a replay`);
  }
  land();
  const before = started.length;
  motion = motionNumbers('subtle', false);
  cards = render();
  if (started.length !== before || shown(cards).some((v) => v !== 1)) {
    fail('picking another motion level sends every card on the page back to invisible and flies it in again, the one just tapped included');
  }
  if (cards.some((c) => c.transform[0].translateY.config.outputRange[0] !== motion.travel)) {
    fail('after a level change the cards still carry the old level\'s travel into their next arrival');
  }

  // The next return plays with the level picked.
  const back = started.length;
  nav.emit('blur');
  nav.emit('focus');
  cards = render();
  const replay = started.slice(back);
  if (replay.length !== cards.length) {
    fail(`coming back to the page after a level change started ${replay.length} arrivals for ${cards.length} cards`);
  } else if (
    replay.some((run, place) => run.config.delay !== arrivalDelay(place, motion) || run.config.damping !== springOf(motion.bounce).damping)
  ) {
    fail('coming back to the page after a level change plays the arrival with the old level\'s stagger or bounce');
  }
}

// Off, or the phone's reduced motion, arriving while cards wait or fly.
for (const [to, n] of [
  ['Off', motionNumbers('off', false)],
  ['reduced motion', motionNumbers('wild', true)],
]) {
  motion = motionNumbers('wild', false);
  const waiting = page(screen(false));
  if (shown(waiting()).some((v) => v !== 0)) fail('a card under another screen is visible at Wild, so this check cannot see it wait');
  motion = n;
  if (shown(waiting()).some((v) => v !== 1)) {
    fail(`switching to ${to} leaves the cards of a screen underneath invisible until it comes back into view`);
  }

  motion = motionNumbers('wild', false);
  const arriving = page(screen(true));
  if (!flying(arriving())) fail('no card of a focused page is in flight at Wild, so this check cannot see one land');
  motion = n;
  const cards = arriving();
  if (flying(cards) || shown(cards).some((v) => v !== 1)) {
    fail(`switching to ${to} lets the cards in flight fly on instead of landing them`);
  }
}

if (problems.length) {
  console.error(`check-arrival-level: ${problems.length} problem(s).`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(
  'ok: a new motion level leaves the cards on screen where they are, the next return arrives with its numbers, ' +
    'and Off or reduced motion lands every card that was waiting or in flight',
);
