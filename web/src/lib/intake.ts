/**
 * isEditableTarget reports whether an event's target handles its own text: an
 * input, a textarea, a select, or anything contentEditable. The global paste
 * and drop listeners check it, or a password pasted into a field would become
 * a download.
 */
export function isEditableTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  return !!el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName ?? ''));
}

/** message turns a caught value into the text a toast can show. */
export function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
