import type { TranslationKey } from './i18n';

/**
 * What a start-check finding MEANS, and the one thing that helps.
 *
 * A sibling of failureAdvice.ts and built on the same rule, because it is the
 * same problem one layer out: the server sends a stable code, never prose, and
 * the interface owns the sentence in whichever of the forty-two languages the
 * reader is using.
 *
 * A CODE WITH NO ENTRY GETS NO SENTENCE. `Check.code` is an open string
 * (internal/startupcheck) and a newer server can answer with something this
 * build has never heard of. The row still draws - the verdict word, the folder
 * or the binary, and whatever the system itself said in `err` - and nothing
 * offers to fix it. A guessed remedy on a failure nobody has diagnosed is worse
 * than none: it sends somebody to change a thing that was never the problem, and
 * it costs the report its reader the first time it is wrong.
 *
 * WHY THE KEY IS "<id>:<code>" WITH A PLAIN "<code>" BEHIND IT. Half of these
 * failures mean the same thing wherever they turn up ("this folder is mounted
 * read-only" needs one sentence, not six) and half mean something different
 * depending on WHICH thing failed. A missing ffprobe costs the length check on a
 * finished video; a missing ffmpeg costs the sound on most media downloads;
 * a missing Java means an encrypted container link cannot be opened at all.
 * "notFound" alone cannot say any of that. So the specific lookup wins and the
 * general one catches everything else, which is also why adding a tool later
 * needs no change here at all - it inherits the general sentence until somebody
 * writes it a better one.
 *
 * Every value is a TranslationKey rather than a string, exactly as failureAdvice
 * types its own: a key renamed out from under this table becomes a compile error
 * instead of a bubble that prints its own key at somebody.
 */
const startupAdvice: Partial<Record<string, TranslationKey>> = {
  // ---- folders -----------------------------------------------------------
  //
  // `missing` is the one that has to be read carefully and it is deliberately
  // not a red row: a download folder that does not exist yet is the normal
  // state of a fresh install, because it is created by whoever writes the first
  // file into it. It is ALSO what a share that failed to mount looks like, so
  // the sentence names the nearest folder that does exist and lets the person
  // who knows their own box tell the two apart.
  missing: 'settings.diagnostics.fix.missing',
  denied: 'settings.diagnostics.fix.denied',
  readonly: 'settings.diagnostics.fix.readonly',
  full: 'settings.diagnostics.fix.full',
  // Written and not deletable again is its own state with its own remedy: an
  // SMB share with a delete-denying ACL, a sticky-bit directory, an exhausted
  // inode table. Its cost is different too - a download arrives and can then
  // never be renamed or moved out of the way - so it may not share a sentence
  // with "cannot write here".
  notRemoved: 'settings.diagnostics.fix.notRemoved',
  notADir: 'settings.diagnostics.fix.notADir',
  timeout: 'settings.diagnostics.fix.timeout',
  // The data directory is the one folder the instance cannot do without, so the
  // same errno needs a different sentence: everything else costs downloads,
  // this one costs the next thing anybody saves.
  'data:denied': 'settings.diagnostics.fix.dataDenied',
  'data:readonly': 'settings.diagnostics.fix.dataDenied',
  'data:full': 'settings.diagnostics.fix.dataDenied',

  // ---- tools -------------------------------------------------------------
  'java:notFound': 'settings.diagnostics.fix.javaMissing',
  'java:javaNotNeeded': 'settings.diagnostics.fix.javaNotNeeded',
  'ytdlp:notFound': 'settings.diagnostics.fix.ytdlpMissing',
  'ffmpeg:notFound': 'settings.diagnostics.fix.ffmpegMissing',
  'ffprobe:notFound': 'settings.diagnostics.fix.ffprobeMissing',
  // On PATH and it will not run, which is a completely different errand from
  // "go and install it": a yt-dlp whose Python environment broke is already
  // installed, and the row carries its own first line as the evidence.
  notRunnable: 'settings.diagnostics.fix.notRunnable',
  appearedLate: 'settings.diagnostics.fix.appearedLate',

  // ---- the clock ---------------------------------------------------------
  utcFallback: 'settings.diagnostics.fix.utcFallback',
  noZoneDatabase: 'settings.diagnostics.fix.noZoneDatabase',
  tzUnset: 'settings.diagnostics.fix.tzUnset',
};

/**
 * adviceFor is the only way to read the table.
 *
 * It takes the id and the code exactly as the server sent them, '' included: a
 * row with nothing wrong carries no code at all, and `startupAdvice['']` would
 * be a lookup that reads as if it might one day answer.
 */
export function adviceFor(id: string, code: string | undefined): TranslationKey | undefined {
  if (!code) return undefined;
  return startupAdvice[`${id}:${code}`] ?? startupAdvice[code];
}
