// The pieces the cards of the Pairing tab are laid out with: a row with its
// control at the end and a fact line.
import type { ReactNode } from 'react';
import { InfoBubble } from '../../../components/ui';

/**
 * SetRow is one setting on a line: what it is at the start, with its
 * explanation behind an (i), and the control at the end. Rows under each other
 * are parted by a hairline.
 */
export function SetRow({
  label,
  hint,
  htmlFor,
  sub,
  children,
}: {
  label: string;
  hint?: string;
  /** The id of the field the label names. */
  htmlFor?: string;
  /** A second line under the label, such as a count. */
  sub?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-t border-carbon-border py-3 first:border-t-0 first:pt-0.5 last:pb-0.5">
      <div className="min-w-0 flex-[1_1_12.5rem]">
        {/* data-glim-label is where the settings search scrolls to. */}
        <span data-glim-label={label} className="inline-flex items-center gap-1.5 text-sm text-carbon-text">
          <label htmlFor={htmlFor}>{label}</label>
          {hint && <InfoBubble tip={hint} />}
        </span>
        {sub && <div className="mt-0.5 text-subline text-carbon-textMuted">{sub}</div>}
      </div>
      <div className="flex min-w-0 max-w-full flex-wrap items-center justify-end gap-2">{children}</div>
    </div>
  );
}

/** Fact is one sentence behind a glyph, with an optional bold lead-in. */
export function Fact({ glyph, label, children }: { glyph: ReactNode; label?: string; children?: ReactNode }) {
  return (
    <p className="flex items-start gap-2 text-sm text-carbon-textSub">
      <span className="mt-0.5 shrink-0 text-carbon-textMuted [&>svg]:h-4 [&>svg]:w-4">{glyph}</span>
      <span className="min-w-0">
        {label && <strong className="font-semibold text-carbon-text">{label} </strong>}
        {children}
      </span>
    </p>
  );
}
