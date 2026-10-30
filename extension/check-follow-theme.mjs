// While the extension follows the default instance, its pages show the
// instance's light or dark, not a copy taken when following was switched on.
//
// The look is stored, so no page waits on the relay before painting. A copy
// that is never taken again keeps whatever the instance held then: no theme at
// all from an instance older than the stored theme, which leaves the local one
// in force, or one chosen in the web interface before the latest. So every
// page that paints the look takes it again once it is open.
//
// The check runs appearance.js against a stand-in root, storage and relay, with
// the browser's own media query reporting light. A stored copy says `system`
// or `light` while the instance now says `dark`; once the page has taken the
// look again the root has to say dark. An instance on `system` leaves the
// choice to the browser, and with following off nothing asks the relay.
//
// Run by CI and by hand: `node extension/check-follow-theme.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');
const problems = [];
const fail = (why) => problems.push(why);

/** A page with appearance.js loaded, a group whose default instance answers
 *  with `instance`, and the browser preferring light. */
function page(stored, instance) {
  const attrs = {};
  const storage = structuredClone(stored);
  const asked = [];
  const ctx = vm.createContext({
    document: {
      documentElement: {
        style: { setProperty() {}, removeProperty() {} },
        setAttribute: (k, v) => (attrs[k] = v),
        removeAttribute: (k) => delete attrs[k],
        getAttribute: (k) => attrs[k] ?? null,
      },
    },
    window: { matchMedia: (q) => ({ matches: /light/.test(q) }) },
    chrome: {
      storage: {
        local: {
          get: async (keys) => {
            const list = typeof keys === 'string' ? [keys] : keys;
            return Object.fromEntries(list.filter((k) => k in storage).map((k) => [k, storage[k]]));
          },
          set: async (patch) => Object.assign(storage, structuredClone(patch)),
          remove: async (keys) => {
            for (const k of typeof keys === 'string' ? [keys] : keys) delete storage[k];
          },
        },
      },
    },
    readDefaultTarget: async () => 'inst-a',
    withGroup: async (work) =>
      work({
        siblings: [{ instanceId: 'inst-a' }],
        call: async (id, method, path) => {
          asked.push(`${method} ${path}`);
          return { status: 200, body: JSON.stringify(instance) };
        },
      }),
  });
  vm.runInContext(read('src', 'appearance.js'), ctx, { filename: 'appearance.js' });
  return { ctx, attrs, asked };
}

const LOOK = { accent: '', shape: 'soft', rainbow: false };

if (!/^async function refreshFollowedLook\(/m.test(read('src', 'appearance.js'))) {
  fail('src/appearance.js: no refreshFollowedLook() - a page keeps the look stored when following was switched on');
}

const cases = [
  {
    what: 'a copy taken before the instance kept a theme, local theme system',
    stored: { followInstance: true, theme: 'system' },
    instance: { ...LOOK, theme: 'dark' },
    want: 'dark',
  },
  {
    what: 'a copy taken while the instance was light',
    stored: { followInstance: true, theme: 'light' },
    instance: { ...LOOK, theme: 'dark' },
    want: 'dark',
  },
  {
    what: 'an instance on system',
    stored: { followInstance: true, theme: 'dark' },
    instance: { ...LOOK, theme: 'system' },
    want: null,
  },
];

for (const { what, stored, instance, want } of cases) {
  const { ctx, attrs } = page(stored, instance);
  await ctx.applyAppearance();
  if (typeof ctx.refreshFollowedLook === 'function') await ctx.refreshFollowedLook();
  const got = attrs['data-theme'] ?? null;
  if (got !== want) {
    fail(
      `${what}: the instance says ${instance.theme} and the browser prefers light, ` +
        `but the page shows data-theme=${got ?? '(none, the browser decides)'}, want ${want ?? '(none, the browser decides)'}`,
    );
  }
}

{
  const { ctx, attrs, asked } = page({ followInstance: false, theme: 'light' }, { ...LOOK, theme: 'dark' });
  if (typeof ctx.refreshFollowedLook === 'function') {
    await ctx.applyAppearance();
    await ctx.refreshFollowedLook();
    if (asked.length) fail(`following off: the page still asked the instance (${asked.join(', ')})`);
    if (attrs['data-theme'] !== 'light') fail(`following off: the local light theme gave way to ${attrs['data-theme']}`);
  }
}

// Every page that paints the look takes it again once it is open.
for (const file of ['popup.js', 'options.js']) {
  if (!/refreshFollowedLook\(/.test(read('src', file))) {
    fail(`src/${file}: never calls refreshFollowedLook() - it shows the look stored when following was switched on`);
  }
}

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log("ok: while following, the popup and the options page take the default instance's light or dark when they open");
