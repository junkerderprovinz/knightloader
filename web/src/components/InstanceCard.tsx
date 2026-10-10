import { useEffect, useState, type ReactNode } from 'react';
import logoUrl from '../assets/logo.svg';
import { ApiError, fetchTasks, type Task } from '../lib/api';
import { fmtDate, fmtSpeed } from '../lib/format';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button, LabelBadge, Modal } from './ui';
import { IconBrowser, IconClose, IconEye, IconPhone, IconTrash } from '../lib/icons';
import { StatePill } from './StatePill';

// What each kind of instance calls itself, keyed by buildinfo.Deployment and by
// what the two clients announce themselves as, "mobile" and "extension".
const KINDS: Record<string, TranslationKey> = {
  mobile: 'instances.kind.mobile',
  desktop: 'instances.kind.desktop',
  container: 'instances.kind.container',
  extension: 'instances.kind.extension',
};

/** The "i" carved out of a disc, on the Details button. */
function IconInfo() {
  return (
    <svg viewBox="0 0 20 20" width={22} height={22} fill="currentColor" className="shrink-0" aria-hidden>
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M17.25 10A7.25 7.25 0 1 0 2.75 10a7.25 7.25 0 0 0 14.5 0ZM10 5.4a1.1 1.1 0 1 1 0 2.2 1.1 1.1 0 0 1 0-2.2ZM9 9h2v5.6H9V9Z"
      />
    </svg>
  );
}

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
export function usePeerStats(base: string): Stats | null {
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
// the subject of the page: the name and the state badge the Instances page shows.
export function InstanceRow({ name, base, onOpen }: { name: string; base: string; onOpen?: () => void }) {
  const { t } = useT();
  const stats = usePeerStats(base);
  const online = stats?.online ?? false;
  const refused = stats?.refused ?? false;
  const state = online ? t('instances.connected') : refused ? t('instances.refused') : t('instances.notConnected');

  const body = (
    <>
      <span className="min-w-0 flex-1 truncate text-[14px] text-carbon-text">{name}</span>
      <LabelBadge
        label={state}
        tip={refused ? t('instances.refusedByPassword') : undefined}
        tone={online ? 'ok' : refused ? undefined : 'fail'}
        hue={refused ? 3 : undefined}
      />
    </>
  );
  return (
    <>
      {onOpen ? (
        <button
          onClick={onOpen}
          className="flex w-full items-center gap-2.5 px-4 py-4 text-start transition-colors hover:bg-carbon-hover/50"
        >
          {body}
        </button>
      ) : (
        <div className="flex items-center gap-2.5 px-4 py-4">{body}</div>
      )}
    </>
  );
}

/**
 * Tile is the shape every card of the instance grid shares: the mark at the
 * start edge, the name with its state, one line of facts, the figures in a row
 * and the buttons at the bottom. The cards of a row take one height, so the
 * buttons line up. Nothing lifts under the pointer, since the card is no
 * control (check-card-hover.mjs).
 */
function Tile({
  entry,
  hot,
  mark,
  name,
  eyebrow,
  state,
  facts,
  figures,
  actions,
}: {
  /** The key the card shares with its node in the net (`data-nk`). */
  entry: string;
  hot: boolean;
  mark: ReactNode;
  name: string;
  eyebrow?: string;
  state: ReactNode;
  facts: string[];
  figures?: ReactNode;
  actions?: ReactNode;
}) {
  const said = facts.filter(Boolean);
  return (
    <div
      data-nk={entry}
      data-new="instance-cards"
      className={`flex flex-col overflow-hidden rounded-[var(--radius-card)] bg-carbon-surface2 ${
        hot ? 'shadow-[inset_0_0_0_2px_var(--accent)]' : ''
      }`}
    >
      <div className="flex items-stretch">
        <div className="flex shrink-0 items-center ps-4">{mark}</div>
        <div className="flex min-w-0 flex-1 flex-col gap-4 p-7 max-sm:p-5">
          {/* The state moves under the name when both do not fit on one line. */}
          <div className="flex flex-wrap items-center gap-2.5">
            <span className="min-w-0 truncate font-semibold text-carbon-text">{name}</span>
            {eyebrow && <span className="glim-eyebrow shrink-0">{eyebrow}</span>}
            <span className="flex-1" />
            {state}
          </div>
          {said.length > 0 && (
            <div className="-mt-3 text-xs text-carbon-textMuted">
              {said.map((fact, i) => (
                <span key={i}>
                  {i > 0 && ' · '}
                  <span dir="auto">{fact}</span>
                </span>
              ))}
            </div>
          )}
          {/* A figure that does not fit moves to the next line rather than
              running into its neighbour's label. */}
          {figures && <div className="flex flex-wrap items-baseline gap-x-7 gap-y-3">{figures}</div>}
        </div>
      </div>
      {/* The first button takes the room the others leave. */}
      {actions && <div className="mx-5 mb-5 mt-auto flex flex-wrap gap-2 [&>button:first-child]:flex-1">{actions}</div>}
    </div>
  );
}

/** The KnightLoader mark at the card's start edge, the same on every card of an
 *  instance. */
function CardLogo() {
  // h-26 matches the sidebar's brand mark (check-mark-scale.mjs); a larger
  // mark squeezes the figures together.
  return <img src={logoUrl} alt="" aria-hidden className="h-26 max-h-full w-auto" />;
}

/** A phone or a browser extension is no KnightLoader instance, so its card
 *  wears its own glyph on a tile of the mark's height. */
function ClientMark({ deployment }: { deployment: string }) {
  const Glyph = deployment === 'extension' ? IconBrowser : IconPhone;
  return (
    <span className="grid h-22 w-16 place-items-center rounded-[var(--radius-control)] bg-carbon-surface3 text-accentInk">
      <Glyph width={34} height={34} />
    </span>
  );
}

function Figure({ value, label }: { value: ReactNode; label: string }) {
  return (
    <div className="shrink-0">
      <div className="glim-num text-sm font-semibold text-carbon-text">{value}</div>
      <div className="glim-eyebrow">{label}</div>
    </div>
  );
}

function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 border-t border-carbon-border py-2.5 first:border-t-0 first:pt-0">
      <dt className="shrink-0 text-sm text-carbon-textSub">{label}</dt>
      <dd className="min-w-0 break-words text-end text-sm text-carbon-text">{children}</dd>
    </div>
  );
}

// InstanceCard shows one instance: its state, where and how it is reached, and
// three live figures. What does not fit a compact card opens in a window.
export function InstanceCard({
  entry,
  name,
  address,
  route,
  base,
  onOpen,
  onRemove,
  onReach,
  hot = false,
  isSelf = false,
  deployment,
}: {
  /** The key the card shares with its node in the net. */
  entry: string;
  /** A peer's displayName or name, never the raw relay address. */
  name: string;
  /** What the instance is, "container" or "desktop", when it is known. */
  deployment?: string;
  /** Where the instance is reached, empty when it has no address. */
  address: string;
  /** How this instance reaches it: directly, through the relay or by its
   *  address. Empty on the card of the instance being viewed. */
  route?: string;
  base: string;
  onOpen?: () => void;
  /** Opens the question that takes the instance out; without it the card has
   *  no such button, as the card of this instance has none. */
  onRemove?: () => void;
  /** Tells the page whether the instance answers, for its node in the net. */
  onReach?: (online: boolean) => void;
  /** The pointer or the focus is on the instance's node in the net. */
  hot?: boolean;
  /** Marks the card of the instance being viewed. */
  isSelf?: boolean;
}) {
  const { t } = useT();
  const stats = usePeerStats(base);
  const [details, setDetails] = useState(false);
  const online = stats?.online ?? false;
  const refused = stats?.refused ?? false;
  const answered = stats !== null;
  useEffect(() => {
    if (answered) onReach?.(online);
  }, [answered, online]);

  // "Refused" is neither up nor down: the instance is there and wants pairing
  // again, which the (i) says.
  const state = online ? (
    <StatePill label={t('instances.connected')} tone="ok" />
  ) : refused ? (
    <StatePill label={t('instances.refused')} tone="warn" tip={t('instances.refusedByPassword')} />
  ) : (
    <StatePill label={t('instances.notConnected')} tone="fail" />
  );
  const kind = deployment && KINDS[deployment] ? t(KINDS[deployment]) : '';
  const figures = (
    <>
      <Figure value={stats?.active ?? '-'} label={t('instances.metricActive')} />
      <Figure value={stats?.total ?? '-'} label={t('instances.metricTasks')} />
      <Figure value={stats ? fmtSpeed(stats.speed) || '0' : '-'} label={t('instances.metricSpeed')} />
    </>
  );

  return (
    <>
      <Tile
        entry={entry}
        hot={hot}
        mark={<CardLogo />}
        name={name}
        eyebrow={isSelf ? t('instances.thisInstance') : undefined}
        state={state}
        facts={[address, kind, route ?? '']}
        figures={figures}
        actions={
          <>
            {onOpen && (
              <Button kind="secondary" className="kl-raised-btn" icon={<IconEye />} onClick={onOpen}>
                {t('instances.open')}
              </Button>
            )}
            {!isSelf && (
              <Button kind="secondary" className="kl-raised-btn" icon={<IconInfo />} onClick={() => setDetails(true)}>
                {t('instances.details')}
              </Button>
            )}
            {onRemove && (
              <Button
                kind="secondary"
                className="kl-raised-btn"
                icon={<IconTrash />}
                aria-label={t('instances.removeTitle', { name })}
                onClick={onRemove}
              >
                {t('instances.remove')}
              </Button>
            )}
          </>
        }
      />
      {details && (
        <Modal
          title={name}
          onClose={() => setDetails(false)}
          footer={
            <Button kind="secondary" labelled icon={<IconClose />} title={t('common.close')} onClick={() => setDetails(false)} />
          }
        >
          <dl className="flex flex-col">
            <DetailRow label={t('columns.status')}>
              <span className="inline-flex">{state}</span>
            </DetailRow>
            {kind && <DetailRow label={t('events.filterLabel')}>{kind}</DetailRow>}
            {address && (
              <DetailRow label={t('detail.address')}>
                <span dir="ltr">{address}</span>
              </DetailRow>
            )}
            {route && <DetailRow label={t('columns.connection')}>{route}</DetailRow>}
          </dl>
          {refused && <p className="text-sm text-carbon-textSub">{t('instances.refusedByPassword')}</p>}
          <div className="flex flex-wrap items-baseline gap-x-7 gap-y-3 rounded-[var(--radius-control)] bg-carbon-surface2 px-4 py-3">
            {figures}
          </div>
        </Modal>
      )}
    </>
  );
}

// AppCard shows a phone or browser extension that joined the group with the
// phrase: its name, whether it is there now and when it last was. It serves no
// downloads, so it has no figures and nothing to open.
export function AppCard({
  entry,
  name,
  deployment,
  connected,
  lastSeen,
  onRemove,
  hot = false,
}: {
  /** The key the card shares with its node in the net. */
  entry: string;
  name: string;
  /** "mobile" or "extension". */
  deployment: string;
  connected: boolean;
  /** Unix seconds. */
  lastSeen: number;
  onRemove: () => void;
  hot?: boolean;
}) {
  const { t } = useT();
  return (
    <Tile
      entry={entry}
      hot={hot}
      mark={<ClientMark deployment={deployment} />}
      name={name}
      eyebrow={KINDS[deployment] ? t(KINDS[deployment]) : undefined}
      state={
        <StatePill label={connected ? t('instances.connected') : t('instances.notConnected')} tone={connected ? 'ok' : 'fail'} />
      }
      facts={[t('instances.lastSeen', { time: fmtDate(new Date(lastSeen * 1000).toISOString()) })]}
      actions={
        <Button
          kind="secondary"
          className="kl-raised-btn"
          icon={<IconTrash />}
          aria-label={t('instances.removeTitle', { name })}
          onClick={onRemove}
        >
          {t('instances.remove')}
        </Button>
      }
    />
  );
}
