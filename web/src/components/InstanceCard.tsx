import { useEffect, useState } from 'react';
import logoUrl from '../assets/logo.svg';
import { ApiError, fetchTasks, type Task } from '../lib/api';
import { fmtSpeed } from '../lib/format';
import { useT } from '../lib/i18n';
import { Card, Button, IconBadge, LabelBadge, useTooltip } from './ui';
import { IconTrash } from '../lib/icons';

interface Stats {
  online: boolean;
  /**
   * The peer answered with 401 or 403, typically because changing its password
   * revoked the pairing token. The fix is re-pairing, not checking cables.
   */
  refused: boolean;
  active: number;
  total: number;
  speed: number;
}

// usePeerStats polls one instance for its live figures.
function usePeerStats(base: string): Stats | null {
  const [stats, setStats] = useState<Stats | null>(null);
  useEffect(() => {
    let alive = true;
    const load = async () => {
      try {
        const list: Task[] = await fetchTasks(base);
        if (!alive) return;
        const running = list.filter((x) => x.status === 'running' || x.status === 'extracting').length;
        const speed = list.reduce((s, x) => s + (x.status === 'running' ? x.speed : 0), 0);
        setStats({ online: true, refused: false, active: running, total: list.length, speed });
      } catch (e) {
        const refused = e instanceof ApiError && (e.status === 401 || e.status === 403);
        if (alive) setStats({ online: false, refused, active: 0, total: 0, speed: 0 });
      }
    };
    load();
    const iv = setInterval(load, 3000);
    return () => {
      alive = false;
      clearInterval(iv);
    };
  }, [base]);
  return stats;
}

// InstanceRow is the quiet form used where instances are a summary rather than
// the subject of the page: a dot, the name, and the current speed.
export function InstanceRow({ name, base, onOpen }: { name: string; base: string; onOpen?: () => void }) {
  const { t } = useT();
  const stats = usePeerStats(base);
  const online = stats?.online ?? false;
  const refused = stats?.refused ?? false;
  const state = online ? t('instances.online') : refused ? t('instances.refused') : t('instances.offline');

  // The dot shows state by colour alone, so it gets a tooltip. Only the hover
  // props are spread: the row can be a <button>, which must not contain a
  // tabindex, and aria-label already names the state.
  const tip = useTooltip<HTMLSpanElement>(state);
  const { ref: tipRef, onMouseEnter, onMouseLeave, 'aria-describedby': tipDescribedBy } = tip.triggerProps;

  const body = (
    <>
      <span
        ref={tipRef}
        onMouseEnter={onMouseEnter}
        onMouseLeave={onMouseLeave}
        aria-describedby={tipDescribedBy}
        role="img"
        aria-label={state}
        className={`h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${online ? 'bg-statusOkSolid' : 'bg-statusFailSolid'}`}
      />
      <span className="min-w-0 flex-1 truncate text-[14px] text-carbon-text">{name}</span>
      <span className="glim-num text-xs text-carbon-textSub">
        {stats ? fmtSpeed(stats.speed) || '-' : '-'}
      </span>
    </>
  );
  return (
    <>
      {onOpen ? (
        <button
          onClick={onOpen}
          className="flex w-full items-center gap-3 px-6 py-4 text-left transition-colors hover:bg-carbon-hover/50"
        >
          {body}
        </button>
      ) : (
        <div className="flex items-center gap-3 px-6 py-4">{body}</div>
      )}
      {tip.node}
    </>
  );
}

// InstanceCard shows one instance: its state, its address and three live figures.
export function InstanceCard({
  name,
  url,
  relayId,
  base,
  onOpen,
  onRemove,
  hue,
  isSelf = false,
}: {
  /** A peer's displayName or name, never the raw relay address. */
  name: string;
  url: string;
  /** Set for a peer reached through the relay, whose url is empty. */
  relayId?: string;
  base: string;
  onOpen?: () => void;
  onRemove?: () => void;
  /** The card's palette position; .glim-hue colours the whole subtree. */
  hue?: number;
  /** Marks the card of the instance being viewed. */
  isSelf?: boolean;
}) {
  const { t } = useT();
  const stats = usePeerStats(base);
  const online = stats?.online ?? false;
  const refused = stats?.refused ?? false;
  // "Refused" takes a rainbow hue, since it is neither ok nor failed.
  const state = online ? t('instances.online') : refused ? t('instances.refused') : t('instances.offline');

  return (
    // padding="none" so the logo runs flush to the left edge at full height,
    // clipped by overflow-hidden. No `hover` lift: the card itself is not
    // clickable (check-card-hover.mjs guards this).
    <Card padding="none" hue={hue} className="group relative flex h-full flex-col overflow-hidden">
      {/* The two columns form one row and the Open button a second, so the
          button can never overlap the text. */}
      <div className="flex min-h-0 flex-1 items-stretch">
      {/* h-28 matches the sidebar's brand mark (check-mark-scale.mjs); a larger
          mark squeezes the metric labels together. max-h-full keeps the text
          column in charge of the card's height. */}
      <div className="flex shrink-0 items-center self-stretch pl-4">
        <img src={logoUrl} alt="" aria-hidden className="h-28 max-h-full w-auto" />
      </div>

      {/* Absolutely placed, with room reserved in the name row. */}
      <span className="absolute right-5 top-5 z-10">
        <LabelBadge label={state} tone={online ? 'ok' : refused ? undefined : 'fail'} hue={refused ? 3 : undefined} />
      </span>

      <div className="flex min-w-0 flex-1 flex-col gap-4 p-7 pr-36">
        {/* Name and address as one block. */}
        <div className="flex flex-col gap-0.5">
        <div className="flex items-center gap-2.5">
          <span className="truncate font-semibold text-carbon-text">{name}</span>
          {isSelf && <span className="glim-eyebrow shrink-0">{t('instances.thisInstance')}</span>}
          <span className="flex-1" />
          {onRemove && (
            <span className="opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
              {/* A lone glyph fills half its square badge (GlimStone rule 13). */}
              <IconBadge
                labelled
                hue={hue}
                icon={<IconTrash width={16} height={16} />}
                title={t('instances.removeTitle', { name })}
                aria-label={t('instances.removeTitle', { name })}
                onClick={onRemove}
              />
            </span>
          )}
        </div>

        <div className="truncate text-xs text-carbon-textMuted">{relayId ? t('instances.viaRelay') : url}</div>
        </div>

        <div className="flex items-baseline gap-7">
          <Metric value={stats?.active ?? '-'} label={t('instances.metricActive')} />
          <Metric value={stats?.total ?? '-'} label={t('instances.metricTasks')} />
          <Metric value={stats ? fmtSpeed(stats.speed) || '0' : '-'} label={t('instances.metricSpeed')} />
        </div>

      </div>

      </div>

      {/* Full width; the margin sits on the button because the card has none. */}
      {onOpen && (
        <Button kind="secondary" onClick={onOpen} className="mx-5 mb-5 justify-center">
          {t('instances.open')}
        </Button>
      )}
    </Card>
  );
}

function Metric({ value, label }: { value: React.ReactNode; label: string }) {
  return (
    <div className="min-w-0">
      <div className="glim-num text-sm font-semibold text-carbon-text">{value}</div>
      <div className="glim-eyebrow">{label}</div>
    </div>
  );
}
