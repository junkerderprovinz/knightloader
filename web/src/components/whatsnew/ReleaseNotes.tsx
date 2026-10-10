import type { ReactNode } from 'react';
import { followExternal } from '../../lib/external';

/** A link in a release body is followed only to a website or a mail address. */
function safeHref(url: string): string | null {
  const u = url.trim();
  return /^(https?:|mailto:)/i.test(u) ? u : null;
}

const INLINE = /\[([^\]]+)\]\(([^)]+)\)|\*\*([^*]+)\*\*|`([^`]+)`/g;

/** inline turns links, bold and code into elements and leaves the rest as text. */
function inline(text: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(INLINE)) {
    if (m.index > last) out.push(text.slice(last, m.index));
    const key = m.index;
    if (m[1] !== undefined) {
      const href = safeHref(m[2]);
      out.push(
        href ? (
          // A plain link rather than a badge, since it stands inside a sentence.
          <a
            key={key}
            href={href}
            target="_blank"
            rel="noreferrer noopener"
            onClick={followExternal}
            className="text-accentInk underline hover:no-underline"
          >
            {m[1]}
          </a>
        ) : (
          m[1]
        ),
      );
    } else if (m[3] !== undefined) {
      out.push(
        <strong key={key} className="font-semibold text-carbon-text">
          {inline(m[3])}
        </strong>,
      );
    } else {
      out.push(
        <code key={key} dir="ltr" className="glim-num text-carbon-text">
          {m[4]}
        </code>,
      );
    }
    last = m.index + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

/**
 * ReleaseNotes draws the part of Markdown the release notes are written in:
 * headings, lists and paragraphs, with links, bold and code inside a line.
 * They are English in every language, like the release page they come from.
 */
export function ReleaseNotes({ text }: { text: string }) {
  const blocks: ReactNode[] = [];
  let items: string[] = [];
  const endList = () => {
    if (items.length === 0) return;
    blocks.push(
      <ul key={blocks.length} className="flex flex-col gap-1.5 ps-4">
        {items.map((item, i) => (
          <li key={i} className="list-disc marker:text-carbon-textMuted">
            {inline(item)}
          </li>
        ))}
      </ul>,
    );
    items = [];
  };

  for (const raw of text.replace(/\r\n/g, '\n').split('\n')) {
    const line = raw.trim();
    const bullet = /^[-*]\s+(.*)$/.exec(line);
    if (bullet) {
      items.push(bullet[1]);
      continue;
    }
    endList();
    if (line === '' || /^([-*_])\1{2,}$/.test(line)) continue;
    const heading = /^#{1,6}\s+(.*)$/.exec(line);
    blocks.push(
      heading ? (
        <h3 key={blocks.length} className="pt-2 text-sm font-semibold text-carbon-text first:pt-0">
          {inline(heading[1])}
        </h3>
      ) : (
        <p key={blocks.length}>{inline(line)}</p>
      ),
    );
  }
  endList();

  return (
    <div lang="en" dir="ltr" className="flex flex-col gap-2 text-sm text-carbon-textSub">
      {blocks}
    </div>
  );
}
