import { InfoBubble } from '../../../components/ui';

/**
 * Reading shows one read-only figure with a caption. It is not a Field because
 * a label with no control inside reads to a screen reader as an unreachable
 * form field, and data-glim-label lets the settings search jump to the row.
 */
export function Reading({
  label,
  hint,
  value,
}: {
  label: string;
  hint?: string;
  value: string;
}) {
  return (
    <div className="flex flex-col gap-1">
      <span data-glim-label={label} className="flex items-center text-[11px] text-carbon-textMuted">
        {label}
        {hint && <InfoBubble tip={hint} />}
      </span>
      <span className="glim-num text-sm text-carbon-text" dir="ltr">
        {value}
      </span>
    </div>
  );
}
