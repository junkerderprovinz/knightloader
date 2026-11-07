// The extension's colour engine keeps the web UI's promises: every ink reaches
// 4.5:1 and is the ink the web UI computes for the same colour (GlimStone's
// rule 45), the system's colour shows in a browser that reports it and gives
// way to the theme's gold in one that does not (rule 44), and OLED is this
// browser's own, so taking the look from an instance leaves it alone (rule 43).
//
// The check runs appearance.js against a stand-in root, storage and relay, and
// the web UI's contrast.ts with its types stripped.
//
// Run by CI and by hand: `node extension/check-accent.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');
const web = await import(pathToFileURL(join(here, '..', 'web', 'src', 'lib', 'contrast.ts')).href);
const problems = [];
const check = (what, ok) => {
  if (!ok) problems.push(what);
};

const FIREFOX = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:150.0) Gecko/20100101 Firefox/150.0';
const CHROME = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36';

/** A page with appearance.js loaded in a browser with this user agent, whose system accent is `system`. */
function page(ua, { stored = {}, system = 'rgb(224, 56, 62)', instance } = {}) {
  const attrs = {};
  const props = {};
  const storage = structuredClone(stored);
  const ctx = vm.createContext({
    navigator: { userAgent: ua },
    CSS: { supports: (_, value) => value === 'AccentColor' },
    getComputedStyle: () => ({ color: system }),
    document: {
      createElement: () => ({ style: {}, remove() {} }),
      documentElement: {
        append() {},
        style: { setProperty: (k, v) => (props[k] = v), removeProperty: (k) => delete props[k] },
        setAttribute: (k, v) => (attrs[k] = v),
        removeAttribute: (k) => delete attrs[k],
        getAttribute: (k) => attrs[k] ?? null,
      },
    },
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
        call: async () => ({ status: 200, body: JSON.stringify(instance ?? {}) }),
      }),
  });
  vm.runInContext(read('src', 'appearance.js'), ctx, { filename: 'appearance.js' });
  return { ctx, attrs, props, storage };
}

{
  const { ctx } = page(CHROME);
  const steps = [0x00, 0x33, 0x66, 0x99, 0xcc, 0xff];
  const sweep = ['#FCC419', '#1D99F3', '#FF8389', '#BE95FF', '#FF832B', '#FF7EB6', '#7B7B7B', '#FFFF00', '#0000FF'];
  for (const r of steps) for (const g of steps) for (const b of steps) sweep.push('#' + [r, g, b].map((c) => c.toString(16).padStart(2, '0')).join('').toUpperCase());
  for (const hex of sweep) {
    const ink = ctx.contrastOn(hex);
    check(`the ink on ${hex} reads at 4.5:1`, web.contrastRatio(ink, hex) >= 4.5);
    check(`the ink on ${hex} is the web UI's`, ink === web.contrastOn(hex));
    check(`${hex} as text gets the web UI's inks`, JSON.stringify(ctx.accentInks(hex)) === JSON.stringify(web.accentInks(hex)));
  }
}

{
  const { ctx, props } = page(FIREFOX);
  ctx.applyAccent('system');
  check(`Firefox paints the system's colour, got ${props['--accent']}`, props['--accent'] === '#E0383E');
  ctx.applyAccent('#FFFF00');
  check('a pale accent gets its own light ink', /^#[0-9A-F]{6}$/.test(props['--accent-ink-light'] ?? ''));
  ctx.applyAccent('#1D99F3');
  check('a preset leaves the inks to the stylesheet', !('--accent-ink-light' in props) && !('--accent-ink-dark' in props));
}

{
  const { ctx, props } = page(CHROME);
  check('Chrome counts as no source', ctx.browserAccent() === undefined);
  ctx.applyAccent('#1D99F3');
  ctx.applyAccent('system');
  check(`Chrome gives the system's colour back to the theme's gold, got ${props['--accent']}`, !('--accent' in props));
}

{
  const { ctx } = page(FIREFOX, { stored: { accent: 'system', accentSlotChosen: 2 } });
  const a = await ctx.readAppearance();
  check(`the word system survives a read, got ${a.accent}`, a.accent === 'system');
  check('the word is never filed as a mixed colour', Object.values(a.accentCustoms).every((v) => v !== 'system'));
}

{
  const { ctx, attrs, storage } = page(CHROME, {
    stored: { oled: true, followInstance: true, theme: 'dark' },
    instance: { theme: 'light', accent: '#1D99F3', shape: 'soft' },
  });
  await ctx.applyAppearance();
  check('a stored OLED puts data-oled on the page', attrs['data-oled'] === '');
  await ctx.adoptFromInstance();
  check("taking the instance's look leaves OLED as it was", storage.oled === true);
}

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems.slice(0, 20)) console.error(`  ${p}`);
  process.exit(1);
}
console.log("ok: the extension's inks are the web UI's and reach 4.5:1, the system colour shows where the browser reports it, and OLED stays this browser's");
