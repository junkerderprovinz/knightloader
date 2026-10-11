import type { Flavour, LinkReport, Rule } from '../RuleEditor';
import { useT } from '../../lib/i18n';
import { IconCheck, IconClose, IconFolderOpen, IconLink } from '../../lib/icons';
import { ENTRY, Entry, LANE, Name, PITCH, RADIUS, Stop, Track, WIDTH, numbered, stopAt } from './lane';
import { LinkParcel } from './PriorityFlow';
import { FAIL, HUE, trip, useStage } from './scene';
import { Traveller, loopOf } from './station';

// The second track, which a link turns onto where the rules are done with it,
// and where the names start beside it.
const SIDING = LANE + 26;
const NAMES = SIDING + 20;
// Seconds a rule that matches holds the link.
const ASK = 0.7;

/** The way one link takes through a list of rules. */
export interface Way {
  /** The rules that matched, by position, in the order they were asked. */
  matched: number[];
  /** The rule the link was done at, or -1 when it ran past every rule. */
  ends: number;
  rejected: boolean;
}

/**
 * wayOf reads a link's way out of its dry run. The server did the matching;
 * what is left is where the walk ended: at the first match when the list
 * stops there, and at the first matching rule that rejects.
 */
export function wayOf(link: LinkReport, rules: Rule[], stop: boolean, flavour: Flavour): Way {
  const { matched } = link;
  if (flavour === 'filter' && link.verdict.rejected) {
    const at = matched.findIndex((i) => rules[i].action.reject);
    if (at >= 0) return { matched: matched.slice(0, at + 1), ends: matched[at], rejected: true };
  }
  return { matched, ends: stop && matched.length > 0 ? matched[0] : -1, rejected: false };
}

export interface SwitchRule {
  name: string;
  /** Why the rule is never asked: it is switched off, or it has problems. */
  skipped?: string;
}

/** Where the link ends up, in the words of the test box. */
export interface Landing {
  kind: 'folder' | 'taken' | 'rejected';
  title: string;
  note?: string;
}

const LANDING_GLYPH = { folder: IconFolderOpen, taken: IconCheck, rejected: IconClose };

/**
 * RuleSwitch plays the way of one sample link through the rules: it comes in
 * at the top and passes them in order, every rule that matches takes it and
 * answers, and where the rules are done with it, it turns off the line and
 * lands. A rule below that point is never asked, which is why the order
 * counts. Without a sample the rules stand in their order and wait.
 */
export function RuleSwitch({
  rules,
  way,
  landing,
  off,
  label,
  onOpen,
}: {
  rules: SwitchRule[];
  way: Way | null;
  landing: Landing | null;
  /** The whole list is switched off. */
  off: boolean;
  label: string;
  onOpen: (index: number) => void;
}) {
  const { t } = useT();
  const going = !off && way !== null && landing !== null;
  const route = going ? `${way.matched.join()}|${way.ends}|${landing.kind}` : '';
  const { stage, still: gated } = useStage([off, ...rules.map((r) => r.skipped ?? ''), route].join('\n'));
  const still = gated || !going;

  const last = rules.length - 1;
  // A link no rule stops runs past the last one before it turns off.
  const turn = going && way.ends >= 0 ? way.ends : last;
  const foot = stopAt(last) + PITCH + 6;
  const points: [number, number][] = [
    [LANE, ENTRY],
    ...rules.slice(0, turn + 1).map((_, i): [number, number] => [LANE, stopAt(i)]),
    [SIDING, stopAt(turn) + PITCH / 2],
    [SIDING, foot],
  ];
  const takes = (i: number) => going && i <= turn && way.matched.includes(i);
  // Point 0 of the walk is the entry, so rule i stands at point i + 1.
  const walk = trip(points, (point) => (takes(point - 1) ? ASK : 0));
  const loop = loopOf(walk);
  const finish = walk.stops.length - 1;

  const told = going
    ? [
        ...(way.matched.length > 0 ? way.matched.map((i) => rules[i].name) : [t('settings.rules.resultNone')]),
        landing.title,
        ...(landing.note ? [landing.note] : []),
      ].join(' → ')
    : rules.map((r, i) => `${i + 1}. ${r.name}`).join(', ');
  const said = off ? t('settings.rules.setOff') : told;
  const Glyph = LANDING_GLYPH[landing?.kind ?? 'folder'];

  return (
    <svg
      ref={stage}
      viewBox={`0 0 ${WIDTH} ${foot + RADIUS + 9}`}
      role="group"
      aria-label={`${label}: ${said}`}
      data-state={off ? 'off' : going ? 'going' : 'waiting'}
      data-new="rule-switch"
      className="block w-full max-w-[24rem] overflow-visible"
    >
      <Track d={`M${LANE} ${ENTRY}V${stopAt(last)}L${SIDING} ${stopAt(last) + PITCH / 2}V${foot}`} />
      {going && (
        <path data-way d={walk.motion.path} fill="none" stroke={HUE} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      )}
      <Entry>
        <IconLink x={-7} y={-7} width={14} height={14} />
      </Entry>

      {rules.map((rule, i) => {
        const unasked = going && i > turn;
        const state = off || rule.skipped ? 'off' : takes(i) ? 'takes' : 'passed';
        return (
          <g key={i} data-rule={i} data-state={unasked ? 'unasked' : state} opacity={unasked ? 0.45 : undefined}>
            <Stop y={stopAt(i)} state={state} face={numbered(i + 1)} at={walk.stops[i + 1]?.arrive ?? 1} loop={loop} still={still} />
            <Name x={NAMES} y={stopAt(i)} quiet={state === 'off' || unasked} note={rule.skipped} onOpen={() => onOpen(i)}>
              {rule.name}
            </Name>
          </g>
        );
      })}

      <g data-landing={going ? landing.kind : ''}>
        <Stop
          x={SIDING}
          y={foot}
          state={going ? 'takes' : 'off'}
          fill={going && landing.kind === 'rejected' ? FAIL : undefined}
          face={(paint) => (
            <g color={paint}>
              <Glyph x={-7} y={-7} width={14} height={14} />
            </g>
          )}
          at={walk.stops[finish].arrive}
          loop={loop}
          still={still}
        />
        <Name x={NAMES} y={foot} quiet={!going} note={going ? landing.note : undefined}>
          {going ? landing.title : off ? t('settings.rules.setOff') : t('picture.rules.noSample')}
        </Name>
      </g>

      {!still && (
        <Traveller
          trip={walk}
          loop={loop}
          taking={walk.stops.filter((_, point) => point === 0 || point === finish || takes(point - 1))}
          stays
        >
          <LinkParcel />
        </Traveller>
      )}
    </svg>
  );
}
