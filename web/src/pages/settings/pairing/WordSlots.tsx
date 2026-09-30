// WordSlots lays out the twelve words in numbered tiles, the same wherever
// they appear: shown to be read out, or filling as somebody types them.
// Beside the QR code the three rows stretch to the code's height, so both
// end flush.
import type { ReactNode } from 'react';
import { QRCode } from '../../../components/QRCode';
import type { QRMatrix } from '../../../lib/api';
import { PHRASE_WORDS } from '../../../lib/phraseWords';

export function WordSlots({
  words,
  unknown = [],
  label,
  qr,
  qrLabel,
}: {
  words: string[];
  /** Positions, from 0, of words not on the list. */
  unknown?: number[];
  /** The list's accessible name; without one the slots are only decoration
   *  beside a field that already says it all. */
  label?: string;
  qr?: QRMatrix | null;
  /** What scanning the code leads to. */
  qrLabel?: string;
}) {
  const bad = new Set(unknown);
  const slots: ReactNode[] = [];
  for (let i = 0; i < PHRASE_WORDS; i++) {
    const w = words[i];
    const tone =
      w === undefined
        ? 'ring-1 ring-inset ring-carbon-border text-carbon-textMuted'
        : bad.has(i)
          ? 'bg-statusFailBg text-statusFail'
          : 'bg-carbon-surface2 text-carbon-text';
    slots.push(
      <li
        key={i}
        data-slot={i + 1}
        data-unknown={bad.has(i) ? '' : undefined}
        className={`flex items-center gap-2 rounded-[var(--radius-control)] px-3 py-1.5 text-sm ${tone}`}
      >
        <span className={`glim-num w-4.5 shrink-0 text-end text-xs ${bad.has(i) ? '' : 'text-carbon-textMuted'}`}>{i + 1}</span>
        <span dir="ltr" className="whitespace-nowrap font-mono">
          {w ?? '·'}
        </span>
      </li>,
    );
  }
  const grid = (
    <ol
      className="grid flex-1 grid-cols-2 gap-2 sm:grid-cols-4 sm:grid-rows-3"
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      {slots}
    </ol>
  );
  if (!qr) return grid;
  return (
    <div className="flex flex-col gap-4 sm:flex-row sm:items-stretch">
      {grid}
      <div className="flex shrink-0 justify-center sm:items-start">
        <QRCode matrix={qr} label={qrLabel ?? words.join(' ')} size={128} />
      </div>
    </div>
  );
}
