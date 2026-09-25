// Wire types and helpers for the event programs: what this instance starts
// when something happens. The only part in lib/api.ts is the Settings field.
//
// The types mirror internal/eventprog's Go shapes, and the constants are copies
// of the Go constants with the same names.

import type { IdleCommandSpec } from './api';
import type { Placeholder } from './eventtargets';

/** idleaction.RedactedCommand. A stored program and its arguments are served as
 *  this and sent back untouched; the server puts the real ones back. */
export const REDACTED = '********';

/** eventprog.DefaultParallel and MaxParallel. 0 means no opinion and is
 *  stored as 0. */
export const DEFAULT_PARALLEL = 1;
export const MAX_PARALLEL = 4;

/** The bounds idleaction.CommandSpec.Sanitize holds a time limit to. */
export const TIMEOUT = { lo: 5, hi: 3600, fallback: 60 };

/** The variables every run gets, in the order internal/eventprog sets them. */
export const ENV_NAMES = [
  'KL_EVENT',
  'KL_TASK_ID',
  'KL_NAME',
  'KL_FILE',
  'KL_FOLDER',
  'KL_PACKAGE',
  'KL_CATEGORY',
  'KL_EXTRACT_OK',
];

/** One stored row, mirroring eventprog.Program. */
export interface EventProgramRow {
  id: string;
  name: string;
  enabled: boolean;
  /** Stored values come back as REDACTED and are sent back untouched. */
  command: IdleCommandSpec;
  triggers?: string[];
  /** 0 is "no opinion": it resolves to DEFAULT_PARALLEL. */
  parallel: number;
}

/** GET /api/eventprograms: the configured rows joined onto live health. */
export interface EventProgramStatus {
  id: string;
  name: string;
  enabled: boolean;
  /** What resolving the stored program found, without running it; absent
   *  when it would start. An idleaction.Problem code. */
  check?: string;
  /** RFC3339. Absent means nothing has run since the server started. */
  lastStart?: string;
  lastEvent?: string;
  lastOk?: string;
  /** An idleaction.Problem code; absent when the last run ended cleanly. */
  lastProblem?: string;
  lastExitCode: number;
  lastOutput?: string;
  lastDurationMs: number;
  runs: number;
  failed: number;
  dropped: number;
}

export async function fetchEventPrograms(): Promise<EventProgramStatus[]> {
  const r = await fetch('/api/eventprograms');
  if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
  return ((await r.json()) as EventProgramStatus[] | null) ?? [];
}

/**
 * fetchProgramPlaceholders returns the names a program's arguments may use, or
 * an empty list on failure so the list stays hidden rather than guessed.
 */
export async function fetchProgramPlaceholders(): Promise<Placeholder[]> {
  try {
    const r = await fetch('/api/eventprograms/placeholders');
    if (!r.ok) return [];
    return ((await r.json()) as Placeholder[] | null) ?? [];
  } catch {
    return [];
  }
}

/** stored reports whether a value is the mask for something the server keeps. */
export function isStored(value: string | undefined): boolean {
  return value === REDACTED;
}

/** storedArgs reports whether every argument is masked, which is how a stored
 *  argument list arrives. */
export function storedArgs(args: string[] | undefined): boolean {
  return (args ?? []).length > 0 && (args ?? []).every(isStored);
}

/**
 * clampParallel stores a number the way eventprog.Sanitize would, so the box
 * does not change once the save comes back.
 */
export function clampParallel(v: number): number {
  if (!Number.isFinite(v)) return 0;
  const whole = Math.round(v);
  if (whole <= 0) return 0;
  return Math.min(whole, MAX_PARALLEL);
}

/**
 * newProgramId returns a random id in the shape eventprog's identify hands out,
 * so the React key and the health row line up before the save returns. Never a
 * number that was free a moment ago: the server gives a stored command line
 * back to whichever row carries its id, and a reused id would hand a deleted
 * row's program to a new one. getRandomValues rather than randomUUID, which a
 * plain-HTTP install does not have.
 */
export function newProgramId(): string {
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  return [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
}
