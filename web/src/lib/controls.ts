// The few settings the shell's quick panel edits. Neither call takes a base:
// the routes are not forwarded to peers, so the panel always describes this
// machine.

/** What the server hands back: the knobs, plus the one bound it owns. */
export interface Controls {
  maxConcurrent: number;
  maxPerHost: number;
  /** Connections one download opens, as configured; 0 leaves it to the
   *  dispatcher's default. Not a live socket count, which no backend reports. */
  chunks: number;
  /** The engine's ceiling for the above, so the spinner cannot offer more. */
  maxChunks: number;
  /** Bytes per second; 0 is unlimited. */
  speedLimit: number;
}

/** Only the fields being changed, so an old widget cannot write back values
 *  the settings page has changed since. */
export type ControlsPatch = Partial<Omit<Controls, 'maxChunks'>>;

async function body<T>(r: Response): Promise<T> {
  if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
  return (await r.json()) as T;
}

export async function fetchControls(): Promise<Controls> {
  return body<Controls>(await fetch('/api/controls'));
}

/** saveControls answers with what was stored after clamping, so the UI needs
 *  no copy of the bounds. */
export async function saveControls(patch: ControlsPatch): Promise<Controls> {
  return body<Controls>(
    await fetch('/api/controls', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    }),
  );
}
