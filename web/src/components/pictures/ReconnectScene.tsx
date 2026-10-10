import type { ReactNode } from 'react';
import logoUrl from '../../assets/logo.svg';
import { useT } from '../../lib/i18n';
import { IconGlobe } from '../../lib/icons';
import { FILL, GROUND, HUE, LABEL, QUIET, SURFACE, TRAVEL, keyed, trip, useStage } from './scene';
import { Answer, Hop, Traveller, litFrom, loopOf } from './station';

// The line of the picture: this app on the left, the router in the middle,
// the internet on the right.
const Y = 62;
const APP = 40;
const ROUTER = 180;
const NET = 318;
const LOOP = 7.6;

/** A walk between two figures that sets off `leaves` seconds into the loop and fills the rest of it. */
function leg(from: number, to: number, leaves: number) {
  return trip([[from, Y], [to, Y]], () => 0, leaves, LOOP - leaves - Math.abs(to - from) / TRAVEL);
}

/**
 * ReconnectScene plays one reconnect in the chosen method: the app asks the
 * router, the router lets go of its address, and a new one comes back. `from`
 * and `to` are the addresses of the last run, and `off` stands the picture grey.
 */
export function ReconnectScene({
  method,
  glyph,
  off,
  from,
  to,
}: {
  /** Names the take, so another method starts the scene over. */
  method: string;
  /** The chosen method's glyph at 16px, which the request carries to the router. */
  glyph: ReactNode;
  off: boolean;
  from?: string;
  to?: string;
}) {
  const { t } = useT();
  const { stage, still: gated } = useStage(method);
  const still = gated || off;

  const ask = leg(APP + 24, ROUTER - 36, 0.6);
  const answer = leg(NET - 26, ROUTER + 36, 3.6);
  const loop = loopOf(ask);
  const asked = ask.stops[1].arrive;
  const dropped = asked + 0.4 / LOOP;
  const back = answer.stops[1].arrive;

  // The line and the router's lights are on, go dark when the address is let go and come back with the new one.
  const online = keyed([[0, 1], [dropped - 0.01, 1], [dropped, 0], [back - 0.01, 0], [back, 1], [1, 1]]);

  return (
    <svg
      ref={stage}
      viewBox="0 0 360 112"
      role="img"
      aria-label={off ? t('settings.reconnect.offState') : t('picture.reconnect.alt')}
      data-state={off ? 'off' : 'on'}
      className="mx-auto block w-full max-w-[30rem] overflow-visible"
    >
      <path d={`M${APP + 24} ${Y}H${ROUTER - 36}`} stroke={QUIET} strokeOpacity=".6" strokeWidth="2" strokeLinecap="round" strokeDasharray="2 7" />
      <path d={`M${ROUTER + 36} ${Y}H${NET - 26}`} stroke={QUIET} strokeOpacity=".6" strokeWidth="2" strokeLinecap="round" strokeDasharray="2 7" />
      {!off && (
        <path d={`M${ROUTER + 36} ${Y}H${NET - 26}`} stroke={HUE} strokeWidth="2.5" strokeLinecap="round">
          {!still && <animate attributeName="opacity" {...online} {...loop.smil} />}
        </path>
      )}

      {/* The app's own mark stands for the app. */}
      <image href={logoUrl} x={APP - 17} y={Y - 27} width="34" height="54" opacity={off ? 0.45 : undefined} />

      <g transform={`translate(${ROUTER} ${Y})`}>
        {!still && <Answer radius={30} at={asked} loop={loop} />}
        {!still && <Answer radius={30} at={back} loop={loop} />}
        <g>
          {!still && <Hop at={asked} loop={loop} />}
          <path d="M-20 -18v-14M20 -18v-14" stroke={QUIET} strokeWidth="2.5" strokeLinecap="round" />
          <rect x="-32" y="-18" width="64" height="36" rx="10" fill={GROUND} />
          {[-16, -6, 4].map((x) => (
            <circle key={x} cx={x} cy="0" r="2.6" fill={QUIET} />
          ))}
          {!off && (
            <g>
              {!still && <animate attributeName="opacity" {...online} {...loop.smil} />}
              {[-16, -6, 4].map((x) => (
                <circle key={x} cx={x} cy="0" r="2.6" fill={HUE} />
              ))}
            </g>
          )}
        </g>
      </g>
      <Caption x={ROUTER}>{t('picture.reconnect.router')}</Caption>

      <circle cx={NET} cy={Y} r="24" fill={GROUND} />
      <g color={LABEL}>
        <IconGlobe x={NET - 15} y={Y - 15} width={30} height={30} />
      </g>
      <Caption x={NET}>{t('picture.reconnect.internet')}</Caption>

      {!off && (
        <>
          {/* The old address rises off the line and is gone; the still frame shows the new one alone. */}
          {!still && (
            <g>
              <animate attributeName="opacity" {...keyed([[0, 0], [0.02, 1], [dropped, 1], [dropped + 1 / LOOP, 0], [1, 0]])} {...loop.smil} />
              <animateTransform
                attributeName="transform"
                type="translate"
                {...keyed([[0, '0 0'], [dropped, '0 0'], [dropped + 1 / LOOP, '0 -12'], [1, '0 -12']])}
                {...loop.smil}
              />
              <Address>{from ?? t('picture.reconnect.oldAddress')}</Address>
            </g>
          )}
          <g opacity={still ? undefined : 0}>
            {!still && <animate attributeName="opacity" {...litFrom(back, loop)} {...loop.smil} />}
            <Address fresh>{to ?? t('picture.reconnect.newAddress')}</Address>
          </g>
        </>
      )}

      {!still && (
        <>
          <Traveller trip={ask} loop={loop} taking={ask.stops} stays>
            <circle r="10" fill={FILL} />
            <g color={SURFACE} transform="translate(-7 -7) scale(.875)">
              {glyph}
            </g>
          </Traveller>
          <Traveller trip={answer} loop={loop} taking={answer.stops} stays>
            <rect x="-9" y="-5" width="18" height="10" rx="5" fill={FILL} />
          </Traveller>
        </>
      )}
    </svg>
  );
}

function Caption({ x, children }: { x: number; children: string }) {
  return (
    <foreignObject x={x - 50} y={Y + 30} width="100" height="18">
      <div className="truncate text-center text-[11px] leading-[18px] text-carbon-textSub">{children}</div>
    </foreignObject>
  );
}

// The address the router holds, over the line it holds it on. Outlined, so its
// words read in every accent.
function Address({ fresh = false, children }: { fresh?: boolean; children: string }) {
  return (
    <foreignObject x={ROUTER + 28} y={Y - 44} width={NET - ROUTER - 30} height="24">
      <div className="flex h-full items-center justify-center">
        <span
          dir="auto"
          className={`glim-num max-w-full truncate rounded-[var(--radius-pill)] border px-2 text-[11px] leading-[18px] ${
            fresh ? 'border-accentInk text-carbon-text' : 'border-carbon-textMuted text-carbon-textMuted'
          }`}
        >
          {children}
        </span>
      </div>
    </foreignObject>
  );
}
