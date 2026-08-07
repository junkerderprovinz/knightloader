// Reading and writing a settings document by dotted key path, for the advanced
// table. Nothing here knows what any particular setting means — that is the
// point: waves 3 to 11 each add fields, and a table that enumerated them would
// be out of date before the wave after it landed.

/** What kind of editor a value gets, decided from the value rather than a schema. */
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
 * rowsFor is the table's row set: every key the settings STRUCT has, valued
 * from the document the server sent.
 *
 * Driven by the schema and not by the document, because `omitempty` means the
 * document is missing exactly the keys that are empty — which is the set
 * somebody comes to this page to fill in. A document key with no schema entry is
 * kept as well, so a field added on the server before this build knew about it
 * still appears.
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
      // An unrecognised kind from a newer server renders as a text box rather
      // than as a missing row: the value is still visible and still editable.
      return 'text';
  }
}

/**
 * flatten walks the document into one row per editable value.
 *
 * It recurses into plain objects and stops at arrays. An array of rule objects
 * or proxy entries has a page of its own with a proper editor, and exploding it
 * into `packagizer.rules.3.conditions.1.op` would produce a hundred rows nobody
 * can safely edit one at a time — the ordering alone is load-bearing. So a list
 * is one row, edited as JSON, and the row says where the real editor is.
 *
 * `skip` drops branches that are not settings at all: the rule-compile problems
 * ride along in the same response and are output, not configuration, and
 * offering a reset for them would be a control with nothing behind it.
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
 * setPath returns a copy of the document with one path replaced.
 *
 * A copy along the whole path, not a mutation: the draft is React state, and
 * writing into it in place means a re-render that shows the old value and a save
 * that sends the new one. Missing intermediate objects are created, so a key
 * that a fresh install has not written yet can still be set.
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
 * same compares two values the way the table needs to: structurally, and
 * treating an absent value as equal to an empty one.
 *
 * The second half matters because Go's `omitempty` drops an empty list on the
 * way out, so a stored `[]` and a never-set field arrive identically — and
 * without this every such row would render as "changed" on a fresh install, and
 * "only what differs from the default" would list the entire document.
 */
export function same(a: unknown, b: unknown): boolean {
  if (isEmptyish(a) && isEmptyish(b)) return true;
  return JSON.stringify(a) === JSON.stringify(b);
}

function isEmptyish(v: unknown): boolean {
  return v === undefined || v === null || (Array.isArray(v) && v.length === 0);
}
