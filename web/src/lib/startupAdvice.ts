import type { TranslationKey } from './i18n';

/**
 * The remedy for a start-check finding, as failureAdvice.ts does for failed
 * downloads: the server sends a stable code and the interface owns the
 * sentence.
 *
 * A code with no entry gets no sentence; the row still shows the verdict and
 * the system's own message. A guessed remedy would send someone to change the
 * wrong thing.
 *
 * Lookups try "<id>:<code>" first and then "<code>": some failures mean the
 * same thing anywhere (a read-only mount), others depend on what failed (a
 * missing ffprobe costs the length check, a missing Java the encrypted
 * containers). A new tool inherits the general sentence.
 */
const startupAdvice: Partial<Record<string, TranslationKey>> = {
  // Folders. `missing` is not a red row: a fresh install's download folder is
  // created by the first write. It also looks like a share that failed to
  // mount, so the sentence names the nearest existing folder.
  missing: 'settings.diagnostics.fix.missing',
  denied: 'settings.diagnostics.fix.denied',
  readonly: 'settings.diagnostics.fix.readonly',
  full: 'settings.diagnostics.fix.full',
  // Writable but not deletable (a delete-denying SMB ACL, a sticky bit, no
  // free inodes): a download arrives and can never be moved.
  notRemoved: 'settings.diagnostics.fix.notRemoved',
  notADir: 'settings.diagnostics.fix.notADir',
  timeout: 'settings.diagnostics.fix.timeout',
  // The data directory costs the next save rather than downloads.
  'data:denied': 'settings.diagnostics.fix.dataDenied',
  'data:readonly': 'settings.diagnostics.fix.dataDenied',
  'data:full': 'settings.diagnostics.fix.dataDenied',

  // Tools.
  'java:notFound': 'settings.diagnostics.fix.javaMissing',
  'java:javaNotNeeded': 'settings.diagnostics.fix.javaNotNeeded',
  'ytdlp:notFound': 'settings.diagnostics.fix.ytdlpMissing',
  'ffmpeg:notFound': 'settings.diagnostics.fix.ffmpegMissing',
  'ffprobe:notFound': 'settings.diagnostics.fix.ffprobeMissing',
  // Installed but will not run, such as a yt-dlp with a broken Python.
  notRunnable: 'settings.diagnostics.fix.notRunnable',
  appearedLate: 'settings.diagnostics.fix.appearedLate',

  // The clock.
  utcFallback: 'settings.diagnostics.fix.utcFallback',
  noZoneDatabase: 'settings.diagnostics.fix.noZoneDatabase',
  tzUnset: 'settings.diagnostics.fix.tzUnset',
};

/** adviceFor returns the remedy for a check's id and code; a row without a
 *  code has nothing wrong. */
export function adviceFor(id: string, code: string | undefined): TranslationKey | undefined {
  if (!code) return undefined;
  return startupAdvice[`${id}:${code}`] ?? startupAdvice[code];
}
