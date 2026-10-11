import { useT } from '../../lib/i18n';
import { IconAccounts, IconCaptcha } from '../../lib/icons';
import { ENTRY, Entry, LANE, Name, RADIUS, Stop, Track, WIDTH, numbered, stopAt } from './lane';
import { FILL, GROUND, HUE, INK, QUIET, SURFACE, keyed, trip, useStage } from './scene';
import { Traveller, loopOf } from './station';

// Seconds a station holds the captcha before it goes on, and how long the
// ring takes to run down. The ring stands for the configured wait, which is a
// minute or more and would hold the scene still.
const ASK = 0.7;
const RUN_DOWN = 1.6;

const RING = RADIUS - 1.5;
const AROUND = (2 * Math.PI * RING).toFixed(2);

export interface ChainSolver {
  id: string;
  name: string;
  /** Its place in the order, counted from 1, or 0 for a solver that is switched off. */
  place: number;
  /** Why a solver in the order gets no captcha: no key, or its account is off. */
  skipped?: string;
}

/**
 * CaptchaChain shows who a captcha goes to: to you first, then, once the time
 * for your answer has run down, to each captcha account in the order. `wait`
 * is that time in seconds, or null when the accounts start at once. `off`
 * stands the chain grey, for captchas that are switched off altogether.
 */
export function CaptchaChain({
  solvers,
  wait,
  off,
  label,
}: {
  solvers: ChainSolver[];
  wait: number | null;
  off: boolean;
  label: string;
}) {
  const { t } = useT();
  const take = [off, wait, ...solvers.map((s) => `${s.id}:${s.place}:${s.skipped ?? ''}`)].join('\n');
  const { stage, still: gated } = useStage(take);
  const still = gated || off;

  // The ring has nothing to hold back while no account is in the order.
  const ringed = solvers.some((s) => s.place > 0);
  const held = ringed && wait !== null && !off;
  const takes = (s: ChainSolver) => !off && s.place > 0 && !s.skipped;

  // How long each station holds the captcha, from the top: you, the ring, the
  // accounts. One that holds it for no time is passed.
  const holds = [ASK, ...(ringed ? [held ? RUN_DOWN : 0] : []), ...solvers.map((s) => (takes(s) ? ASK : 0))];
  const firstSolver = holds.length - solvers.length;
  // The captcha goes no further than the last station that takes it.
  const reach = holds.reduce((last, hold, i) => (hold > 0 ? i : last), 0);
  const points: [number, number][] = [[LANE, ENTRY], ...holds.slice(0, reach + 1).map((_, i): [number, number] => [LANE, stopAt(i)])];
  const walk = trip(points, (i) => (i === 0 ? 0 : holds[i - 1]));
  const loop = loopOf(walk);
  /** When the captcha reaches station `i`, or the end of the loop for one it never reaches. */
  const reached = (i: number) => walk.stops[i + 1] ?? { arrive: 1, leave: 1 };

  const waits = wait === null ? t('picture.captcha.atOnce') : t('picture.captcha.wait', { n: wait });
  const said = [
    t('picture.captcha.you'),
    ...(ringed ? [waits] : []),
    ...solvers.filter((s) => s.place > 0).map((s) => `${s.place}. ${s.name}${s.skipped ? ` (${s.skipped})` : ''}`),
  ];
  const foot = stopAt(holds.length - 1);

  return (
    <svg
      ref={stage}
      viewBox={`0 0 ${WIDTH} ${foot + RADIUS + 9}`}
      role="img"
      aria-label={`${label}: ${off ? t('settings.modules.off') : said.join(', ')}`}
      data-state={off ? 'off' : 'on'}
      data-new="captcha-chain"
      className="block w-full max-w-[24rem] overflow-visible"
    >
      <Track d={`M${LANE} ${ENTRY}V${foot}`} />
      <Entry>
        <IconCaptcha x={-7} y={-7} width={14} height={14} />
      </Entry>

      <g data-station="you">
        <Stop
          y={stopAt(0)}
          state={off ? 'passed' : 'takes'}
          face={(paint) => (
            <g color={paint}>
              <IconAccounts x={-7} y={-7} width={14} height={14} />
            </g>
          )}
          at={reached(0).arrive}
          loop={loop}
          still={still}
        />
        <Name y={stopAt(0)} quiet={off} note={t('picture.captcha.where')}>
          {t('picture.captcha.you')}
        </Name>
      </g>

      {ringed && (
        <g data-station="wait" data-state={held ? 'on' : 'off'}>
          {held ? (
            <g transform={`translate(${LANE} ${stopAt(1)})`}>
              <circle r={RADIUS} fill={GROUND} />
              <circle r={RING} fill="none" stroke={QUIET} strokeOpacity=".3" strokeWidth="3" />
              <circle
                r={RING}
                fill="none"
                stroke={HUE}
                strokeWidth="3"
                strokeDasharray={`${AROUND} ${AROUND}`}
                transform="rotate(-90)"
              >
                {!still && (
                  <animate
                    attributeName="stroke-dashoffset"
                    {...keyed([[0, 0], [reached(1).arrive, 0], [reached(1).leave, AROUND], [0.97, AROUND], [0.985, 0], [1, 0]])}
                    {...loop.smil}
                  />
                )}
              </circle>
              <text y="3" textAnchor="middle" fontSize={String(wait).length > 2 ? 7 : 8.5} fontWeight="700" fill={INK} className="glim-num">
                {wait}
              </text>
            </g>
          ) : (
            <Stop y={stopAt(1)} state="off" face={() => null} at={1} loop={loop} still={still} />
          )}
          <Name y={stopAt(1)} quiet={!held}>
            {waits}
          </Name>
        </g>
      )}

      {solvers.map((s, i) => {
        const y = stopAt(firstSolver + i);
        return (
          <g key={s.id} data-station={s.id} data-state={takes(s) ? 'on' : 'off'}>
            <Stop
              y={y}
              state={takes(s) ? 'takes' : 'off'}
              face={s.place > 0 ? numbered(s.place) : () => null}
              at={reached(firstSolver + i).arrive}
              loop={loop}
              still={still}
            />
            <Name y={y} quiet={!takes(s)} note={s.place > 0 ? s.skipped : t('settings.modules.off')}>
              {s.name}
            </Name>
          </g>
        );
      })}

      {!still && (
        <Traveller trip={walk} loop={loop} taking={walk.stops.filter((_, i) => i === 0 || holds[i - 1] > 0)} stays>
          <circle r="8" fill={FILL} />
          <g color={SURFACE}>
            <IconCaptcha x={-6} y={-6} width={12} height={12} />
          </g>
        </Traveller>
      )}
    </svg>
  );
}
