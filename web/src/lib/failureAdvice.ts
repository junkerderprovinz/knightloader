import type { TranslationKey } from './i18n';

/**
 * What a typed failure means and the one thing that helps: the sentence and
 * remedy for a single row, where columns.tsx's `reasonKey` is the short badge
 * word. Kept apart because reasonKey has other readers, such as Advanced.tsx's
 * per-reason retry settings.
 *
 * A reason with no entry gets no dialog and no button: the reason is an open
 * string, and the row keeps the hoster's own sentence.
 *
 * An action that cannot help is worse than none, so two causes stop at a
 * sentence. geoBlocked offers no connection picker, because yt-dlp downloads
 * are not routed through connections. extractorBroken offers no update,
 * because the image's packaged, unprivileged yt-dlp cannot update itself.
 */
export type FailureFix = 'cookies' | 'remove' | 'none';

export interface Advice {
  /** The plain sentence that replaces the backend's line on the row. */
  line: TranslationKey;
  /** The details, shown in an InfoBubble. */
  hint: TranslationKey;
  /** The single button. Present exactly when `fix` is not 'none'. */
  action?: TranslationKey;
  fix: FailureFix;
}

export const failureAdvice: Partial<Record<string, Advice>> = {
  // Two cookie causes: a bot check needs any signed-in session, members-only
  // needs an account that holds the membership.
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
  // Not specific to yt-dlp: a 404 from any hoster means the same to the reader.
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

/** adviceFor returns the advice for a failure reason; '' has none. */
export function adviceFor(reason: string | undefined): Advice | undefined {
  return reason ? failureAdvice[reason] : undefined;
}
