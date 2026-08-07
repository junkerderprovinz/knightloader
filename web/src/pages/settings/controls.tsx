/**
 * The one control this tree adds to the design language, and why.
 *
 * `Toggle` in components/ui.tsx fills with the accent when it is on. That is
 * right for a switch you look at one of, and wrong for a column of them: the
 * accent means activity, and a settings table where nearly every row is "on"
 * would read as a page where six things are happening. Wave 1 had to un-gold
 * exactly such a column on the task list, and both surfaces in this directory —
 * the module list and the advanced key table — are that same shape.
 *
 * Everything except the fill is deliberately identical to Toggle, so the two
 * read as one control: role="switch", the geometry, and the left-0 anchor the
 * knob needs inside a button.
 */
export function NeutralSwitch({
  on,
  onChange,
  name,
  onLabel,
  offLabel,
  disabled = false,
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
      className="flex shrink-0 items-center gap-2.5 text-left text-xs text-carbon-textSub select-none disabled:opacity-40"
    >
      {worded && <span className="glim-num w-6 text-end">{on ? onLabel : offLabel}</span>}
      <span
        className={`relative h-5 w-9 shrink-0 rounded-[var(--radius-pill)] transition-colors ${
          on ? 'bg-carbon-textMuted' : 'bg-carbon-surface3'
        }`}
      >
        {/* left-0 is load-bearing: without it the knob starts from its static
            position, which a button's inherited text-align centres, and the knob
            then slides out past the pill. Tailwind v4 also animates the
            `translate` property here, not `transform`. */}
        <span
          className={`absolute left-0 top-0.5 h-4 w-4 rounded-[var(--radius-pill)] bg-white shadow-sm transition-[translate] duration-150 ${
            on ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </span>
    </button>
  );
}
