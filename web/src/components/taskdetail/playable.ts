// Which of a task's own files this app is willing to offer a player for, and
// what the browser says about actually playing them.
//
// The table below is the audio/video half of internal/api/routes_files.go's
// inlineTypes, verbatim. That map is the ONLY thing the file route trusts to
// say what a file is: the type is never sniffed from the bytes, never taken
// from the resolver and never taken from the request, so the client can work
// out the same answer here and know the response is going to agree with it.
//
// It lives in a file of its own, with no JSX in it, for two reasons: the
// mirror of a Go map belongs in exactly one place where it can be read against
// its counterpart without opening a component, and a plain module is testable
// without a renderer.
//
// TRAP, and it is a security one rather than a cosmetic one: this table only
// decides whether a player is OFFERED. It must never grow past what the Go map
// serves inline, nothing here may ever be sent to the server as a type hint,
// and no element built from it may carry a `type` attribute that disagrees
// with the response. routes_files.go's allowlist is a boundary, not a
// convenience list: HTML, SVG and XML are left out of it on purpose, because
// served inline at this app's own origin they would run with this app's
// session live in the tab.

/** One entry of the allowlist, plus this browser's verdict on it. */
export interface Playable {
  kind: 'audio' | 'video';
  /**
   * The type the server will label this extension with. Read here only to ask
   * canPlayType; it is never written onto the element and never sent back.
   */
  type: string;
  /**
   * Whether THIS browser says it can play it. Asked rather than assumed:
   * whether an mkv, a mov or an avi plays is the browser's answer and nobody
   * else's, and the same file gets a different answer in Safari and in Chrome.
   */
  supported: boolean;
}

const MEDIA: Record<string, { kind: 'audio' | 'video'; type: string }> = {
  '.mp3': { kind: 'audio', type: 'audio/mpeg' },
  '.m4a': { kind: 'audio', type: 'audio/mp4' },
  '.wav': { kind: 'audio', type: 'audio/wav' },
  '.flac': { kind: 'audio', type: 'audio/flac' },
  '.ogg': { kind: 'audio', type: 'audio/ogg' },
  '.mp4': { kind: 'video', type: 'video/mp4' },
  '.m4v': { kind: 'video', type: 'video/mp4' },
  '.webm': { kind: 'video', type: 'video/webm' },
  '.mkv': { kind: 'video', type: 'video/x-matroska' },
  '.mov': { kind: 'video', type: 'video/quicktime' },
  '.avi': { kind: 'video', type: 'video/x-msvideo' },
};

/**
 * The extension, worked out the way Go's filepath.Ext works it out, because
 * that is literally what decides the answer on the other end: routes_files.go
 * takes filepath.Ext of the task's stored name and looks it up.
 *
 * Scanning backwards and stopping at a separator is the whole of Go's rule, so
 * ".mp3" as a complete name genuinely has the extension ".mp3" there, and
 * "release.2024/readme" genuinely has none. A lastIndexOf('.') on its own gets
 * the second case wrong and would offer a player for a folder-shaped name.
 */
function extensionOf(name: string): string {
  for (let i = name.length - 1; i >= 0; i--) {
    const c = name[i];
    if (c === '/' || c === '\\') return '';
    if (c === '.') return name.slice(i).toLowerCase();
  }
  return '';
}

// One detached probe element per session, and one cached verdict per type.
// A task list repaints every second while something is downloading, and
// building a fresh <video> on each of those renders only to ask it one
// question is an allocation nobody asked for.
const verdicts = new Map<string, boolean>();
let audioProbe: HTMLAudioElement | null = null;
let videoProbe: HTMLVideoElement | null = null;

function canPlay(kind: 'audio' | 'video', type: string): boolean {
  const cached = verdicts.get(type);
  if (cached !== undefined) return cached;
  // No document means no browser to ask, so nothing is offered. A player that
  // appeared on the strength of a guess would be a control that does nothing.
  if (typeof document === 'undefined') return false;
  let probe: HTMLMediaElement;
  if (kind === 'audio') probe = audioProbe ??= document.createElement('audio');
  else probe = videoProbe ??= document.createElement('video');
  // canPlayType answers '', 'maybe' or 'probably', and the two hopeful answers
  // are the only ones a browser ever gives for a container it has not opened.
  // Anything but the empty string is offered.
  const answer = probe.canPlayType(type) !== '';
  verdicts.set(type, answer);
  return answer;
}

/**
 * playableAs is "is this a media file at all, and can this browser have it".
 *
 * Null means the name is not one of the types the file route serves inline as
 * audio or video, which is a different thing from "the browser refuses it":
 * the first has no player card to draw at all, the second has a card with a
 * sentence in it. Folding the two together would put a dead Play card under
 * every archive in the list.
 *
 * The name is the task's own `name` and nothing else. internal/app's
 * filename() returns t.Name on its own, routes_files.go takes filepath.Ext of
 * exactly that, and core.Task.Ext is a display-only guess that is deliberately
 * not part of the stored name. Appending it here would ask the browser about a
 * file the server is never going to serve.
 */
export function playableAs(name: string): Playable | null {
  const entry = MEDIA[extensionOf(name)];
  if (!entry) return null;
  return { kind: entry.kind, type: entry.type, supported: canPlay(entry.kind, entry.type) };
}
