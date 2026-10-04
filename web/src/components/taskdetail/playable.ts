// MEDIA mirrors the audio and video half of inlineTypes in
// internal/api/routes_files.go, the only source the file route trusts for a
// type. It decides whether a player is offered and never sends a type to the
// server: that allowlist is a security boundary, and HTML, SVG or XML served
// inline at this origin would run with the session live.

export interface Playable {
  kind: 'audio' | 'video';
  /** Used only to ask canPlayType, never set on the element. */
  type: string;
  /** This browser's canPlayType verdict. */
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

// extensionOf follows Go's filepath.Ext, which the server uses: it stops at a
// separator, so "release.2024/readme" has no extension.
function extensionOf(name: string): string {
  for (let i = name.length - 1; i >= 0; i--) {
    const c = name[i];
    if (c === '/' || c === '\\') return '';
    if (c === '.') return name.slice(i).toLowerCase();
  }
  return '';
}

// One probe element per kind and one cached verdict per type, since the list
// re-renders every second while downloading.
const verdicts = new Map<string, boolean>();
let audioProbe: HTMLAudioElement | null = null;
let videoProbe: HTMLVideoElement | null = null;

function canPlay(kind: 'audio' | 'video', type: string): boolean {
  const cached = verdicts.get(type);
  if (cached !== undefined) return cached;
  if (typeof document === 'undefined') return false;
  let probe: HTMLMediaElement;
  if (kind === 'audio') probe = audioProbe ??= document.createElement('audio');
  else probe = videoProbe ??= document.createElement('video');
  // 'maybe' is the best a browser says about a container it has not opened.
  const answer = probe.canPlayType(type) !== '';
  verdicts.set(type, answer);
  return answer;
}

/**
 * playableAs returns null when the server does not serve the name inline as
 * audio or video, and otherwise the kind and whether this browser can play it.
 * It takes the task's name alone, since the server ignores the display-only Ext.
 */
function playableAs(name: string): Playable | null {
  const entry = MEDIA[extensionOf(name)];
  if (!entry) return null;
  return { kind: entry.kind, type: entry.type, supported: canPlay(entry.kind, entry.type) };
}

/**
 * playableTask is playableAs for a task. A torrent of several files is named
 * after its folder, so the kind comes from torrentMedia and the type from the
 * file route's answer, which names the file Play opens. Until that answer is
 * there, the browser's verdict is left open.
 */
export function playableTask(
  task: { name: string; torrentFileCount?: number; torrentMedia?: 'audio' | 'video' },
  contentType = '',
): Playable | null {
  if ((task.torrentFileCount ?? 0) <= 1) return playableAs(task.name);
  const kind = task.torrentMedia;
  if (!kind) return null;
  return { kind, type: contentType, supported: contentType === '' || canPlay(kind, contentType) };
}
