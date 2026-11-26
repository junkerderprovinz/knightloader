import { request } from './client';
import { ApiError } from './refusal';
import type { ServerConnection, Task } from './types';
import type { TranslationKey } from '../i18n/en';

// What the properties window and the add screen's options send, on the routes
// the web interface's properties panel, rename windows and add form use
// (web/src/lib/api.ts), so the two clients cannot drift on what a field means.

/** The fields of a task this file edits, which the list screens never read. */
export interface EditableTask extends Task {
  comment?: string;
  chunks?: number;
  category?: string;
  infoHash?: string;
}

/** A category as GET /api/categories names it. */
export interface Category {
  id: string;
  name: string;
}

/** A step of the instance's priority ladder (GET /api/queue/priorities). */
export interface PriorityChoice {
  id: string;
  value: number;
}

/** The folders a download of this phone may go to. An instance too old for
 *  the route offers none, and the folder box is then typed into alone. */
export async function fetchDownloadFolders(conn: ServerConnection, base = '/api'): Promise<string[]> {
  const r = await request<{ folders?: string[] } | undefined>(conn, base, '/download-folders').catch(() => undefined);
  return r?.folders ?? [];
}

/** The categories a link can be filed under, none from an instance too old to say. */
export async function fetchCategories(conn: ServerConnection, base = '/api'): Promise<Category[]> {
  const r = await request<{ categories?: Category[] } | undefined>(conn, base, '/categories').catch(() => undefined);
  return r?.categories ?? [];
}

/** The priority ladder, highest first, as the web's add form offers it. */
export async function fetchPriorities(conn: ServerConnection, base = '/api'): Promise<PriorityChoice[]> {
  const r = await request<PriorityChoice[] | null>(conn, base, '/queue/priorities').catch(() => null);
  return (r ?? []).slice().reverse();
}

export interface TaskOptionsPatch {
  name?: string;
  dir?: string;
  password?: string;
  downloadPassword?: string;
  comment?: string;
  chunks?: number;
}

const post = (conn: ServerConnection, base: string, path: string, body: unknown) =>
  request(conn, base, path, { method: 'POST', body: JSON.stringify(body) });

export const setTaskOptions = (conn: ServerConnection, ids: string[], opts: TaskOptionsPatch, base = '/api') =>
  post(conn, base, '/tasks/options', { ids, ...opts });

/** renamePackage renames the package these tasks are in, and the folder named after it. */
export const renamePackage = (conn: ServerConnection, ids: string[], name: string, base = '/api') =>
  post(conn, base, '/tasks/package/rename', { ids, name });

/** movePackage puts tasks into the package named, or takes them out of theirs for ''. */
export const movePackage = (conn: ServerConnection, ids: string[], pkg: string, base = '/api') =>
  post(conn, base, '/tasks/package', { ids, package: pkg });

/** setCategory files tasks under a category by its id, or under none for ''. */
export const setCategory = (conn: ServerConnection, ids: string[], category: string, base = '/api') =>
  post(conn, base, '/tasks/category', { ids, category });

/** The add screen's options, as typed. */
export interface AddOptions {
  package: string;
  dir: string;
  category: string;
  /** A value of the ladder, or '' for whatever the rules and the category say. */
  priority: string;
  autoExtract: 'inherit' | 'on' | 'off';
  comment: string;
  password: string;
  downloadPassword: string;
  overrule: boolean;
}

export const NO_ADD_OPTIONS: AddOptions = {
  package: '',
  dir: '',
  category: '',
  priority: '',
  autoExtract: 'inherit',
  comment: '',
  password: '',
  downloadPassword: '',
  overrule: false,
};

/** addOptionsSet says whether any option differs from a plain paste. */
export function addOptionsSet(o: AddOptions): boolean {
  return (Object.keys(NO_ADD_OPTIONS) as (keyof AddOptions)[]).some((k) =>
    typeof o[k] === 'string' ? (o[k] as string).trim() !== NO_ADD_OPTIONS[k] : o[k] !== NO_ADD_OPTIONS[k],
  );
}

/**
 * addLinksBody is what POST /api/links is sent for links and the options. A
 * box left empty is left out, so the instance's rules and defaults decide it.
 */
export function addLinksBody(links: string[], o: AddOptions): Record<string, unknown> {
  const text = (s: string) => s.trim() || undefined;
  return {
    links: links.join('\n'),
    package: text(o.package),
    dir: text(o.dir),
    category: o.category || undefined,
    priority: o.priority === '' ? undefined : Number(o.priority),
    autoExtract: o.autoExtract === 'inherit' ? undefined : o.autoExtract === 'on',
    comment: text(o.comment),
    password: text(o.password),
    downloadPassword: text(o.downloadPassword),
    overrule: o.overrule || undefined,
  };
}

export async function addLinksWithOptions(
  conn: ServerConnection,
  links: string[],
  o: AddOptions,
  base = '/api',
): Promise<Task[]> {
  return request<Task[]>(conn, base, '/links', { method: 'POST', body: JSON.stringify(addLinksBody(links, o)) });
}

/** What the properties window edits: one link, or every link of a package. */
export type EditTarget = { kind: 'link'; task: EditableTask } | { kind: 'package'; name: string; tasks: EditableTask[] };

/** The category of a package whose links are filed under more than one. */
export const MIXED = ' mixed';

/** The window's boxes. `mixed` names those a package's links disagree on,
 *  which open empty and are sent only once something is typed. */
export interface EditDraft {
  name: string;
  pkg: string;
  category: string;
  dir: string;
  password: string;
  downloadPassword: string;
  comment: string;
  chunks: string;
  mixed: ('dir' | 'comment' | 'chunks')[];
}

export const tasksOf = (target: EditTarget): EditableTask[] => (target.kind === 'link' ? [target.task] : target.tasks);

/** startOf is what the boxes hold when the window opens. Passwords open
 *  empty, since a member of the group is never shown one. */
export function startOf(target: EditTarget): EditDraft {
  const tasks = tasksOf(target);
  const shared = (pick: (x: EditableTask) => string) => {
    const first = pick(tasks[0]);
    return tasks.every((x) => pick(x) === first) ? first : null;
  };
  const dir = shared((x) => x.dir ?? '');
  const comment = shared((x) => x.comment ?? '');
  const chunks = shared((x) => String(x.chunks ?? 0));
  const mixed: EditDraft['mixed'] = [];
  if (dir === null) mixed.push('dir');
  if (comment === null) mixed.push('comment');
  if (chunks === null) mixed.push('chunks');
  return {
    name: target.kind === 'link' ? target.task.name : target.name,
    pkg: target.kind === 'link' ? target.task.package || '' : '',
    category: shared((x) => x.category ?? '') ?? MIXED,
    dir: dir ?? '',
    password: '',
    downloadPassword: '',
    comment: comment ?? '',
    chunks: chunks ?? '',
    mixed,
  };
}

/** What a save sends, in the order it is sent. */
export interface EditPlan {
  /** A package's new name, which takes its folder along. */
  renamePackage?: string;
  /** The package the links go into, '' for none. */
  move?: string;
  options: TaskOptionsPatch;
  category?: string;
}

/**
 * planEdit compares the boxes with how they opened and says what to send, or
 * which sentence stops the save before anything is sent. Only what changed
 * goes out, so a box a package's links disagree on keeps their own values
 * until somebody types into it.
 */
export function planEdit(target: EditTarget, start: EditDraft, draft: EditDraft): EditPlan | { fault: TranslationKey } {
  const plan: EditPlan = { options: {} };
  const name = draft.name.trim();
  if (name !== start.name.trim()) {
    const fault = nameFault(name);
    if (fault) return { fault };
    if (target.kind === 'link') plan.options.name = name;
    // Links outside any package have no folder named after one to rename,
    // so they are put into the package instead.
    else if (target.name === '') plan.move = name;
    else plan.renamePackage = name;
  }
  if (target.kind === 'link' && draft.pkg.trim() !== start.pkg.trim()) plan.move = draft.pkg.trim();
  if (draft.dir.trim() !== start.dir.trim()) plan.options.dir = draft.dir.trim();
  if (draft.comment.trim() !== start.comment.trim()) plan.options.comment = draft.comment.trim();
  if (draft.password.trim() !== '') plan.options.password = draft.password.trim();
  if (draft.downloadPassword.trim() !== '') plan.options.downloadPassword = draft.downloadPassword.trim();
  if (draft.chunks.trim() !== start.chunks.trim() && draft.chunks.trim() !== '') {
    const n = Number(draft.chunks.trim());
    if (!Number.isInteger(n) || n < 0 || n > 16) return { fault: 'edit.chunksRange' };
    plan.options.chunks = n;
  }
  if (draft.category !== start.category && draft.category !== MIXED) plan.category = draft.category;
  return plan;
}

/** nameFault says what is wrong with a name before it is sent. The instance
 *  refuses such a name too, but in English. */
export function nameFault(name: string): TranslationKey | null {
  if (name === '') return 'edit.rename.empty';
  return /[\\/]/.test(name) ? 'edit.rename.separator' : null;
}

/** A refused rename's code (app.RenameRefusal) and what the window says for it. */
const RENAME_REFUSALS: Partial<Record<string, TranslationKey>> = {
  torrent: 'edit.rename.torrent',
  remote: 'edit.rename.remote',
  unpacking: 'edit.rename.unpacking',
  volume: 'edit.rename.volume',
  empty: 'edit.rename.empty',
  separator: 'edit.rename.separator',
  dots: 'edit.rename.dots',
  exists: 'edit.rename.exists',
  busy: 'edit.rename.busy',
  folderExists: 'edit.rename.folderExists',
  dataFolder: 'edit.rename.dataFolder',
  notMoved: 'edit.rename.notMoved',
};

/**
 * editRefusal words a refused save. A member asking for a folder outside the
 * download folders is told so, since ownLoginNeeded alone would send it to
 * the web UI for something it can fix here. Null leaves the wording to
 * errorText.
 */
export function editRefusal(
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string,
  e: unknown,
  sentDir: boolean,
): string | null {
  if (!(e instanceof ApiError) || !e.code) return null;
  if (e.code === 'ownLoginNeeded' && sentDir) return t('edit.folderRefused');
  const key = RENAME_REFUSALS[e.code];
  return key ? t(key, e.params) : null;
}

/**
 * renameNote is what the window says beside a link's name, by the rules of
 * app.renameLocked: that the name lands once the download has finished, or
 * that it cannot land at all. Null means it applies at once.
 */
export function renameNote(task: EditableTask): { key: TranslationKey; blocked: boolean } | null {
  if (task.infoHash) return { key: 'edit.rename.torrent', blocked: true };
  if (task.resolver === 'jd') return { key: 'edit.rename.remote', blocked: true };
  if (task.status === 'extracting') return { key: 'edit.rename.unpacking', blocked: true };
  if ((task.archivePart ?? 0) > 0) return { key: 'edit.rename.volume', blocked: true };
  if (task.status === 'running') return { key: 'edit.rename.whenDone', blocked: false };
  return null;
}

/** The priority words of the ladder's steps, by the id the instance sends. */
export const PRIORITY_KEYS: Partial<Record<string, TranslationKey>> = {
  highest: 'edit.priority.highest',
  higher: 'edit.priority.higher',
  high: 'edit.priority.high',
  default: 'edit.priority.default',
  low: 'edit.priority.low',
  lower: 'edit.priority.lower',
  lowest: 'edit.priority.lowest',
};
