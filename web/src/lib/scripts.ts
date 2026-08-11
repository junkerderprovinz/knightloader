// Wave 11B: the frontend half of KnightLoader's scripting feature - the
// census's "Script Editor" and "Toolbar / Contextmenu Button Pressed" rows
// (docs/jd-feature-census.md, family E).
//
// internal/script (11A) and internal/api/routes_scripts.go (wired during
// Wave 11's review pass, since no agent's file list included it) are both
// real now: Host, Fire, RunNow, the trigger index, and every route below all
// exist and are exercised by internal/api's own test suite. The REST shape
// this file talks to was this file's own proposal at the time it was
// written, and routes_scripts.go was built to match it field for field
// rather than the other way round - so nothing here changed once the route
// file landed.
//
// The types below are the actual Go shapes from internal/script/script.go,
// field for field, because the package's own JSON tags are the wire format
// routes_scripts.go serialises verbatim:
//   - Trigger's real values are "task.done" / "task.failed" / "queue.idle" /
//     "manual" (script.go) - dotted, not the underscored guesses an earlier
//     version of this file shipped before internal/script existed to check
//     against.
//   - Result.Output is string[], one line per log()/console.* call
//     (sandbox.go's maxLogLines/maxLogBytes caps what lands in it), not one
//     block of text.
//   - Script carries TimeoutMS (script.go's DefaultTimeout/MinTimeout/
//     MaxTimeout = 5s default, 100ms-30s range) and NOT any persisted
//     last-run status - internal/script has no field for that anywhere, so
//     Scripts.tsx does not invent one to show in the list.
//
// Design choices in the REST shape, each for a reason that already bit a
// previous wave:
//
//   - PER-SCRIPT save (POST to create, PUT keyed by id to update), never a
//     whole-list PUT. Schedule.tsx can replace its whole `entries` array in
//     one PUT because a timetable row is small and edited by one person at a
//     time; a script carries real authored code, and two browser tabs open on
//     the same script must not let the second Save silently erase the first
//     person's edit - store.go's own doc comment gives the identical reason
//     for scripts.json being its own file rather than living in
//     settings.Settings, one layer further out.
//   - `trigger` is an open string on this side even though internal/script's
//     own Trigger is a closed Go type - matching `Reason` and `Origin` in
//     lib/api.ts and `ScheduleAction` in settings/Schedule.tsx: a build newer
//     than this frontend could add a fifth trigger, and a value this build
//     does not recognise must still render as itself instead of failing to
//     compile.
//   - the trigger vocabulary is fetched, never hard-coded into a closed
//     union - see fetchScriptTriggers. Same reasoning as GET /api/options
//     (api.ts's ApiOptions): the registry is the one source of what can
//     actually fire, and a picker built from a guess here would offer
//     triggers the server cannot honour and omit ones it can.
//   - runScript takes at most ONE taskId, not a list. internal/script's own
//     Actions interface and sandbox.go's taskGlobal close every task-scoped
//     closure over exactly one taskID chosen in Go - "there is no function
//     anywhere in this package's JS surface that TAKES a task ID as a
//     script-supplied argument" is the package doc comment's own words for
//     why. A batch-run button built here would be offering something the
//     one execution model underneath it cannot do.
//
// Not routed through lib/api.ts's private json()/ok() helpers - they are not
// exported, and lib/api.ts is a one-writer-per-wave hot lane (build-plan.md
// section 2, lane L6) that at least 11C (PATCH /api/settings) and 11E
// (resolver options) also want this same wave. decode() below is a small
// local mirror of the same {error,code} envelope, the same way
// settings/Schedule.tsx wrote its own saveSchedule() rather than reaching
// into api.ts.

/**
 * Mirrors internal/script.Trigger's real constants exactly (script.go), kept
 * open (the `(string & {})` branch) for a value a newer build defines that
 * this frontend does not know about yet - see the file doc comment.
 */
export type ScriptTrigger = 'manual' | 'task.done' | 'task.failed' | 'queue.idle' | (string & {});

/**
 * Shown until GET /api/scripts/triggers answers - see fetchScriptTriggers.
 * The exact four values internal/script.Trigger.Valid() currently accepts
 * (script.go), not a guess.
 */
export const FALLBACK_TRIGGERS: ScriptTrigger[] = ['manual', 'task.done', 'task.failed', 'queue.idle'];

export interface ScriptInput {
  name: string;
  trigger: ScriptTrigger;
  enabled: boolean;
  code: string;
  /** 0 (omitted) means DefaultTimeout (5s); otherwise clamped server-side to
   *  [MinTimeout, MaxTimeout] = [100, 30000] - script.go's own constants. */
  timeoutMs?: number;
}

export interface Script extends ScriptInput {
  id: string;
  createdAt?: string;
  updatedAt?: string;
}

/** Mirrors internal/script.Result (script.go) field for field. */
export interface ScriptRunResult {
  scriptId: string;
  name: string;
  trigger: ScriptTrigger;
  taskId?: string;
  startedAt: string;
  durationMs: number;
  /** One entry per log()/console.* call, oldest first - already capped
   *  server-side (sandbox.go's maxLogLines/maxLogBytes). */
  output?: string[];
  ok: boolean;
  error?: string;
  timedOut?: boolean;
}

export class ScriptApiError extends Error {
  code?: string;
  constructor(message: string, code?: string) {
    super(message);
    this.name = 'ScriptApiError';
    this.code = code;
  }
}

/** Mirrors api.ts's json() decoder: the {error,code} envelope where a route
 *  sends one, the raw text otherwise - never JSON.parse'd blind, which is
 *  the exact bug api.ts's own doc comment on json() describes fixing for
 *  saveSettings(). */
async function decode<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const body = (await r.text()).trim();
    try {
      const p = JSON.parse(body) as { error?: string; code?: string };
      if (p && typeof p.error === 'string') throw new ScriptApiError(p.error, p.code);
    } catch (e) {
      if (e instanceof ScriptApiError) throw e;
      // Not JSON at all, which is the ordinary case (and, right now, the
      // guaranteed one - see the file doc comment).
    }
    throw new ScriptApiError(body || String(r.status));
  }
  if (r.status === 204) return undefined as T;
  const text = await r.text();
  return text ? (JSON.parse(text) as T) : (undefined as T);
}

const jsonHeaders = { 'Content-Type': 'application/json' };

export async function fetchScripts(): Promise<Script[]> {
  return (await decode<Script[]>(await fetch('/api/scripts'))) ?? [];
}

export async function createScript(input: ScriptInput): Promise<Script> {
  return decode<Script>(
    await fetch('/api/scripts', { method: 'POST', headers: jsonHeaders, body: JSON.stringify(input) }),
  );
}

export async function updateScript(id: string, input: ScriptInput): Promise<Script> {
  return decode<Script>(
    await fetch(`/api/scripts/${encodeURIComponent(id)}`, {
      method: 'PUT',
      headers: jsonHeaders,
      body: JSON.stringify(input),
    }),
  );
}

export async function deleteScript(id: string): Promise<void> {
  await decode<void>(await fetch(`/api/scripts/${encodeURIComponent(id)}`, { method: 'DELETE' }));
}

/**
 * runScript is both "Test Run" (no taskId, called from the editor) and the
 * user action button (one taskId, called from ScriptActions.tsx) - one
 * route rather than two, because a script that only works one of those two
 * ways is not actually tested before it is wired to a real download. See
 * the file doc comment for why this is at most ONE task, never a list.
 */
export async function runScript(id: string, taskId?: string): Promise<ScriptRunResult> {
  return decode<ScriptRunResult>(
    await fetch(`/api/scripts/${encodeURIComponent(id)}/run`, {
      method: 'POST',
      headers: jsonHeaders,
      body: JSON.stringify(taskId ? { taskId } : {}),
    }),
  );
}

/**
 * fetchScriptTriggers asks the trigger registry what it actually fires on.
 * Never throws and never resolves empty - a picker with nothing pickable is
 * worse than one offering the honest fallback list, so a network failure or
 * a genuinely empty answer both land on FALLBACK_TRIGGERS.
 */
export async function fetchScriptTriggers(): Promise<ScriptTrigger[]> {
  try {
    const list = await decode<string[]>(await fetch('/api/scripts/triggers'));
    return list && list.length > 0 ? list : FALLBACK_TRIGGERS;
  } catch {
    return FALLBACK_TRIGGERS;
  }
}
