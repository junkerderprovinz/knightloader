import { useEffect, useState } from 'react';
import logoUrl from '../assets/logo.svg';
import { ApiError, fetchTasks, type Task } from '../lib/api';
import { fmtSpeed } from '../lib/format';
import { useT } from '../lib/i18n';
import { Card, Button, IconBadge, LabelBadge } from './ui';
import { IconTrash } from '../lib/icons';

interface Stats {
  online: boolean;
  /**
   * Reached, and it said no. Distinct from offline because the fix is
   * different: a peer refuses once the credential pairing gave it stops being
   * valid, which happens on its own the moment that peer sets or changes its
   * password - every token it issued is revoked with it. Shown as plain
   * offline, that reads as a machine somebody unplugged.
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
        // "It refused us" is not "it is switched off", and the two need
        // opposite reactions. A peer stops accepting this instance whenever the
        // credential pairing handed over stops being valid - most easily by
        // that peer setting or changing its password, which revokes every token
        // it ever issued. Reported as plain offline, that looks like a machine
        // somebody unplugged, and the pairing that would fix it is the last
        // thing anyone would try.
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
  const body = (
    <>
      <span
        role="img"
        aria-label={state}
        title={state}
        className={`h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${online ? 'bg-statusOkSolid' : 'bg-statusFailSolid'}`}
      />
      <span className="min-w-0 flex-1 truncate text-[13.5px] text-carbon-text">{name}</span>
      <span className="glim-num text-xs text-carbon-textSub">
        {stats ? fmtSpeed(stats.speed) || '—' : '—'}
      </span>
    </>
  );
  return onOpen ? (
    <button
      onClick={onOpen}
      className="flex w-full items-center gap-3 px-5 py-3 text-left transition-colors hover:bg-carbon-hover/50"
    >
      {body}
    </button>
  ) : (
    <div className="flex items-center gap-3 px-5 py-3">{body}</div>
  );
}

// One instance at a glance: a status dot, the host, and three quiet figures.
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
  /** The label shown - callers pass a peer's displayName (falling back to
   *  its name) here, never the raw relay address. */
  name: string;
  url: string;
  /** Set for a peer reached through the relay: url is empty for one of
   *  these (federation.Instance's own doc comment on why), so the second
   *  line shows "connected via relay" instead of a blank line. */
  relayId?: string;
  base: string;
  onOpen?: () => void;
  onRemove?: () => void;
  /** This card's position among the OTHER linked instances on Instances.tsx
   *  - "this instance"'s own card carries none, since it has no remove
   *  badge to colour and is not one of that set to begin with. */
  hue?: number;
  /** Marks the card for the instance you are looking at right now. It gets
   *  the same Open button as any peer (pointing at the local download list),
   *  so the row does not have one card shaped differently from the rest, plus
   *  a quiet label saying which one it is. */
  isSelf?: boolean;
}) {
  const { t } = useT();
  const stats = usePeerStats(base);
  const online = stats?.online ?? false;
  const refused = stats?.refused ?? false;
  // Three states, one badge. "Refused" is deliberately neither green nor red:
  // the peer is there and answering, it just will not accept this instance -
  // a different thing from a machine that is gone, and one a person fixes by
  // re-entering the phrase rather than by checking cables. It takes a rainbow
  // hue instead of a status tone, which is exactly the "neither ok nor
  // failed" the two status colours cannot express.
  const state = online ? t('instances.online') : refused ? t('instances.refused') : t('instances.offline');

  return (
    // padding="none" and a horizontal split, so the mark can sit flush
    // against the card's own left edge and run its full height (jdp,
    // 2026-08-27: "soll jede Instanz in deren card das logo ganz links in
    // der card sein und die card in der höhe ausfüllen"). With Card's usual
    // p-5 the mark would float inside a 20px margin instead, which is the
    // one thing that was asked against. overflow-hidden clips it to the
    // card's own corner radius; without it a square-edged image pokes out of
    // a rounded card at both left corners.
    //
    // The right-hand column carries the padding the card gave up, and it is
    // the side that decides the card's height - the mark is `self-stretch`
    // and so takes whatever height it is handed rather than setting it,
    // which is what keeps a row of cards the same height whatever their
    // contents.
    <Card padding="none" hover={!!onOpen} className="group relative flex h-full items-stretch overflow-hidden">
      {/* The mark, larger again and then larger once more (jdp, 2026-08-27,
          twice: "Das logo in den instanzencard bitte größer") but still on the
          card's own surface with no plate behind it, which was the other half
          of that earlier correction.
          `max-h-full` rather than a bare height: the right-hand column decides
          how tall the card is, and a mark taller than that column would start
          setting the height itself - which is the one thing the split here
          exists to prevent. It grows to 7rem where the card allows it and
          stops at the card's own edge where it does not. */}
      <div className="flex shrink-0 items-center self-stretch pl-4">
        <img src={logoUrl} alt="" aria-hidden className="h-28 max-h-full w-auto" />
      </div>

      {/* The state as a badge in the corner, the same shape the connection
          card uses (jdp, 2026-08-27: "Der status-punkt soll wie in der
          Fernzugriffcard ein badge in der rechten oberen ecke sein"). A 8px
          dot beside the name said the same thing in a form that had to be
          hovered to be read at all. Absolutely placed so it cannot push the
          name around, and the name row reserves room for it. */}
      <span className="absolute right-4 top-4 z-10">
        <LabelBadge label={state} tone={online ? 'ok' : refused ? undefined : 'fail'} hue={refused ? 3 : undefined} />
      </span>

      <div className="flex min-w-0 flex-1 flex-col gap-3 p-5 pr-32">
        <div className="flex items-center gap-2.5">
          <span className="truncate font-semibold text-carbon-text">{name}</span>
          {/* Which card is the machine you are on. An eyebrow rather than a
              coloured pill: it is an orientation aid, not a status. */}
          {isSelf && <span className="glim-eyebrow shrink-0">{t('instances.thisInstance')}</span>}
          <span className="flex-1" />
          {onRemove && (
            <span className="opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
              <IconBadge
                kind="danger"
                hue={hue}
                icon={<IconTrash />}
                title={t('instances.removeTitle', { name })}
                aria-label={t('instances.removeTitle', { name })}
                onClick={onRemove}
              />
            </span>
          )}
        </div>

        <div className="truncate text-xs text-carbon-textMuted">{relayId ? t('instances.viaRelay') : url}</div>

        <div className="flex items-baseline gap-5">
          <Metric value={stats?.active ?? '—'} label={t('instances.metricActive')} />
          <Metric value={stats?.total ?? '—'} label={t('instances.metricTasks')} />
          <Metric value={stats ? fmtSpeed(stats.speed) || '0' : '—'} label={t('instances.metricSpeed')} />
        </div>

        {onOpen && (
          <Button kind="secondary" onClick={onOpen} className="mt-auto w-full justify-center">
            {t('instances.open')}
          </Button>
        )}
      </div>
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
