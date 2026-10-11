import { useState } from 'react';
import type { SelfTestStatus } from '../../lib/api';
import { IconCheck, IconClose } from '../../lib/icons';
import { FAIL, FILL, GROUND, HUE, INFO, OK, QUIET, SURFACE, TRAVEL, WARN, useStage } from './scene';

export interface PulseCheck {
  id: string;
  name: string;
  /** `pending` until the run has an answer for it. */
  status: SelfTestStatus | 'pending';
  /** The status in a word, as the list of results says it. */
  word: string;
}

/** Where the beads stand and where their names go: side by side in a wide card, stacked in a narrow one. */
interface Arrangement {
  size: (beads: number) => { width: number; height: number };
  at: (i: number) => [x: number, y: number];
  /** The direction the spike of a beat points in. */
  up: [x: number, y: number];
  caption: (i: number) => { x: number; y: number; width: number; height: number };
  captionClass: string;
  className: string;
}

const ROW: Arrangement = {
  size: (beads) => ({ width: beads * 88, height: 100 }),
  at: (i) => [44 + i * 88, 30],
  up: [0, -1],
  caption: (i) => ({ x: 2 + i * 88, y: 50, width: 84, height: 50 }),
  captionClass: 'items-center text-center',
  className: 'hidden @[38rem]:block',
};

const COLUMN: Arrangement = {
  size: (beads) => ({ width: 290, height: beads * 40 + 12 }),
  at: (i) => [26, 26 + i * 40],
  up: [1, 0],
  caption: (i) => ({ x: 56, y: 8 + i * 40, width: 230, height: 36 }),
  captionClass: 'justify-center',
  className: 'block max-w-[20rem] @[38rem]:hidden',
};

const RADIUS = 11;
// One beat between two beads, as steps along the line and towards the spike.
const BEAT: [along: number, up: number][] = [[-12, 0], [-8, 3], [-5, 0], [-2, -4], [2, 13], [6, -8], [9, 0]];

const PAINT: Partial<Record<PulseCheck['status'], string>> = { pass: OK, warn: WARN, fail: FAIL, unknown: INFO };

/** The line from bead `from` to bead `to`, with a beat between each two. */
function line(a: Arrangement, from: number, to: number): [number, number][] {
  const points: [number, number][] = [a.at(from)];
  for (let i = from; i < to; i++) {
    const [x, y] = a.at(i);
    const [nx, ny] = a.at(i + 1);
    const [mx, my] = [(x + nx) / 2, (y + ny) / 2];
    const [dx, dy] = [Math.sign(nx - x), Math.sign(ny - y)];
    for (const [along, up] of BEAT) points.push([mx + along * dx + up * a.up[0], my + along * dy + up * a.up[1]]);
    points.push([nx, ny]);
  }
  return points;
}

const pathOf = (points: [number, number][]) => `M${points.map(([x, y]) => `${x} ${y}`).join('L')}`;
const lengthOf = (points: [number, number][]) =>
  points.slice(1).reduce((sum, [x, y], i) => sum + Math.hypot(x - points[i][0], y - points[i][1]), 0);

/**
 * Pulse draws the checks of a self-test as beads on a pulse line. While the
 * test runs, the beat stands on the check that is being made and moves on as
 * the answers come in, and each bead takes the colour of its answer. At rest
 * the line shows the last run.
 */
export function Pulse({ checks, running, label }: { checks: PulseCheck[]; running: boolean; label: string }) {
  const waiting = checks.findIndex((c) => c.status === 'pending');
  const beat = running ? waiting : -1;
  // Where the beat stood before, so it travels from there instead of jumping.
  const [seen, setSeen] = useState({ beat, from: beat });
  if (seen.beat !== beat) setSeen({ beat, from: seen.beat >= 0 && seen.beat < beat ? seen.beat : beat });

  const said = `${label}: ${checks.map((c) => `${c.name} ${c.word}`).join(', ')}`;
  return (
    <div data-new="selftest-pulse" data-state={running ? 'running' : 'rest'} className="@container">
      <Line arrangement={ROW} checks={checks} beat={beat} from={seen.from} label={said} />
      <Line arrangement={COLUMN} checks={checks} beat={beat} from={seen.from} label={said} />
    </div>
  );
}

function Line({
  arrangement: a,
  checks,
  beat,
  from,
  label,
}: {
  arrangement: Arrangement;
  checks: PulseCheck[];
  beat: number;
  from: number;
  label: string;
}) {
  const { stage, still } = useStage(String(beat));
  const { width, height } = a.size(checks.length);
  const last = checks.length - 1;
  // The line is in colour as far as the test has come, and all of it once a run has finished.
  const done = beat >= 0 ? beat : checks.some((c) => c.status !== 'pending') ? last : -1;
  const way = beat >= 0 ? line(a, Math.min(from, beat), beat) : [];
  const travel = lengthOf(way) / TRAVEL;
  // The ring starts to throb once the beat has arrived.
  const throb = { dur: '1.2s', begin: `${travel.toFixed(2)}s`, repeatCount: 'indefinite' };

  return (
    <svg
      ref={stage}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label={label}
      style={a === ROW ? { maxWidth: `${checks.length * 6.25}rem` } : undefined}
      className={`mx-auto w-full overflow-visible ${a.className}`}
    >
      <g fill="none" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round">
        <path d={pathOf(line(a, 0, last))} stroke={QUIET} strokeOpacity=".6" />
        {done > 0 && <path data-done={done} d={pathOf(line(a, 0, done))} stroke={HUE} />}
      </g>

      {checks.map((c, i) => {
        const [x, y] = a.at(i);
        const paint = PAINT[c.status];
        return (
          <g key={c.id} transform={`translate(${x} ${y})`} data-check={c.id} data-status={c.status}>
            {paint ? (
              <>
                <circle r={RADIUS} fill={paint} />
                <Verdict status={c.status} />
              </>
            ) : (
              <>
                <circle
                  r={RADIUS}
                  fill={GROUND}
                  stroke={i === beat ? HUE : c.status === 'skipped' ? QUIET : undefined}
                  strokeWidth={i === beat ? 2 : undefined}
                  strokeOpacity={i === beat ? undefined : 0.6}
                  strokeDasharray={c.status === 'skipped' ? '3 4' : undefined}
                />
                {c.status === 'pending' && <circle r="2.5" fill={QUIET} />}
              </>
            )}
          </g>
        );
      })}

      {beat >= 0 && !still && (
        <g key={beat} data-beat={beat}>
          {travel > 0 && (
            <circle r="4.5" fill={FILL}>
              <animateMotion path={pathOf(way)} dur={`${travel.toFixed(2)}s`} calcMode="linear" fill="freeze" />
              <animate attributeName="opacity" values="1;1;0" keyTimes="0;.98;1" dur={`${travel.toFixed(2)}s`} fill="freeze" />
            </circle>
          )}
          <circle cx={a.at(beat)[0]} cy={a.at(beat)[1]} r={RADIUS} fill="none" stroke={HUE} strokeWidth="2" opacity="0">
            <animate attributeName="r" values={`${RADIUS};${RADIUS + 12}`} {...throb} />
            <animate attributeName="opacity" values=".7;0" {...throb} />
          </circle>
        </g>
      )}

      {checks.map((c, i) => (
        <foreignObject key={c.id} {...a.caption(i)}>
          <div className={`flex h-full flex-col text-meta leading-tight ${a.captionClass}`}>
            <span className="max-w-full break-words text-carbon-text">{c.name}</span>
            <span className="max-w-full truncate text-carbon-textMuted">{c.word}</span>
          </div>
        </foreignObject>
      ))}
    </svg>
  );
}

/** The answer inside a bead, cut out in the colour of the surface behind. */
function Verdict({ status }: { status: PulseCheck['status'] }) {
  if (status === 'pass' || status === 'fail') {
    const Glyph = status === 'pass' ? IconCheck : IconClose;
    return (
      <g color={SURFACE}>
        <Glyph x={-6} y={-6} width={12} height={12} />
      </g>
    );
  }
  return (
    <text y="4.5" textAnchor="middle" fontSize="13" fontWeight="800" fill={SURFACE}>
      {status === 'warn' ? '!' : '?'}
    </text>
  );
}
