// Reading, writing and comparing settings documents: by dotted key path for the
// advanced table, and field by field for the autosave. Nothing here knows what
// a setting means, so new fields need no change.

/** The kind of editor a value gets. */
export type ValueKind = 'boolean' | 'number' | 'text' | 'list' | 'object';

export interface Row {
  /** Dotted path, e.g. "reconnect.intervalSeconds". */
  path: string;
  value: unknown;
  kind: ValueKind;
}

export function kindOf(v: unknown): ValueKind {
  if (typeof v === 'boolean') return 'boolean';
  if (typeof v === 'number') return 'number';
  if (Array.isArray(v)) return 'list';
  if (v !== null && typeof v === 'object') return 'object';
  return 'text';
}

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return v !== null && typeof v === 'object' && !Array.isArray(v);
}

/**
 * rowsFor returns every key of the settings struct, valued from the document.
 * It follows the schema because omitempty drops the empty keys from the
 * document, and it keeps document keys the schema lacks.
 */
export function rowsFor(
  doc: Record<string, unknown>,
  kinds: Record<string, string>,
  skip: readonly string[] = [],
): Row[] {
  const seen = new Set<string>();
  const out: Row[] = [];
  for (const path of Object.keys(kinds).sort()) {
    if (skip.includes(path)) continue;
    seen.add(path);
    out.push({ path, value: getPath(doc, path), kind: asKind(kinds[path]) });
  }
  for (const r of flatten(doc, skip)) {
    if (!seen.has(r.path)) out.push(r);
  }
  return out.sort((a, b) => a.path.localeCompare(b.path));
}

function asKind(s: string): ValueKind {
  switch (s) {
    case 'boolean':
    case 'number':
    case 'list':
    case 'object':
      return s;
    default:
      // An unknown kind from a newer server still gets an editable text box.
      return 'text';
  }
}

/**
 * flatten walks the document into one row per editable value. It recurses into
 * plain objects and stops at arrays, which stay one JSON row since their order
 * matters. `skip` drops branches that are output rather than settings.
 */
export function flatten(doc: Record<string, unknown>, skip: readonly string[] = []): Row[] {
  const out: Row[] = [];
  const walk = (node: Record<string, unknown>, prefix: string) => {
    for (const key of Object.keys(node).sort()) {
      const path = prefix ? `${prefix}.${key}` : key;
      if (skip.includes(path)) continue;
      const value = node[key];
      if (isPlainObject(value)) {
        walk(value, path);
        continue;
      }
      out.push({ path, value, kind: kindOf(value) });
    }
  };
  walk(doc, '');
  return out;
}

export function getPath(doc: Record<string, unknown>, path: string): unknown {
  let cur: unknown = doc;
  for (const seg of path.split('.')) {
    if (!isPlainObject(cur)) return undefined;
    cur = cur[seg];
  }
  return cur;
}

/**
 * setPath returns a copy of the document with one path replaced, creating
 * missing objects on the way. It copies because the draft is React state.
 */
export function setPath(
  doc: Record<string, unknown>,
  path: string,
  value: unknown,
): Record<string, unknown> {
  const [head, ...rest] = path.split('.');
  const next: Record<string, unknown> = { ...doc };
  if (rest.length === 0) {
    next[head] = value;
    return next;
  }
  const child = doc[head];
  next[head] = setPath(isPlainObject(child) ? child : {}, rest.join('.'), value);
  return next;
}

/**
 * same compares two values structurally and treats an absent value as equal to
 * an empty one, because omitempty drops empty lists on the way out.
 */
export function same(a: unknown, b: unknown): boolean {
  if (isEmptyish(a) && isEmptyish(b)) return true;
  return JSON.stringify(a) === JSON.stringify(b);
}

function isEmptyish(v: unknown): boolean {
  return v === undefined || v === null || (Array.isArray(v) && v.length === 0);
}

type Doc = Record<string, unknown>;

/**
 * pendingFields is what the autosave still has to send: every top-level field
 * the draft changed, except one still holding a value the server has answered
 * already, a refusal or a tidied value kept while its box had focus.
 */
export function pendingFields(draft: Doc, saved: Doc, answered: Doc): Doc {
  const out: Doc = {};
  for (const k of Object.keys(draft)) {
    if (same(draft[k], saved[k])) continue;
    if (k in answered && same(draft[k], answered[k])) continue;
    out[k] = draft[k];
  }
  return out;
}

/** A value the server refused, with the place it named and its reason. */
export interface Refusal {
  value: unknown;
  /** The top-level key, or a dotted path below it such as "reconnect.checkUrl". */
  field: string;
  error: unknown;
}

/** What the server has said about the draft that it did not take. */
export interface Answer {
  /** Refused values by top-level key. Each stays out of the saves until its field changes. */
  refused: Record<string, Refusal>;
  /**
   * The fields a save left unsent when it failed without naming one of them.
   * They are not sent again until the draft changes, or every autosave would
   * repeat the failure.
   */
  failed: Doc | null;
}

export const noAnswer: Answer = { refused: {}, failed: null };

/** topKey is the settings key a field path starts with. */
export function topKey(field: string): string {
  return field.split('.')[0];
}

/**
 * shownAt reports whether a refusal of field belongs at the control for at:
 * the same place or, with below, anywhere under it, as for a control that
 * edits a whole list.
 */
export function shownAt(field: string, at: string, below: boolean): boolean {
  return field === at || (below && field.startsWith(`${at}.`));
}

/**
 * toSend is what the next autosave sends. held is what the server tidied while
 * its box had focus, which also stays out.
 */
export function toSend(draft: Doc, saved: Doc, held: Doc, answer: Answer): Doc {
  const answered: Doc = { ...held };
  for (const [k, r] of Object.entries(answer.refused)) answered[k] = r.value;
  const pending = pendingFields(draft, saved, answered);
  return answer.failed && sameFields(pending, answer.failed) ? {} : pending;
}

function sameFields(a: Doc, b: Doc): boolean {
  const keys = Object.keys(a);
  return keys.length === Object.keys(b).length && keys.every((k) => k in b && same(a[k], b[k]));
}

/** The outcome of one save. */
export interface Round {
  /** The server's answer to the fields in sent, when they went through. */
  applied: Doc | null;
  sent: Doc;
  /** The fields refused on the way, each taken out before the rest went again. */
  refused: Record<string, Refusal>;
  /** A failure that named no field it could take out, with the fields it left unsent. */
  failed: { fields: Doc; error: unknown } | null;
}

/**
 * saveRound sends fields. A refusal that names one of them takes that field
 * out and sends the rest again, so one unusable folder does not hold back every
 * other edit. Any other failure ends the round.
 */
export async function saveRound(fields: Doc, send: (patch: Doc) => Promise<Doc>): Promise<Round> {
  const sent: Doc = { ...fields };
  const refused: Record<string, Refusal> = {};
  while (Object.keys(sent).length > 0) {
    try {
      return { applied: await send(sent), sent, refused, failed: null };
    } catch (error) {
      const field = (error as { field?: unknown } | null)?.field;
      const key = typeof field === 'string' ? topKey(field) : '';
      if (!key || !(key in sent)) return { applied: null, sent: {}, refused, failed: { fields: sent, error } };
      refused[key] = { value: sent[key], field: field as string, error };
      delete sent[key];
    }
  }
  return { applied: null, sent, refused, failed: null };
}

/**
 * afterRound is the answer once a save is back. An earlier failure is over:
 * the save only went out because the draft had moved on from it.
 */
export function afterRound(answer: Answer, round: Round): Answer {
  const refused = { ...answer.refused, ...round.refused };
  for (const k of Object.keys(round.sent)) delete refused[k];
  return { refused, failed: round.failed ? round.failed.fields : null };
}

/**
 * settle drops what the draft has moved away from: a refusal once its field
 * holds something else, and a failed save once any of its fields changed.
 */
export function settle(answer: Answer, draft: Doc): Answer {
  const stale = Object.keys(answer.refused).filter((k) => !same(draft[k], answer.refused[k].value));
  const failedMoved = answer.failed !== null && Object.keys(answer.failed).some((k) => !same(draft[k], answer.failed?.[k]));
  if (stale.length === 0 && !failedMoved) return answer;
  const refused = { ...answer.refused };
  for (const k of stale) delete refused[k];
  return { refused, failed: failedMoved ? null : answer.failed };
}

/**
 * typedIn reports whether text, what the focused box shows, is a part of sent
 * that the server answered with something else: a string anywhere inside it,
 * or a list of strings edited one per line. Only the parts that differ count,
 * so an empty box does not match an unrelated empty value beside the one that
 * changed.
 */
export function typedIn(sent: unknown, answer: unknown, text: string): boolean {
  if (same(sent, answer)) return false;
  if (typeof sent === 'string') return sent === text;
  if (Array.isArray(sent)) {
    if (sent.length > 0 && sent.every((v) => typeof v === 'string') && sent.join('\n') === text) return true;
    const other = Array.isArray(answer) ? answer : [];
    return sent.some((v, i) => typedIn(v, other[i], text));
  }
  if (isPlainObject(sent)) {
    const other = isPlainObject(answer) ? answer : {};
    return Object.keys(sent).some((k) => typedIn(sent[k], other[k], text));
  }
  return false;
}

/**
 * heldAfter is what stays held once a save is back. A field it sent is held at
 * the sent value while its box is still typed in and let go otherwise, so an
 * old entry cannot keep a later edit back to that same text from going out.
 */
export function heldAfter(held: Doc, sent: Doc, keep: readonly string[]): Doc {
  const out: Doc = { ...held };
  for (const k of Object.keys(sent)) {
    if (keep.includes(k)) out[k] = sent[k];
    else delete out[k];
  }
  return out;
}

/**
 * foldAnswer is the draft once a save has come back: the server's answer,
 * except for a field the draft changed while the save was out and a field in
 * keep, whose box is still being typed in. sent is what the save sent, and
 * before is the stored document the draft was compared with to send it.
 */
export function foldAnswer(draft: Doc, answer: Doc, sent: Doc, before: Doc, keep: readonly string[]): Doc {
  const out: Doc = { ...answer };
  for (const k of Object.keys(draft)) {
    const expected = k in sent ? sent[k] : before[k];
    if (keep.includes(k) || !same(draft[k], expected)) out[k] = draft[k];
  }
  return out;
}
