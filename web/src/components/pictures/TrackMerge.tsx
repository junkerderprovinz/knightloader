import { RADIUS, Stop, Track, WIDTH } from './lane';
import { FILL, GROUND, LABEL, QUIET, SURFACE, trip, useStage } from './scene';
import { Answer, Hop, Traveller, litFrom, loopOf } from './station';

// The source stands on the left, each track runs along a line of its own, and
// the lines meet in the file on the right.
const SOURCE = 20;
const PICKED = 66;
const MEET = 250;
const FILE = 292;
const FIRST = 38;
const PITCH = 50;
// Seconds a track is picked out of the source before it goes on.
const PICK = 0.6;

export interface MediaTrack {
  id: 'video' | 'audio' | 'subtitle';
  name: string;
  /** What is taken of it, in the words of the setting. */
  value: string;
  /** Whether it ends up inside the file. */
  joins: boolean;
}

// The figures of the three tracks: a frame with the play mark, the bars of a
// level meter, and a frame with a line of text.
const FIGURES: Record<MediaTrack['id'], string> = {
  video: 'M-8 -6h16v12h-16zM-2.4 -3.3v6.6l5.6 -3.3z',
  audio: 'M-7.5 -2h3v4h-3zM-1.5 -7h3v14h-3zM4.5 -4h3v8h-3z',
  subtitle: 'M-8 -6h16v12h-16zM-5 1h10v2.4h-10z',
};

// The file: a sheet with its corner folded.
const SHEET = 'M-14 -19h19l9 9v29h-28z';

/**
 * TrackMerge shows what a video download is put together from with the
 * current settings: the source fans out into its tracks, the ones that are
 * taken run together, and one file with its extension and name comes out. A
 * track that stays out of the file is drawn switched off and ends where it
 * was picked.
 */
export function TrackMerge({
  tracks,
  extension,
  filename,
  label,
}: {
  tracks: MediaTrack[];
  extension: string;
  /** The output template as it is set. */
  filename: string;
  label: string;
}) {
  const { stage, still } = useStage(tracks.map((tr) => `${tr.id}:${tr.joins}`).join('\n'));
  const lane = (i: number) => FIRST + i * PITCH;
  const middle = lane((tracks.length - 1) / 2);
  const foot = lane(tracks.length - 1) + 16;

  // Every parcel moves at the one speed, so a track on an outer line arrives
  // later, and each walk is padded to the longest so they loop together.
  const route = (i: number, tail = 0) =>
    trip([[SOURCE, middle], [PICKED, lane(i)], [MEET, lane(i)], [FILE, middle]], (stop) => (stop === 1 ? PICK : 0), 0.5, tail);
  const joining = tracks.flatMap((tr, i) => (tr.joins ? [i] : []));
  const whole = Math.max(0, ...joining.map((i) => route(i).dur)) + 1.8;
  const walks = tracks.map((tr, i) => route(i, tr.joins ? whole - route(i).dur : 0));
  const loop = loopOf(walks[joining[0] ?? 0]);
  const complete = Math.max(0, ...joining.map((i) => walks[i].stops[3].arrive));
  const moving = !still && joining.length > 0;

  const said = [...tracks.map((tr) => `${tr.name} ${tr.value}`), extension, filename].join(', ');
  const ext = (paint: string) => (
    <text y="9" textAnchor="middle" fontSize="8" fontWeight="700" fill={paint} className="glim-num uppercase">
      {extension}
    </text>
  );

  return (
    <svg
      ref={stage}
      viewBox={`0 0 ${WIDTH} ${foot + 24}`}
      role="img"
      aria-label={`${label}: ${said}`}
      data-new="track-merge"
      className="mx-auto block w-full max-w-[24rem] overflow-visible"
    >
      {tracks.map((tr, i) => (
        <Track key={tr.id} d={`M${SOURCE} ${middle}L${PICKED} ${lane(i)}${tr.joins ? `H${MEET}L${FILE} ${middle}` : ''}`} />
      ))}

      <circle cx={SOURCE} cy={middle} r={RADIUS} fill={GROUND} />
      <path d={`M${SOURCE - 3} ${middle - 5.5}v11l9 -5.5z`} fill={QUIET} />

      {tracks.map((tr, i) => {
        const figure = (paint: string) => <path d={FIGURES[tr.id]} fill={paint} fillRule="evenodd" transform="scale(.8)" />;
        return (
          <g key={tr.id} data-track={tr.id} data-state={tr.joins ? 'on' : 'off'}>
            <Stop
              x={PICKED}
              y={lane(i)}
              state={tr.joins ? 'takes' : 'off'}
              face={figure}
              at={walks[i].stops[1].arrive}
              loop={loop}
              still={still}
            />
            {/* Over the line, which the parcel travels along. */}
            <foreignObject x={PICKED + RADIUS + 6} y={lane(i) - 36} width={MEET - PICKED - RADIUS - 12} height="30">
              <span className="block truncate text-meta leading-[15px] text-carbon-textMuted">{tr.name}</span>
              {/* dir="auto": a list of language codes, or a selector somebody typed. */}
              <span
                dir="auto"
                className={`block truncate text-meta leading-[15px] ${tr.joins ? 'text-carbon-text' : 'text-carbon-textMuted'}`}
              >
                {tr.value}
              </span>
            </foreignObject>
          </g>
        );
      })}

      <g transform={`translate(${FILE} ${middle})`} data-file={extension}>
        {moving && <Answer radius={20} at={complete} loop={loop} />}
        <g>
          {moving && <Hop at={complete} loop={loop} />}
          <path d={SHEET} fill={GROUND} />
          {ext(LABEL)}
          {joining.length > 0 && (
            <g opacity={moving ? 0 : undefined}>
              {moving && <animate attributeName="opacity" {...litFrom(complete, loop)} {...loop.smil} />}
              <path d={SHEET} fill={FILL} />
              <path d="M5 -19v9h9" fill="none" stroke={SURFACE} strokeOpacity=".6" />
              {ext(SURFACE)}
            </g>
          )}
        </g>
      </g>
      <foreignObject x="8" y={foot} width={WIDTH - 16} height="18">
        <span dir="ltr" data-filename className="glim-num block truncate text-end text-meta leading-[18px] text-carbon-text">
          {filename}
        </span>
      </foreignObject>

      {moving &&
        joining.map((i) => (
          // It is taken in where it is picked and in the file, and runs past the point where the lines meet.
          <Traveller
            key={tracks[i].id}
            trip={walks[i]}
            loop={loop}
            taking={[walks[i].stops[0], walks[i].stops[1], walks[i].stops[3]]}
            stays
          >
            <circle r="7" fill={FILL} />
            <path d={FIGURES[tracks[i].id]} fill={SURFACE} fillRule="evenodd" transform="scale(.55)" />
          </Traveller>
        ))}
    </svg>
  );
}
