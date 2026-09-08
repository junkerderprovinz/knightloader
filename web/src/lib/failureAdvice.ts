import type { TranslationKey } from './i18n';

/**
 * What a typed failure MEANS, and the one thing that helps.
 *
 * This is a second table beside columns.tsx's `reasonKey`, and the two stay
 * apart on purpose. reasonKey is the badge word: a category, one or two words,
 * read at a glance in a list of thousands. This is the sentence and the remedy,
 * read once, by somebody who has stopped scrolling and wants to know what to do
 * about one row. Folding them together would also grow an "action" column onto
 * a table that has three other consumers - Advanced.tsx iterates reasonKey to
 * build the per-reason retry spinners, and a remedy is not a retry count.
 *
 * A REASON WITH NO ENTRY GETS NO DIALOG AND NO BUTTON. The server's reason is
 * an open string (core.Reason) and a newer backend can settle a task with a
 * value this build has never heard of, so the same rule reasonKey already
 * follows applies here: no honest word, so no word. The row keeps the hoster's
 * own sentence and nothing offers to fix it.
 *
 * AN ACTION OFFERED WHERE IT CANNOT HELP IS WORSE THAN NO ACTION, which is why
 * two of the six causes below deliberately end in a sentence and stop:
 *
 *   geoBlocked has no connection picker. yt-dlp downloads are not routed - the
 *   backend interface takes no route, buildArgs passes no --proxy, and the one
 *   lever that does reach the process is the environment this server was
 *   started in, which nothing in this interface can set. A picker there would
 *   be the interface implying something the server cannot do.
 *
 *   extractorBroken has no "update yt-dlp". The image installs it through the
 *   package manager and runs unprivileged, so yt-dlp refuses to self-update and
 *   could not write over itself if it agreed to. That button would ship as a
 *   button that always fails, on the one case where somebody is already annoyed.
 *
 * Every value is a TranslationKey rather than a string, the same way reasonKey
 * is typed: a key renamed out from under this table is then a compile error
 * instead of a dialog printing its own key at somebody.
 */
export type FailureFix = 'cookies' | 'remove' | 'none';

export interface Advice {
  /** The plain sentence that replaces the backend's line on the row. */
  line: TranslationKey;
  /** The depth, and it belongs in an InfoBubble - never loose under a button. */
  hint: TranslationKey;
  /** The single button. Present exactly when `fix` is not 'none'. */
  action?: TranslationKey;
  fix: FailureFix;
}

export const failureAdvice: Partial<Record<string, Advice>> = {
  // Both cookie cases, and they are two causes rather than one because the
  // remedy is only half the same: a bot check wants any signed-in session, a
  // members-only video wants the session of an account that actually holds the
  // membership. Telling somebody who pasted a working YouTube jar that their
  // cookies are the problem, when the account simply is not a member, is how a
  // remedy that works becomes a remedy nobody trusts.
  botCheck: {
    line: 'failure.botCheck.line',
    hint: 'failure.botCheck.hint',
    action: 'failure.botCheck.action',
    fix: 'cookies',
  },
  membersOnly: {
    line: 'failure.membersOnly.line',
    hint: 'failure.membersOnly.hint',
    action: 'failure.membersOnly.action',
    fix: 'cookies',
  },
  geoBlocked: {
    line: 'failure.geoBlocked.line',
    hint: 'failure.geoBlocked.hint',
    fix: 'none',
  },
  // 'gone' is deliberately NOT a yt-dlp-only entry: a 404 from an ordinary
  // hoster means exactly the same thing to the person reading the row and
  // deserves the same sentence. That is also why the raw block in the dialog is
  // labelled backend-neutrally rather than naming yt-dlp.
  gone: {
    line: 'failure.gone.line',
    hint: 'failure.gone.hint',
    action: 'failure.gone.action',
    fix: 'remove',
  },
  drm: {
    line: 'failure.drm.line',
    hint: 'failure.drm.hint',
    action: 'failure.drm.action',
    fix: 'remove',
  },
  extractorBroken: {
    line: 'failure.extractorBroken.line',
    hint: 'failure.extractorBroken.hint',
    fix: 'none',
  },
};

/**
 * adviceFor is the only way to read the table.
 *
 * It takes the reason exactly as the server sent it, '' included - a task that
 * failed with nothing recognised carries an empty reason, and `failureAdvice['']`
 * would be a lookup that reads as if it might one day answer.
 */
export function adviceFor(reason: string | undefined): Advice | undefined {
  return reason ? failureAdvice[reason] : undefined;
}
