import { ApiError } from './api';
import type { TranslationKey } from './i18n';

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

// The upload refusals the server sends with a code.
const CONTAINER_REFUSALS: Partial<Record<string, TranslationKey>> = {
  noJD: 'container.noJD',
  jdOff: 'container.jdOff',
  noUsenet: 'container.noUsenet',
};

/**
 * containerRefusal is why an upload was not taken: in this interface's words
 * where the server sent a code, in the server's sentence otherwise.
 */
export function containerRefusal(t: (key: TranslationKey) => string, e: unknown): string {
  const key = e instanceof ApiError && e.code ? CONTAINER_REFUSALS[e.code] : undefined;
  return key ? t(key) : message(e);
}
