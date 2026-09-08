import { InfoBubble } from '../../../components/ui';

/**
 * One read-only figure with a caption, for a page that has nothing to save.
 *
 * WHY NOT `Field`. Field is a `<label>`, and a `<label>` hands its clicks and
 * its accessible name to the first labelable thing inside it. With no control
 * in there at all it is a label naming nothing, which reads to a screen reader
 * as a form field that cannot be reached - the same trap Field's own doc
 * comment describes for a caption over a SET of controls, one step further on.
 *
 * `data-glim-label` is copied from ui.tsx's own Caption on purpose and is the
 * whole of the settings search's jump mechanism: the search resolves a row's
 * key to its TRANSLATED caption and looks for that property in the DOM (see
 * pages/settings/jump.ts). Without it these rows would be findable and then
 * land on the page rather than on the row, and the search would be telling the
 * reader something untrue about their own page.
 *
 * `dir="ltr"` on the value for the same reason every other number, path and
 * version cell in settings carries it: a version string, a date and an uptime
 * all read wrong mirrored, whatever direction the interface is running in.
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
