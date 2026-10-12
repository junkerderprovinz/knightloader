import { useLayoutEffect, useRef, useState, type ComponentType, type SVGProps } from 'react';
import { useT } from '../../lib/i18n';
import { IconCollector } from '../../lib/icons';
import { FILL, GROUND, HUE, QUIET, SURFACE, pictureHue, useStage } from './scene';
import { HubLabel, SpokeLabel, Stream, lineOf, spokeLayout, type SpokeShape } from './spokes';

/** One way a link reaches the collector. */
export interface Way {
  id: string;
  name: string;
  glyph: ComponentType<SVGProps<SVGSVGElement>>;
  /** Open for links, switched off, or with nothing set up for it yet. */
  state: 'on' | 'off' | 'unset';
  /** What an open way reads from: a folder, a device, a count. */
  detail?: string;
}

const SIZES = { pitch: 46, hub: 30, node: 15 };

/**
 * WaysIn draws every way a link reaches the collector as a line into it. A way
 * that is open carries packets, one that is off or not set up stands grey, and
 * a click on a way goes to where it is set up.
 */
export function WaysIn({
  ways,
  label,
  onPick,
}: {
  ways: Way[];
  /** What the picture shows, read out before the ways. */
  label: string;
  onPick: (id: string) => void;
}) {
  const { t } = useT();
  const idle = { off: t('settings.modules.off'), unset: t('settings.selftest.status.skipped') };
  const drawn = ways.map((way) => ({ way, sub: way.state === 'on' ? way.detail : idle[way.state] }));
  const alt = `${label}: ${drawn.map((d) => (d.way.state === 'on' ? d.way.name : `${d.way.name} (${d.sub})`)).join(', ')}`;
  // The wide picture is laid out at the card's own width, one unit to the
  // pixel, so it fills the card without its names growing with it.
  const box = useRef<HTMLDivElement>(null);
  const [room, setRoom] = useState(0);
  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    const measure = () => setRoom(el.clientWidth);
    measure();
    const watch = new ResizeObserver(measure);
    watch.observe(el);
    return () => watch.disconnect();
  }, []);
  return (
    <div ref={box} data-new="ways-in" className="kl-pic @container">
      {(['wide', 'narrow'] as const).map((shape) => (
        <Net key={shape} shape={shape} drawn={drawn} alt={alt} room={room} onPick={onPick} />
      ))}
    </div>
  );
}

function Net({
  shape,
  drawn,
  alt,
  room,
  onPick,
}: {
  shape: SpokeShape;
  drawn: { way: Way; sub?: string }[];
  alt: string;
  room: number;
  onPick: (id: string) => void;
}) {
  const { t } = useT();
  const { stage, still } = useStage(drawn.map((d) => `${d.way.id}:${d.way.state}`).join(' '));
  const { width, height, hub, hubLabel, spokes } = spokeLayout(shape, drawn.length, SIZES, room);
  return (
    <svg
      ref={stage}
      viewBox={`0 0 ${width} ${height}`}
      role="group"
      aria-label={alt}
      className={`mx-auto w-full overflow-visible ${
        shape === 'wide' ? 'hidden @[42rem]:block' : 'block max-w-[26rem] @[42rem]:hidden'
      }`}
    >
      {drawn.map(({ way, sub }, i) => {
        const spoke = spokes[i];
        const on = way.state === 'on';
        const Glyph = way.glyph;
        return (
          <g key={way.id} data-way={way.id} data-state={way.state} {...pictureHue(i + 1)}>
            <path
              d={lineOf(spoke)}
              fill="none"
              stroke={on ? HUE : QUIET}
              strokeOpacity={on ? undefined : 0.6}
              strokeWidth="2"
              strokeLinecap="round"
              strokeDasharray={on ? undefined : '2 7'}
            />
            {/* A link comes in, so the packet walks the line towards the hub. */}
            {on && !still && <Stream from={spoke.to} to={spoke.from} lead={0.2 + (i % 4) * 0.35} />}
            <g
              className="kl-net-node"
              tabIndex={0}
              role="button"
              aria-label={sub ? `${way.name} (${sub})` : way.name}
              onClick={() => onPick(way.id)}
              onKeyDown={(e) => {
                if (e.key !== 'Enter' && e.key !== ' ') return;
                e.preventDefault();
                onPick(way.id);
              }}
            >
              <circle className="kl-net-ring" cx={spoke.x} cy={spoke.y} r={SIZES.node + 5} />
              <circle cx={spoke.x} cy={spoke.y} r={SIZES.node} fill={GROUND} />
              <g color={on ? HUE : QUIET} opacity={on ? undefined : 0.7}>
                <Glyph x={spoke.x - 8} y={spoke.y - 8} width={16} height={16} />
              </g>
            </g>
            <SpokeLabel at={spoke.label} name={way.name} sub={sub} faint={!on} />
          </g>
        );
      })}
      <g {...pictureHue(0)}>
        <circle cx={hub.x} cy={hub.y} r={SIZES.hub} fill={FILL} />
        <g color={SURFACE}>
          <IconCollector x={hub.x - 13} y={hub.y - 13} width={26} height={26} />
        </g>
      </g>
      <HubLabel at={hubLabel}>{t('nav.collector')}</HubLabel>
    </svg>
  );
}
