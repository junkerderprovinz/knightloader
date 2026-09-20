// Reading and writing a settings document by dotted key path, for the advanced
// table. Nothing here knows what a setting means, so new fields need no change.

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
