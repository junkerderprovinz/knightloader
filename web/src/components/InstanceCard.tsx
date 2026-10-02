import { useEffect, useState } from 'react';
import logoUrl from '../assets/logo.svg';
import { ApiError, fetchTasks, type Task } from '../lib/api';
import { fmtDate, fmtSpeed } from '../lib/format';
import { useT, type TranslationKey } from '../lib/i18n';
import { Card, Button, LabelBadge } from './ui';
import { IconBrowser, IconContainers, IconDesktop, IconPhone } from '../lib/icons';

// What each kind of instance is drawn as, keyed by buildinfo.Deployment and by
// what the two clients announce themselves as, "mobile" and "extension".
const KINDS: Record<string, { Glyph: typeof IconPhone; label: TranslationKey }> = {
  mobile: { Glyph: IconPhone, label: 'instances.kind.mobile' },
  desktop: { Glyph: IconDesktop, label: 'instances.kind.desktop' },
  container: { Glyph: IconContainers, label: 'instances.kind.container' },
  extension: { Glyph: IconBrowser, label: 'instances.kind.extension' },
};

/**
 * KindBadge says beside the state badge what the instance is: the Android app,
 * the desktop app or the container. A kind this build does not know, or a peer
 * that never said, has none.
 */
function KindBadge({ deployment }: { deployment?: string }) {
  const { t } = useT();
  const kind = deployment ? KINDS[deployment] : undefined;
  if (!kind) return null;
  const { Glyph } = kind;
  const label = t(kind.label);
  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      data-kind={deployment}
      className="inline-flex h-[var(--btn-h)] w-[var(--btn-h)] shrink-0 items-center justify-center rounded-[var(--radius-pill)]
        bg-carbon-surface2 text-carbon-textSub"
    >
      <Glyph width={16} height={16} />
    </span>
  );
}

/** The KnightLoader mark at the card's start edge, the same on every card of the page. */
function CardLogo() {
  // h-26 matches the sidebar's brand mark (check-mark-scale.mjs); a larger
  // mark squeezes the metric labels together. max-h-full keeps the text
  // column in charge of the card's height.
  return (
    <div className="flex shrink-0 items-center self-stretch ps-4">
      <img src={logoUrl} alt="" aria-hidden className="h-26 max-h-full w-auto" />
    </div>
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

// InstanceCard shows one instance: its state, its address where it has one, and
// three live figures.
export function InstanceCard({
  name,
  address,
  base,
  onOpen,
  hue,
  isSelf = false,
  deployment,
}: {
  /** A peer's displayName or name, never the raw relay address. */
  name: string;
  /** What the instance is, "container" or "desktop", when it is known. */
  deployment?: string;
  /** Where the instance is reached, empty when it has no address. */
  address: string;
  base: string;
  onOpen?: () => void;
  /** The card's palette position; .glim-hue colours the whole subtree. */
  hue?: number;
  /** Marks the card of the instance being viewed. */
  isSelf?: boolean;
}) {
  const { t } = useT();
  const stats = usePeerStats(base);
  const online = stats?.online ?? false;
  const refused = stats?.refused ?? false;
  // "Refused" takes a rainbow hue, since it is neither ok nor failed, and its
  // (i) says why and what makes the two instances trust each other.
  const state = online ? t('instances.connected') : refused ? t('instances.refused') : t('instances.notConnected');

  return (
    // padding="none" so the logo runs flush to the start edge at full height,
    // clipped by overflow-hidden. No `hover` lift: the card itself is not
    // clickable (check-card-hover.mjs guards this).
    <Card padding="none" hue={hue} className="flex h-full flex-col overflow-hidden">
      {/* The two columns form one row and the Open button a second, so the
          button can never overlap the text. */}
      <div className="flex min-h-0 flex-1 items-stretch">
        <CardLogo />

        <div className="flex min-w-0 flex-1 flex-col gap-4 p-7">
          {/* Name and address as one block. */}
          <div className="flex flex-col gap-0.5">
            {/* The badges move under the name when both do not fit on one line. */}
            <div className="flex flex-wrap items-center gap-2.5">
              <span className="min-w-0 truncate font-semibold text-carbon-text">{name}</span>
              {isSelf && <span className="glim-eyebrow shrink-0">{t('instances.thisInstance')}</span>}
              <span className="ms-auto flex items-center gap-2">
                <KindBadge deployment={deployment} />
                <LabelBadge
                  label={state}
                  tip={refused ? t('instances.refusedByPassword') : undefined}
                  tone={online ? 'ok' : refused ? undefined : 'fail'}
                  hue={refused ? 3 : undefined}
                />
              </span>
            </div>

            {address && <div className="truncate text-xs text-carbon-textMuted">{address}</div>}
          </div>

          {/* A figure that does not fit moves to the next line rather than
              running into its neighbour's label. */}
          <div className="flex flex-wrap items-baseline gap-x-7 gap-y-3">
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

// AppCard shows a phone or browser extension that joined the group with the
// phrase: its name, whether it is there now and when it last was. It serves no
// downloads, so it has no figures and nothing to open; the pairing list is
// where members are managed.
export function AppCard({
  name,
  deployment,
  connected,
  lastSeen,
  hue,
}: {
  name: string;
  /** "mobile" or "extension". */
  deployment: string;
  connected: boolean;
  /** Unix seconds. */
  lastSeen: number;
  hue?: number;
}) {
  const { t } = useT();
  return (
    <Card padding="none" hue={hue} className="flex h-full flex-col overflow-hidden">
      <div className="flex min-h-0 flex-1 items-stretch">
        <CardLogo />
        <div className="flex min-w-0 flex-1 flex-col gap-1 p-7">
          <div className="flex flex-wrap items-center gap-2.5">
            <span className="min-w-0 truncate font-semibold text-carbon-text">{name}</span>
            <span className="ms-auto flex items-center gap-2">
              <KindBadge deployment={deployment} />
              <LabelBadge
                label={connected ? t('instances.connected') : t('instances.notConnected')}
                tone={connected ? 'ok' : 'fail'}
              />
            </span>
          </div>
          <div className="truncate text-xs text-carbon-textMuted">
            {t('instances.lastSeen', { time: fmtDate(new Date(lastSeen * 1000).toISOString()) })}
          </div>
        </div>
      </div>
      {/* The room an instance card's Open button takes, so the logo sits at
          the same height on every card of a row. */}
      <div aria-hidden="true" className="mx-5 mb-5 h-[var(--btn-h)] shrink-0" />
    </Card>
  );
}

function Metric({ value, label }: { value: React.ReactNode; label: string }) {
  return (
    <div className="shrink-0">
      <div className="glim-num text-sm font-semibold text-carbon-text">{value}</div>
      <div className="glim-eyebrow">{label}</div>
    </div>
  );
}
