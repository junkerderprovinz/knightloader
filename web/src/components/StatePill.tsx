// The two small state marks of a card in a grid and of a row in a list: a
// tinted tag for a state somebody should notice, and a dot with plain words
// for one that is simply fine.
import { Fragment } from 'react';
import { InfoBubble } from './ui';

type Tone = 'ok' | 'warn' | 'fail' | 'neutral' | 'run';

const TONE: Record<Tone, string> = {
  ok: 'bg-statusOkBg text-statusOk',
  warn: 'bg-statusWarnBg text-statusWarn',
  fail: 'bg-statusFailBg text-statusFail',
  // Two tiers up, so it shows on a surface2 tile as well as on a card.
  neutral: 'bg-carbon-surface3 text-carbon-textSub',
  run: 'bg-accentSoft text-accentInk',
};

/** StatePill says in a word or two where something stands. `tip` holds the
 *  sentence behind the word. */
export function StatePill({ label, tone, tip }: { label: string; tone: Tone; tip?: string }) {
  return (
    <span
      className={`inline-flex h-[22px] shrink-0 items-center gap-1.5 whitespace-nowrap rounded-[min(6px,var(--radius-pill))] px-[9px]
        text-xs font-semibold ${TONE[tone]}`}
    >
      {tone === 'run' && <span aria-hidden="true" className="glim-live h-1.5 w-1.5 rounded-full bg-current" />}
      {label}
      {tip && <InfoBubble tip={tip} onColor />}
    </span>
  );
}

/** OkState is the quiet form of "all is well": a green dot and the words, with
 *  a middle dot between its parts. */
export function OkState({ parts }: { parts: string[] }) {
  return (
    <span className="glim-num inline-flex shrink-0 items-center gap-2 whitespace-nowrap text-[13px] text-carbon-textSub">
      <span aria-hidden="true" className="h-[7px] w-[7px] shrink-0 rounded-full bg-statusOkSolid" />
      {parts.map((part, i) => (
        <Fragment key={i}>
          {i > 0 && <span aria-hidden="true">·</span>}
          <span>{part}</span>
        </Fragment>
      ))}
    </span>
  );
}
