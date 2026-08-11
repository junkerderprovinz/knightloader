import { useCallback } from 'react';
import { useUIState } from '../lib/uistate';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button } from './ui';
import { IconClose, IconHelp } from '../lib/icons';

// The single field every dismissed hint lives in — a growing array of keys,
// not one uistate field per hint. A field per hint is how this file would
// have to be edited again for the third page, and the fourth, forever; one
// array field costs nothing extra per hint and never needs a second look.
const FIELD = 'help.dismissed';

/**
 * useFirstTouch tracks whether ONE hint (identified by its own key, e.g.
 * "collector") has ever been dismissed, on the same server-persisted,
 * cross-browser bucket every other remembered UI choice in this app already
 * lives in — see lib/uistate.ts's own doc comment for why that is the one
 * place this kind of state belongs, and why SkippedLinks.tsx's session-only
 * `useState` (the closest existing "seen this" flag before this hook) is not
 * good enough for something meant to stay dismissed after a reload.
 *
 * Dismissing is one-way on purpose: a hint's whole job is to explain a
 * surface the FIRST time it is seen, and a hint that came back after being
 * read once would train a person to stop reading it, which defeats the one
 * thing it is for.
 */
export function useFirstTouch(key: string): { seen: boolean; dismiss: () => void } {
  const [dismissed, setDismissed] = useUIState<string[]>(FIELD, []);
  const seen = dismissed.includes(key);
  const dismiss = () => {
    if (!seen) setDismissed([...dismissed, key]);
  };
  return { seen, dismiss };
}

/**
 * Copy for every hint this build ships, keyed by the same id a page passes to
 * `<FirstTouchHint id="…">`. Centralised here rather than left for each page
 * to pass its own title/body: the id IS the content, and a page reaching for
 * its own strings is how a hint and its dismissed-key would drift out of the
 * pair they have to stay in.
 *
 * These six keys already live in en.ts, but not yet in the other 41 locale
 * files — that translation pass is a dedicated, later wave (see en.ts's own
 * comment above the `hint.*` block), not this one. Kept here as a PENDING
 * table anyway, same arrangement every other newly-added page in this build
 * used while its own keys were mid-rollout (Torrents.tsx, Help.tsx's own
 * history): cx() below asks the real catalogue first and only falls back to
 * this table, so a reader on any language other than English still gets a
 * real sentence today instead of raw English or an empty string, and the
 * fallback simply stops being consulted the day that translation pass
 * lands — nothing here needs to change when it does.
 */
const PENDING = {
  'hint.collector.title': 'Links are staged here, not downloaded yet',
  'hint.collector.body':
    'Everything you add lands in this collector first, where you can check names and sizes before anything starts. Start it here, or turn on auto-start in settings if you never want that pause.',
  'hint.downloads.title': 'What is actually running',
  'hint.downloads.body':
    'This is the transfer queue — links you have started, whether they are running, waiting their turn, or already finished. Nothing you have merely staged in the collector shows up here yet.',
  'hint.instances.title': 'A peer, not a copy',
  'hint.instances.body':
    'Adding another KnightLoader here does not move or sync anything — this instance simply calls that one’s own API to show and control its queue, the same way a browser would.',
} as const;

/** The set of hints this build knows how to draw — one entry per page that calls FirstTouchHint. */
export type HintId = 'collector' | 'downloads' | 'instances';

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return useCallback(
    (key: PendingKey) => {
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      return translated ?? PENDING[key];
    },
    [t],
  );
}

/**
 * FirstTouchHint is a page's own "before you start" callout: one or two
 * honest sentences about what the surface actually does, shown once and
 * never again once dismissed.
 *
 * Styled as InfoBubble's proactive sibling rather than a third look: the same
 * quiet, furniture-coloured glyph InfoBubble uses for its (i), the same
 * `glim-well` shell SkippedLinks already uses for a quiet strip above a list,
 * and the same close button every dismissible strip in this app already has.
 * A page renders this once, near its own top, passing the id its own row in
 * PENDING above is keyed under — that id doubles as the dismissed-key, so a
 * hint and its own persisted "have I shown this" flag can never point at two
 * different strings.
 */
export function FirstTouchHint({ id }: { id: HintId }) {
  const { t } = useT();
  const cx = useCx();
  const { seen, dismiss } = useFirstTouch(id);

  if (seen) return null;

  return (
    <div role="note" className="glim-well glim-fade flex items-start gap-3 px-4 py-3 text-xs">
      <span className="mt-0.5 shrink-0 text-carbon-textMuted" aria-hidden="true">
        <IconHelp width={15} height={15} />
      </span>
      <span className="flex flex-1 flex-col gap-0.5">
        <span className="font-medium text-carbon-text">{cx(`hint.${id}.title` as PendingKey)}</span>
        <span className="text-carbon-textSub">{cx(`hint.${id}.body` as PendingKey)}</span>
      </span>
      <Button
        kind="ghost"
        icon={<IconClose width={14} height={14} />}
        aria-label={t('common.dismiss')}
        onClick={dismiss}
      />
    </div>
  );
}
