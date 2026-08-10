/**
 * isEditableTarget reports whether an event's target already handles its own
 * text: an input, a textarea, a select, or anything contentEditable.
 *
 * A global paste or drop listener that skipped this check would turn
 * pasting a password into a search field into a queued download that also
 * prints the password into the task list (build-plan.md section 8's Wave 8
 * note, 8B) - the same reason ListToolbar's own document-level Delete-key
 * handler already carries this exact check, which this mirrors rather than
 * reinvents.
 */
export function isEditableTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  return !!el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName ?? ''));
}

/** message turns a caught value into the text a toast can show. */
export function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
