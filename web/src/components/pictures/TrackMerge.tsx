import type { ReactNode } from 'react';
import { RADIUS, Stop, WIDTH } from './lane';
import { FILL, GROUND, HUE, LABEL, SURFACE, pictureHue, trip, useRoom, useStage } from './scene';
import { Answer, Hop, Traveller, litFrom, loopOf } from './station';

// The source stands on the left, each track runs along a line of its own, and
// the lines meet in the file on the right. The picture is laid out at its
// card's width, one unit to the pixel, and never narrower than the lane's.
const SOURCE = 20;
const PICKED = 66;
// From where the lines meet to the file, and from the file to the right edge.
const JOIN = 42;
const EDGE = 28;
// How much further in the file stands when a sidecar stands beside it.
const SIDECAR = 32;
const FIRST = 38;
const PITCH = 50;
// Seconds a track is picked out of the source before it goes on.
const PICK = 0.6;
// The height of a line of words under the file.
const LINE = 16;

export interface MediaTrack {
  id: 'video' | 'audio' | 'subtitle' | 'thumbnail' | 'metadata' | 'chapters';
  name: string;
  /** What is taken of it, in the words of the setting. */
  value: string;
}

// The figures of the tracks: a frame with the play mark, the bars of a level
// meter, a frame with a line of text, a picture with hills, a tag, and a
// timeline cut into chapters.
const FIGURES: Record<MediaTrack['id'], string> = {
  video: 'M-8 -6h16v12h-16zM-2.4 -3.3v6.6l5.6 -3.3z',
  audio: 'M-7.5 -2h3v4h-3zM-1.5 -7h3v14h-3zM4.5 -4h3v8h-3z',
  subtitle: 'M-8 -6h16v12h-16zM-5 1h10v2.4h-10z',
  thumbnail: 'M-8 -6h16v12h-16zM-6 4l4 -5 3 3.5 2 -2 3 3.5z',
  metadata: 'M-8 -7h7l8 8 -6.5 6.5 -8 -8zM-5.2 -3.6a1.6 1.6 0 1 0 3.2 0a1.6 1.6 0 1 0 -3.2 0z',
  chapters: 'M-8 -2.5h4.5v5h-4.5zM-2.25 -2.5h4.5v5h-4.5zM3.5 -2.5h4.5v5h-4.5z',
};

// The file: a sheet with its corner folded. A sidecar is a smaller one.
const SHEET = 'M-14 -19h19l9 9v29h-28z';
const SMALL_SHEET = 'M-9 -12h12l6 6v18h-18z';

// The tracks that are files of their own when nothing is merged. Tags and
// chapters still go into the files, and are drawn into the video's.
const OWN_FILE: ReadonlySet<MediaTrack['id']> = new Set(['video', 'audio', 'subtitle', 'thumbnail']);

/**
 * TrackMerge shows what a video download is put together from with the
 * current settings: the source fans out into the tracks that go into the
 * file, they run together, and one file with its extension and name comes
 * out. `split` says that each chapter becomes a file of its own as well, and
 * `sidecar` is the extension of a file written beside the download. `apart`
 * says that nothing is merged: each track that is a file ends in a file of
 * its own on its line.
 */
export function TrackMerge({
  tracks,
  extension,
  filename,
  label,
  split,
  sidecar,
  apart,
}: {
  tracks: MediaTrack[];
  extension: string;
  /** The output template as it is set. */
  filename: string;
  label: string;
  /** The words for the further files, one per chapter, or none. */
  split?: string;
  sidecar?: string;
  /** The words for the tracks kept apart, each in a file of its own, or none for one merged file. */
  apart?: string;
}) {
  const { box, room } = useRoom<HTMLDivElement>();
  const width = Math.max(WIDTH, room);
  const file = width - EDGE - (sidecar ? SIDECAR : 0);
  const meet = file - JOIN;
  const { stage, still } = useStage([...tracks.map((tr) => tr.id), split, sidecar, apart].join('\n'));
  const lane = (i: number) => FIRST + i * PITCH;
  const middle = lane((tracks.length - 1) / 2);
  const merged = apart === undefined;
  // A file on the last line reaches further down than the lines do.
  const foot = lane(tracks.length - 1) + (merged ? 16 : 24);
  // Where each track ends: the one file, or kept apart, a file of its own or the video's.
  const video = Math.max(0, tracks.findIndex((tr) => tr.id === 'video'));
  const ownFile = (i: number) => !merged && OWN_FILE.has(tracks[i].id);
  const end = (i: number) => (merged ? middle : lane(ownFile(i) ? i : video));
  const main = merged ? middle : lane(video);

  // Every parcel moves at the one speed, so a track on an outer line arrives
  // later, and each walk is padded to the longest so they loop together.
  const route = (i: number, tail = 0) =>
    trip([[SOURCE, middle], [PICKED, lane(i)], [meet, lane(i)], [file, end(i)]], (stop) => (stop === 1 ? PICK : 0), 0.5, tail);
  const whole = Math.max(...tracks.map((_, i) => route(i).dur)) + 1.8;
  const walks = tracks.map((_, i) => route(i, whole - route(i).dur));
  const loop = loopOf(walks[0]);
  const complete = Math.max(...walks.map((w) => w.stops[3].arrive));

  const said = [...tracks.map((tr) => `${tr.name} ${tr.value}`), apart ?? extension, sidecar, filename, split]
    .filter(Boolean)
    .join(', ');
  const ext = (words: string, paint: string, y = 9) => (
    <text y={y} textAnchor="middle" fontSize="8" fontWeight="700" fill={paint} className="glim-num uppercase">
      {words}
    </text>
  );
  // A file kept apart shows the figure of its track where the merged one shows its extension.
  const kind = (id: MediaTrack['id'], paint: string) => (
    <path d={FIGURES[id]} fill={paint} fillRule="evenodd" transform="translate(0 2) scale(.8)" />
  );
  // The file and its sidecar wait in grey and take the accent once every track is in.
  const lit = (children: ReactNode) => (
    <g opacity={still ? undefined : 0}>
      {!still && <animate attributeName="opacity" {...litFrom(complete, loop)} {...loop.smil} />}
      {children}
    </g>
  );

  return (
    <div ref={box} data-new="track-merge">
      <svg
        ref={stage}
        viewBox={`0 0 ${width} ${foot + 24 + (split ? LINE : 0)}`}
        role="img"
        aria-label={`${label}: ${said}`}
        className="block w-full overflow-visible"
      >
        {tracks.map((tr, i) => {
          const figure = (paint: string) => <path d={FIGURES[tr.id]} fill={paint} fillRule="evenodd" transform="scale(.8)" />;
          return (
            <g key={tr.id} data-track={tr.id} {...pictureHue(i + 1)}>
              <path
                d={`M${SOURCE} ${middle}L${PICKED} ${lane(i)}H${meet}L${file} ${end(i)}`}
                fill="none"
                stroke={HUE}
                strokeOpacity=".6"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
              <Stop x={PICKED} y={lane(i)} state="takes" face={figure} at={walks[i].stops[1].arrive} loop={loop} still={still} />
              {/* Over the line, which the parcel travels along. */}
              <foreignObject x={PICKED + RADIUS + 6} y={lane(i) - 36} width={meet - PICKED - RADIUS - 12} height="30">
                <span className="block truncate text-meta leading-[15px] text-carbon-textMuted">{tr.name}</span>
                {/* dir="auto": a list of language codes, or a selector somebody typed. */}
                <span dir="auto" className="block truncate text-meta leading-[15px] text-carbon-text">
                  {tr.value}
                </span>
              </foreignObject>
            </g>
          );
        })}

        <circle cx={SOURCE} cy={middle} r={RADIUS} fill={GROUND} />
        <path d={`M${SOURCE - 3} ${middle - 5.5}v11l9 -5.5z`} fill={LABEL} />

        <g transform={`translate(${file} ${main})`} data-file={merged ? extension : 'video'} {...pictureHue(0)}>
          {!still && <Answer radius={20} at={complete} loop={loop} />}
          <g>
            {!still && <Hop at={complete} loop={loop} />}
            {/* One more sheet stacked behind for the files the chapters become. */}
            {split && <path data-split d={SHEET} transform="translate(7 -7)" fill={GROUND} stroke={SURFACE} strokeWidth="1.5" />}
            <path d={SHEET} fill={GROUND} />
            {merged ? ext(extension, LABEL) : kind('video', LABEL)}
            {lit(
              <>
                <path d={SHEET} fill={FILL} />
                <path d="M5 -19v9h9" fill="none" stroke={SURFACE} strokeOpacity=".6" />
                {merged ? ext(extension, SURFACE) : kind('video', SURFACE)}
              </>,
            )}
          </g>
          {sidecar && (
            <g transform={`translate(${SIDECAR} 8)`} data-sidecar={sidecar}>
              <path d={SMALL_SHEET} fill={GROUND} />
              {ext(sidecar, LABEL, 6)}
              {lit(
                <>
                  <path d={SMALL_SHEET} fill={FILL} />
                  {ext(sidecar, SURFACE, 6)}
                </>,
              )}
            </g>
          )}
        </g>
        {tracks.map(
          (tr, i) =>
            ownFile(i) &&
            i !== video && (
              <g key={tr.id} transform={`translate(${file} ${lane(i)})`} data-apart={tr.id} {...pictureHue(i + 1)}>
                <path d={SHEET} fill={GROUND} />
                {kind(tr.id, LABEL)}
                {lit(
                  <>
                    <path d={SHEET} fill={FILL} />
                    <path d="M5 -19v9h9" fill="none" stroke={SURFACE} strokeOpacity=".6" />
                    {kind(tr.id, SURFACE)}
                  </>,
                )}
              </g>
            ),
        )}
        <foreignObject x="8" y={foot} width={width - 16} height={18 + (split ? LINE : 0)}>
          <span dir="ltr" data-filename className="glim-num block truncate text-end text-meta leading-[18px] text-carbon-text">
            {filename}
          </span>
          {split && <span className="block truncate text-end text-meta leading-[16px] text-carbon-textMuted">{split}</span>}
        </foreignObject>

        {!still &&
          tracks.map((tr, i) => (
            // It is taken in where it is picked and in the file, and runs past the point where the lines meet.
            <g key={tr.id} {...pictureHue(i + 1)}>
              <Traveller trip={walks[i]} loop={loop} taking={[walks[i].stops[0], walks[i].stops[1], walks[i].stops[3]]} stays>
                <circle r="7" fill={FILL} />
                <path d={FIGURES[tr.id]} fill={SURFACE} fillRule="evenodd" transform="scale(.55)" />
              </Traveller>
            </g>
          ))}
      </svg>
    </div>
  );
}
