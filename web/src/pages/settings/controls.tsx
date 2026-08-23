import type { CSSProperties } from 'react';
import { hueVars, rainbowAt } from '../../lib/appearance';

/**
 * The one control this tree adds to the design language, and why.
 *
 * `Toggle` in components/ui.tsx fills with the accent when it is on. That was
 * right for a switch you look at one of, and wrong for a column of them: the
 * accent means activity, and a settings table where nearly every row is "on"
 * would read as a page where six things are happening. Wave 1 had to un-gold
 * exactly such a column on the task list, and both surfaces in this directory —
 * the module list and the advanced key table — are that same shape.
 *
 * jdp overrode that reasoning on purpose (2026-08-23, after it was put to him
 * directly as a real trade-off): every switch takes a rainbow hue anyway, so
 * a column of them tells its members apart in rainbow mode the same way every
 * other set in the app does, "loud column" risk accepted knowingly rather
 * than left as an unreviewed exception.
 *
 * Everything except the fill is deliberately identical to Toggle, so the two
 * read as one control: role="switch", the geometry, the left-0 anchor the
 * knob needs inside a button, and now the same hue mechanism.
 */
export function NeutralSwitch({
  on,
  onChange,
  name,
  onLabel,
  offLabel,
  disabled = false,
  hue,
}: {
  on: boolean;
  onChange: (next: boolean) => void;
  /** The accessible name; the visible label is the on/off word beside it. */
  name: string;
  /** The on/off word beside the pill. Both empty renders the pill alone, for a
   *  switch whose meaning is already written next to it. */
  onLabel?: string;
  offLabel?: string;
  disabled?: boolean;
  /** This switch's position in a list of switches sharing one card - same
   *  0-based sequence Toggle/SectionTitle/Tabs already carry. Omit for a
   *  lone switch with no siblings needing to be told apart. */
  hue?: number;
}) {
  const worded = Boolean(onLabel || offLabel);
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={name}
      disabled={disabled}
      onClick={() => onChange(!on)}
      className={`${hue !== undefined ? 'glim-hue' : ''} flex shrink-0 items-center gap-2.5 text-left text-xs text-carbon-textSub select-none disabled:opacity-40`}
      style={hue !== undefined ? (hueVars(rainbowAt(hue)) as CSSProperties) : undefined}
    >
      {worded && <span className="glim-num w-6 text-end">{on ? onLabel : offLabel}</span>}
      <span
        className={`relative h-5 w-9 shrink-0 rounded-[var(--radius-pill)] transition-colors ${
          on ? (hue !== undefined ? 'bg-accent' : 'bg-carbon-textMuted') : 'bg-carbon-surface3'
        }`}
      >
        {/* left-0 is load-bearing: without it the knob starts from its static
            position, which a button's inherited text-align centres, and the knob
            then slides out past the pill. Tailwind v4 also animates the
            `translate` property here, not `transform`. */}
        {/* bg-carbon-background, not a fixed white — same "opposite ground"
            fix as Toggle's own knob in ui.tsx (jdp: "Die Toggle Punkte
            sollen im Darkmode schwarz sein"); this switch was missed by
            that pass since it is a separate component, just built to look
            identical. */}
        <span
          className={`absolute left-0 top-0.5 h-4 w-4 rounded-[var(--radius-pill)] bg-carbon-background shadow-sm transition-[translate] duration-150 ${
            on ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </span>
    </button>
  );
}
