// Every text on the phone is set in the house font.
//
// React Native has no cascade, so a Text that does not name Noto Sans draws in
// the system font, and nothing looks broken on the device the change was tried
// on. src/components/Text.tsx is the one place that names it: its Text and
// TextInput swap each fontWeight for the matching cut. A screen that imports
// either from 'react-native' instead, or reaches for Animated.Text, is back on
// Roboto without anyone noticing. So is a style that names the system family.
//
// Run by hand and by CI, from mobile/: `node check-house-font.mjs`
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const home = join(here, 'src', 'components', 'Text.tsx');

const walk = (dir) => {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else if (/\.tsx?$/.test(name)) out.push(p);
  }
  return out;
};

/** Comments blanked with their offsets kept, so a line number is the real one
 *  and a sentence about Animated.Text is not read as a use of it. */
const strip = (text) =>
  text
    .replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '))
    .replace(/^([^\n'"`]*?)\/\/.*$/gm, (_, head) => head);

const files = [join(here, 'App.tsx'), ...walk(join(here, 'src'))].filter((f) => f !== home);
const problems = [];
for (const file of files) {
  const code = strip(readFileSync(file, 'utf8'));
  const where = (at) => `${relative(here, file).replace(/\\/g, '/')}:${code.slice(0, at).split('\n').length}`;
  for (const m of code.matchAll(/import\s*\{([^}]*)\}\s*from\s*'react-native'/g)) {
    const names = m[1].split(',').map((n) => n.trim().replace(/^type\s+/, '').split(/\s+as\s+/)[0]);
    for (const n of names.filter((x) => x === 'Text' || x === 'TextInput')) {
      problems.push(`${where(m.index)} imports ${n} from 'react-native'; take it from src/components/Text.tsx`);
    }
  }
  for (const m of code.matchAll(/\bAnimated\.(Text|createAnimatedComponent\(\s*Text)\b/g)) {
    problems.push(`${where(m.index)} animates a Text that bypasses the house font`);
  }
  for (const m of code.matchAll(/fontFamily\s*:\s*['"](System|sans-serif|Roboto)['"]/g)) {
    problems.push(`${where(m.index)} names the system family ${m[1]}`);
  }
}

if (problems.length) {
  console.error(`check-house-font: ${problems.length} text(s) outside the house font.`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(`ok: ${files.length} files, every Text and TextInput comes from src/components/Text.tsx`);
