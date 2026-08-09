// The few settings the shell's quick panel edits, kept out of lib/api.ts for the
// same reason lib/connections.ts is: this is one screen's own vocabulary, and the
// module every page imports should not grow a section per widget.
//
// There is no `base` on either call, and that is not an oversight. Neither is
// forwarded to a peer — /api/instances/{name}/… only proxies the task, link and
// queue routes — so a base here would be a parameter whose only other value
// answers 403. The quick panel is this machine's, and says so.

/** What the server hands back: the knobs, plus the one bound it owns. */
export interface Controls {
  maxConcurrent: number;
  maxPerHost: number;
  /**
   * Connections ONE download opens, as configured. Zero means nobody has an
   * opinion and the dispatcher's own default applies.
   *
   * It is not a live socket count and there is no such number anywhere in this
   * app: a backend reports status, size, bytes and speed, and never how many
   * connections it holds. Anything on screen calling this "open connections"
   * would be inventing one.
   */
  chunks: number;
  /** The engine's ceiling for the above, so the spinner cannot offer more. */
  maxChunks: number;
  /** Bytes per second; 0 is unlimited. */
  speedLimit: number;
}

/**
 * A patch is only the fields being changed.
 *
 * Sending the whole object would be the very bug the route exists to prevent: a
 * widget that mounted an hour ago and writes back everything it is holding puts
 * back whatever the settings page changed in between.
 */
export type ControlsPatch = Partial<Omit<Controls, 'maxChunks'>>;

async function body<T>(r: Response): Promise<T> {
  if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
  return (await r.json()) as T;
}

export async function fetchControls(): Promise<Controls> {
  return body<Controls>(await fetch('/api/controls'));
}

/**
 * saveControls answers with what was actually STORED, not with what was sent —
 * the concurrency numbers are clamped on the way to disk. Callers adopt the
 * answer, which is what lets the interface carry no copy of those bounds.
 */
export async function saveControls(patch: ControlsPatch): Promise<Controls> {
  return body<Controls>(
    await fetch('/api/controls', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    }),
  );
}
