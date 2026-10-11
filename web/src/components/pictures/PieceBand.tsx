import type { PieceMap } from '../../lib/api';
import { fmtTotal } from '../../lib/format';
import type { useT } from '../../lib/i18n';
import { FAIL, FILL, GROUND, INK, SURFACE, useStill } from './scene';

// The band is drawn 1000 wide and stretched to its card, so a place in the
// file is a share of that whatever the window. Strokes keep their pixels.
const WIDTH = 1000;
const BAND = 18;
const TALL = 36;
// Pieces are told apart by a line between them while they are few enough to
// leave room for one.
const COUNTABLE = 64;
// A reading arrives every second, and what moved in between glides there.
const GLIDE = 'transform 1s linear';

interface Stretch {
  from: number;
  to: number;
}

export interface Band {
  written: Stretch[];
  lost: Stretch[];
  /** Where each connection writes, from the start of the file on. */
  heads: number[];
  done: number;
  lostUnits: number;
}

/** bandOf lays a map out along the band. */
export function bandOf(map: PieceMap): Band {
  const scale = map.total > 0 ? WIDTH / map.total : 0;
  const band: Band = { written: [], lost: [], heads: [], done: 0, lostUnits: 0 };
  let at = 0;
  for (const run of map.runs) {
    const stretch = { from: at * scale, to: (at + run.len) * scale };
    at += run.len;
    if (run.state === 'done') {
      band.done += run.len;
      const last = band.written[band.written.length - 1];
      if (last && last.to === stretch.from) last.to = stretch.to;
      else band.written.push(stretch);
    } else if (run.state === 'lost') {
      band.lostUnits += run.len;
      band.lost.push(stretch);
    }
  }
  // A byte is a point on the band; an article or a block has a middle.
  const middle = map.unit === 'bytes' ? 0 : 0.5;
  band.heads = (map.heads ?? []).map((unit) => (unit + middle) * scale).sort((a, b) => a - b);
  return band;
}

/** pieceWords says in words what the band shows, or nothing for a unit this build does not know. */
export function pieceWords(map: PieceMap, t: ReturnType<typeof useT>['t']): string {
  const { done, lostUnits: lost } = bandOf(map);
  const n = map.heads?.length ?? 0;
  switch (map.unit) {
    case 'bytes':
      return t('picture.pieces.bytes', { done: fmtTotal(done), total: fmtTotal(map.total), n });
    case 'articles':
      return t('picture.pieces.articles', { done, total: map.total, lost, n });
    case 'blocks':
      return t('picture.pieces.blocks', { done, total: map.total, lost });
    default:
      return '';
  }
}

// A stretch that reaches an end of the band runs a little past it, or the track shows as a hairline in the rounded end.
function placed(s: Stretch): string {
  const from = s.from <= 0 ? -2 : s.from;
  const to = s.to >= WIDTH ? WIDTH + 2 : s.to;
  return `translateX(${from.toFixed(2)}px) scaleX(${(to - from).toFixed(2)})`;
}

/**
 * PieceBand draws one file from its start to its end: what is written is
 * filled, a head stands where each connection is writing, and what arrived
 * damaged or not at all is lit until something replaces it. `mending` says a
 * repair is under way and replacing the lit parts.
 */
export function PieceBand({ map, label, mending = false }: { map: PieceMap; label: string; mending?: boolean }) {
  const still = useStill();
  const band = bandOf(map);
  const glide = still ? undefined : GLIDE;
  const pieces = map.unit !== 'bytes' && map.total <= COUNTABLE ? map.total : 0;

  return (
    <div role="img" aria-label={label} data-unit={map.unit} className="relative h-9 w-full">
      <svg
        viewBox={`0 0 ${WIDTH} ${BAND}`}
        preserveAspectRatio="none"
        height={BAND}
        className="absolute inset-x-0 top-[9px] block w-full overflow-hidden rounded-[var(--radius-pill)]"
        style={{ background: GROUND }}
      >
        {/* By where it starts, which holds still while its end follows the head that writes it. */}
        {band.written.map((s) => (
          <rect key={s.from} data-piece="written" width="1" height={BAND} fill={FILL} style={{ transform: placed(s), transition: glide }} />
        ))}
        <g data-mending={mending || undefined}>
          {mending && !still && (
            <animate attributeName="opacity" values="1;.45;1" keyTimes="0;.5;1" dur="1.6s" repeatCount="indefinite" />
          )}
          {band.lost.map((s) => (
            // The stroke keeps one damaged block among thousands wide enough to see.
            <rect
              key={s.from}
              data-piece="lost"
              x={s.from}
              width={s.to - s.from}
              height={BAND}
              fill={FAIL}
              stroke={FAIL}
              strokeWidth="2"
              vectorEffect="non-scaling-stroke"
            />
          ))}
        </g>
        {pieces > 0 && (
          <path
            data-piece="between"
            d={Array.from({ length: pieces - 1 }, (_, i) => `M${(((i + 1) * WIDTH) / pieces).toFixed(2)} 0V${BAND}`).join('')}
            stroke={SURFACE}
            strokeWidth="1"
            vectorEffect="non-scaling-stroke"
          />
        )}
      </svg>
      <svg viewBox={`0 0 ${WIDTH} ${TALL}`} preserveAspectRatio="none" className="absolute inset-0 block h-full w-full overflow-visible">
        {/* By place in the file, so a head keeps its element while it writes on and glides to where it is next. */}
        {band.heads.map((x, i) => (
          <g key={i} data-head style={{ transform: `translateX(${x.toFixed(2)}px)`, transition: glide }}>
            <path d={`M0 4V${TALL - 4}`} stroke={SURFACE} strokeWidth="6" strokeLinecap="round" vectorEffect="non-scaling-stroke" />
            <path d={`M0 4V${TALL - 4}`} stroke={INK} strokeWidth="3" strokeLinecap="round" vectorEffect="non-scaling-stroke" />
          </g>
        ))}
      </svg>
    </div>
  );
}
