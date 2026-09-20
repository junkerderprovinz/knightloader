// The client for user scripts (internal/script, routes_scripts.go). The types
// mirror internal/script's Go shapes field for field.
//
// Scripts are saved one at a time (POST to create, PUT by id), never as a
// whole list, so two tabs editing different scripts cannot erase each other's
// code. `trigger` is an open string and the trigger list is fetched, so a
// newer server's trigger still renders. runScript takes at most one task,
// because the sandbox binds every task-scoped function to a single task id.

/** internal/script.Trigger's values, open for ones a newer build adds. */
export type ScriptTrigger = 'manual' | 'task.done' | 'task.failed' | 'queue.idle' | (string & {});

/** Shown until GET /api/scripts/triggers answers; the values Trigger.Valid()
 *  accepts. */
export const FALLBACK_TRIGGERS: ScriptTrigger[] = ['manual', 'task.done', 'task.failed', 'queue.idle'];

export interface ScriptInput {
  name: string;
  trigger: ScriptTrigger;
  enabled: boolean;
  code: string;
  /** Omitted or 0 means 5 s; the server clamps to 100..30000. */
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
  /** One entry per log() or console.* call, oldest first, capped by the server. */
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

// decode works like api.ts's json(): a refusal throws the {error,code}
// envelope where the route sends one, and the raw text otherwise.
async function decode<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const body = (await r.text()).trim();
    try {
      const p = JSON.parse(body) as { error?: string; code?: string };
      if (p && typeof p.error === 'string') throw new ScriptApiError(p.error, p.code);
    } catch (e) {
      if (e instanceof ScriptApiError) throw e;
      // Plain text, the usual case.
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
 * runScript serves both the editor's test run (no taskId) and the action
 * button on a download (one taskId), so a script is tested the way it runs.
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
 * fetchScriptTriggers asks the registry which triggers it fires. It never
 * throws and never returns an empty list: failure or emptiness gives
 * FALLBACK_TRIGGERS.
 */
export async function fetchScriptTriggers(): Promise<ScriptTrigger[]> {
  try {
    const list = await decode<string[]>(await fetch('/api/scripts/triggers'));
    return list && list.length > 0 ? list : FALLBACK_TRIGGERS;
  } catch {
    return FALLBACK_TRIGGERS;
  }
}
