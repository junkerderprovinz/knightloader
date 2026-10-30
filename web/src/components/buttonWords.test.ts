import { expect, it } from 'vitest';

// A Button with a glyph follows the Beschriftung setting by hiding its words in
// glyph mode and naming itself with them. Words it can read are plain text
// children; children that hold an element need `label`, or the button keeps
// its words in glyph mode and has no name there.

const sources = import.meta.glob(['../**/*.tsx', '!../**/*.test.tsx'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

/** openingTag returns the attributes of the tag that starts at `at` and where it ends, braces and strings skipped. */
function openingTag(text: string, at: number): { attrs: string; end: number; selfClosing: boolean } {
  let depth = 0;
  for (let i = at; i < text.length; i++) {
    const c = text[i];
    if (c === '{') depth++;
    else if (c === '}') depth--;
    else if (depth === 0 && (c === '"' || c === "'")) i = text.indexOf(c, i + 1);
    else if (depth === 0 && c === '>') {
      const selfClosing = text[i - 1] === '/';
      return { attrs: text.slice(at, selfClosing ? i - 1 : i), end: i + 1, selfClosing };
    }
  }
  throw new Error(`unclosed tag at ${at}`);
}

it('reads the sources it checks', () => {
  expect(Object.keys(sources).length).toBeGreaterThan(100);
});

it('gives every Button with a glyph words the label setting can hide', () => {
  const unnamed: string[] = [];
  for (const [file, text] of Object.entries(sources)) {
    for (const m of text.matchAll(/<Button\b/g)) {
      const tag = openingTag(text, m.index + m[0].length);
      if (tag.selfClosing || !/\bicon=/.test(tag.attrs) || /\blabel=/.test(tag.attrs)) continue;
      const children = text.slice(tag.end, text.indexOf('</Button>', tag.end));
      if (/<[A-Za-z>]/.test(children.replace(/\{\/\*[\s\S]*?\*\/\}/g, ''))) {
        unnamed.push(`${file.slice(3)}:${text.slice(0, m.index).split('\n').length}`);
      }
    }
  }
  expect(unnamed).toEqual([]);
});
