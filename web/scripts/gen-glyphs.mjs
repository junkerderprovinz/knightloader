// Regenerates src/lib/glyphs.tsx from GlimStone's reference/glyphs.json, the
// list that fixes one drawing per meaning for every GlimStone app.
//
// GlimStone is not a package, so the list is read from a checkout of it:
//
//   node scripts/gen-glyphs.mjs ../../glimstone/reference/glyphs.json
//
// Using another glyph from the list means adding its name to USED and running
// this again. KnightLoader's own drawings, which have no entry there, stay in
// src/lib/icons.tsx.
import { readFile, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, '..', 'src', 'lib', 'glyphs.tsx');
const source = process.argv[2] ?? join(here, '..', '..', '..', 'glimstone', 'reference', 'glyphs.json');

// In the order the list has them.
const USED = [
  'IconEye', 'IconEyeOff', 'IconRefresh', 'IconUpload', 'IconDownload', 'IconSearch',
  'IconPlay', 'IconPause', 'IconStop', 'IconPower', 'IconTrash', 'IconTrashFiles',
  'IconAdd', 'IconClose', 'IconPencil', 'IconCopy', 'IconCheck', 'IconLink', 'IconKey',
  'IconSignOut', 'IconInfo', 'IconHelp', 'IconMore', 'IconMenu', 'IconBack', 'IconForward',
  'IconLatest', 'IconFirst', 'IconMoveUp', 'IconMoveDown', 'IconExpand', 'IconCollapse',
  'IconFilter', 'IconPin', 'IconPriority', 'IconGrip', 'IconExternalLink', 'IconWarning',
  'IconBolt', 'IconShieldOn', 'IconCode', 'IconMoon', 'IconSun', 'IconGear',
  'IconDashboard', 'IconFolder', 'IconFolderOpen', 'IconFolderAdd', 'IconArchive',
  'IconFleet', 'IconCollector', 'IconAccounts', 'IconNetwork', 'IconPhone', 'IconBrowser',
  'IconApp', 'IconModules', 'IconCaptcha', 'IconContainers', 'IconGithub',
  'IconTabGeneral', 'IconTabLook', 'IconTabSecurity', 'IconTabAdvanced', 'IconTabApp',
  'IconSchedules', 'IconNotifications', 'IconSliders', 'IconDiagnostics', 'IconKeyboard',
  'IconPaste', 'IconSort', 'IconFolderUp', 'IconHealth', 'IconQueued', 'IconCaptchaTimer',
];

// The centre line IconCheckDrawn traces through IconCheck, in IconCheck's
// coordinates. Each end runs past the round cap and the stroke is wider than
// the bar, so the finished trace uncovers the whole check and nothing else
// shows through it.
const CHECK_TRACE = 'M0.54 6.54 5.3 11.3 13.39 2.07';
const CHECK_TRACE_WIDTH = '3.2';

const LICENCES = [
  [/^Streamline/, 'Streamline, free Core Solid (https://streamlinehq.com), CC BY 4.0'],
  [/^Font Awesome/, 'Font Awesome Free (https://fontawesome.com), CC BY 4.0'],
  [/^Tabler/, 'Tabler Icons (https://tabler.io/icons), MIT'],
  [/^Material Design Icons/, 'Material Design Icons (https://pictogrammers.com/library/mdi/), Apache 2.0'],
  [/^Simple Icons/, 'Simple Icons (https://simpleicons.org), CC0; the marks are trademarks, used only to name their owner'],
  [/^reCAPTCHA/, "reCAPTCHA's mark, a trademark used only where a captcha is the subject"],
];

const round = (n) => String(Math.round(n * 1000) / 1000);

// Each entry's viewBox is cropped square to its ink. Here a glyph renders at
// 22px with its ink across three quarters of the box, so the crop is widened
// by a third around its centre.
function widen(viewBox) {
  const [x, y, side] = viewBox.split(/\s+/).map(Number);
  const wide = (side * 4) / 3;
  const pad = (wide - side) / 2;
  return [x - pad, y - pad, wide, wide].map(round).join(' ');
}

const SHAPES = new Set(['path', 'rect', 'circle']);
const CAMEL = { 'fill-rule': 'fillRule', 'clip-rule': 'clipRule' };

// The list spells markup the SVG way, wrapped in plain <g>s and with paint the
// <svg> already sets; this turns it into one JSX element per line.
function toJsx(markup, indent) {
  const lines = [];
  const kept = [];
  for (const [, close, tag, attrs, selfClosing] of markup.matchAll(/<(\/?)(\w+)([^>]*?)\s*(\/?)>/g)) {
    const empty = selfClosing || SHAPES.has(tag);
    if (close && SHAPES.has(tag)) continue;
    if (close) {
      if (kept.pop()) lines.push(`${' '.repeat(indent + 2 * kept.filter(Boolean).length)}</${tag}>`);
      continue;
    }
    const props = [...attrs.matchAll(/([\w:-]+)="([^"]*)"/g)]
      .filter(([, name, value]) => !(name === 'fill' && value === 'currentColor') && name !== 'stroke-width')
      .map(([, name, value]) => `${CAMEL[name] ?? name}="${value}"`);
    if (tag === 'g' && props.length === 0) {
      kept.push(false);
      continue;
    }
    const pad = ' '.repeat(indent + 2 * kept.filter(Boolean).length);
    lines.push(`${pad}<${[tag, ...props].join(' ')}${empty ? ' />' : '>'}`);
    if (!empty) kept.push(true);
  }
  return lines.join('\n');
}

const list = JSON.parse(await readFile(source, 'utf8'));
const byName = new Map(list.map((g) => [g.name, g]));
const glyphs = USED.map((name) => {
  const g = byName.get(name);
  if (!g) throw new Error(`${name} is not in ${source}`);
  return g;
});

const licences = LICENCES.filter(([pattern]) => glyphs.some((g) => pattern.test(g.source))).map(([, text]) => text);

const parts = [
  `// Generated by scripts/gen-glyphs.mjs from GlimStone's reference/glyphs.json.
// Edit the generator or the list, not this file.
//
// Each glyph is GlimStone's drawing for one meaning, a filled shape whose gaps
// are carved with evenodd. The drawings come from:
${licences.map((l) => `//   ${l}`).join('\n')}
// and the rest were drawn for GlimStone or KnightLoader.
import { useId, type SVGProps } from 'react';

export const base = (p: SVGProps<SVGSVGElement>) => ({
  width: 22,
  height: 22,
  fill: 'currentColor',
  className: 'shrink-0',
  'aria-hidden': true,
  ...p,
});
`,
];

for (const g of glyphs) {
  const box = widen(g.viewBox);
  parts.push(`/** ${g.means}: ${g.source}. */
export const ${g.name} = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ viewBox: '${box}', ...p })}>
${toJsx(g.svg, 4)}
  </svg>
);
`);
  if (g.name === 'IconCheck') {
    parts.push(`/**
 * IconCheckDrawn is IconCheck drawing itself in, for a copy button's "Copied".
 * glim-check-draw animates a stroke, so the stroke runs along the check inside
 * a mask and uncovers the filled glyph as it goes. pathLength="1" lets the dash
 * offset run from 1 to 0 whatever the path's real length.
 */
export function IconCheckDrawn(p: SVGProps<SVGSVGElement>) {
  const mask = useId();
  return (
    <svg {...base({ viewBox: '${box}', ...p })}>
      <mask id={mask}>
        <path
          className="glim-check-draw"
          pathLength="1"
          d="${CHECK_TRACE}"
          stroke="white"
          strokeWidth="${CHECK_TRACE_WIDTH}"
          strokeLinejoin="round"
        />
      </mask>
      <g mask={\`url(#\${mask})\`}>
${toJsx(g.svg, 8)}
      </g>
    </svg>
  );
}
`);
  }
}

await writeFile(out, parts.join('\n'));
console.log(`wrote ${glyphs.length} glyphs to ${out}`);
