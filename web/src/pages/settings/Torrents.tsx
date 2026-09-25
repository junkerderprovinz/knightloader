import { useCallback, useState } from 'react';
import { Button, Card, Field, NumberInput, PageHeader, SectionTitle, ToggleRow } from '../../components/ui';
import { interpolate, useT, type TranslationKey } from '../../lib/i18n';
import { useDraft } from './context';
import { ModuleToggle } from './ModuleToggle';

/**
 * Torrents sets the seed target, transfer limit, port with its UPnP mapping,
 * and DHT/PEX. settings.Torrent is a flat group of fields, so the page uses the
 * shared draft like Reconnect.tsx. lib/api.ts's Settings does not name
 * `torrent`, so readTorrent casts the way readReconnect does.
 */

interface TorrentSettings {
  seedRatioTarget: number;
  seedDurationSeconds: number;
  uploadLimitKiBs: number;
  port: number;
  dhtEnabled: boolean;
  pexEnabled: boolean;
}

// For an older server that sends no `torrent`; mirrors settings.defaultTorrent().
const DEFAULTS: TorrentSettings = {
  seedRatioTarget: 1,
  seedDurationSeconds: 2 * 60 * 60,
  uploadLimitKiBs: 0,
  port: 0,
  dhtEnabled: true,
  pexEnabled: true,
};

function readTorrent(cfg: unknown): TorrentSettings {
  return { ...DEFAULTS, ...((cfg as { torrent?: Partial<TorrentSettings> }).torrent ?? {}) };
}

/**
 * PENDING holds the English strings until the catalogue has them; cx asks the
 * catalogue first.
 */
const PENDING = {
  'settings.torrents.title': 'Torrents',
  'settings.torrents.subtitle':
    'Seed targets, transfer limits, port mapping and DHT/PEX for magnet links and .torrent files.',
  'settings.torrents.seedingTitle': 'Seeding',
  'settings.torrents.seedRatio': 'Seed ratio target',
  'settings.torrents.seedRatioHint':
    'Keep seeding a finished torrent until this much has gone back to the swarm, relative to its own size. 0 = no ratio target.',
  'settings.torrents.seedDuration': 'Seed duration target',
  'settings.torrents.seedDurationHint':
    'Keep seeding a finished torrent for this long after it completes. 0 = no time limit. Whichever of the two targets above is reached first stops seeding.',
  'settings.torrents.seedDurationUnit': 'hours',
  'settings.torrents.transferTitle': 'Transfer limit',
  'settings.torrents.uploadLimit': 'Upload limit',
  'settings.torrents.uploadLimitHint': 'Caps how fast a torrent uploads to the swarm while seeding. 0 = unlimited.',
  'settings.torrents.uploadLimitUnit': 'KiB/s',
  'settings.torrents.portTitle': 'Port & mapping',
  'settings.torrents.port': 'Port',
  'settings.torrents.portHint':
    'The port this instance listens for swarm connections on. 0 lets the torrent engine pick one.',
  'settings.torrents.portMapHint':
    'Asks the router to forward the port above to this machine over UPnP, so peers behind a different router can still reach it. Not every router supports this, and some accept the request without it actually working.',
  'settings.torrents.portMapButton': 'Attempt UPnP mapping',
  'settings.torrents.portMapping': 'Asking the router…',
  'settings.torrents.portMapNeedsPort': 'Set a port above before mapping it - 0 leaves nothing for the router to forward to.',
  'settings.torrents.portMapConfirmed': 'Confirmed: port {port} is mapped and was verified reachable.',
  'settings.torrents.portMapUnconfirmed':
    'The router accepted the request, but the mapping could not be confirmed as actually working. Some routers do this silently - try a connectivity check from outside the network.',
  'settings.torrents.portMapFailed': 'Could not map the port: {error}',
  'settings.torrents.portMapUnavailable': 'This build does not expose port mapping yet.',
  'settings.torrents.networkTitle': 'Peer discovery',
  'settings.torrents.dht': 'DHT',
  'settings.torrents.dhtHint': 'Finds peers with no tracker involved, using other BitTorrent clients as a distributed lookup.',
  'settings.torrents.pex': 'Peer exchange (PEX)',
  'settings.torrents.pexHint': 'Trades known peers with the ones already connected, so a swarm with few peers is found faster.',
  'settings.torrents.privateNote':
    'A private torrent switches both off automatically once its metadata is known, regardless of what is set here - immediately for an uploaded .torrent file, or as soon as a magnet link\'s own metadata arrives from the swarm. Most private trackers ban accounts that use either.',
  'settings.torrents.engineLimits':
    'Seed ratio and seed duration reach every torrent this engine starts. The port reaches only the first torrent started since this instance’s last restart, because the engine builds its own torrent client once and never rebuilds it. A later port change is still saved correctly and takes effect after the next restart. The upload limit is only saved and validated so far; the engine has no way yet to apply it to a running download. For an ordinary torrent, DHT and PEX below do not take effect yet either: this instance’s own default does not reach a running download, so a torrent seeds with both on regardless of what is set here. A private torrent is a different case, explained in the (i) of Peer discovery further down. The mapping button further down works regardless: it asks the router to forward the port number typed above, whether or not a torrent is listening on it yet.',
} as const;

type PendingKey = keyof typeof PENDING;
type Cx = (key: PendingKey, vars?: Record<string, string | number>) => string;

function useCx(): Cx {
  const { t } = useT();
  return useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      // These keys are not in the union yet; only PENDING keys can be passed.
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      return interpolate(translated ?? PENDING[key], vars);
    },
    [t],
  );
}

export function Torrents() {
  const cx = useCx();
  const { cfg, patch } = useDraft();
  const tr = readTorrent(cfg);

  const write = useCallback(
    (fields: Partial<TorrentSettings>) => {
      const next = { ...readTorrent(cfg), ...fields };
      // patch's Partial<Settings> does not name `torrent` either.
      patch({ torrent: next } as unknown as Parameters<typeof patch>[0]);
    },
    [cfg, patch],
  );

  // Stored in seconds, shown in hours.
  const seedHours = Math.round(tr.seedDurationSeconds / 3600);

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={cx('settings.torrents.title')} />

      {/* Seed ratio, seed duration and port reach the engine; the upload limit
          and the DHT/PEX default for ordinary torrents have no gopeed setting
          to reach, and the note says which is which. */}
      <div className="glim-well px-3 py-2.5 text-[11px] text-statusWarn">
        {cx('settings.torrents.engineLimits')}
      </div>

      <Card hue={0} className="flex flex-col gap-5">
        <SectionTitle>{cx('settings.torrents.seedingTitle')}</SectionTitle>
        <ModuleToggle id="torrents" />
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label={cx('settings.torrents.seedRatio')} hint={cx('settings.torrents.seedRatioHint')}>
            <NumberInput
              value={tr.seedRatioTarget}
              min={0}
              max={50}
              step={0.1}
              onValue={(v) => write({ seedRatioTarget: Math.max(0, v) })}
            />
          </Field>
          <Field label={cx('settings.torrents.seedDuration')} hint={cx('settings.torrents.seedDurationHint')}>
            <div className="flex items-center gap-2">
              <NumberInput
                value={seedHours}
                min={0}
                max={8760}
                onValue={(v) => write({ seedDurationSeconds: Math.max(0, v) * 3600 })}
              />
              <span className="glim-num shrink-0 text-xs text-carbon-textMuted">
                {cx('settings.torrents.seedDurationUnit')}
              </span>
            </div>
          </Field>
        </div>
      </Card>

      <Card hue={1} className="flex flex-col gap-5">
        <SectionTitle>{cx('settings.torrents.transferTitle')}</SectionTitle>
        <Field label={cx('settings.torrents.uploadLimit')} hint={cx('settings.torrents.uploadLimitHint')}>
          <div className="flex items-center gap-2">
            <NumberInput
              value={tr.uploadLimitKiBs}
              min={0}
              onValue={(v) => write({ uploadLimitKiBs: Math.max(0, v) })}
            />
            <span className="glim-num shrink-0 text-xs text-carbon-textMuted">
              {cx('settings.torrents.uploadLimitUnit')}
            </span>
          </div>
        </Field>
      </Card>

      <Card hue={2} className="flex flex-col gap-5">
        <SectionTitle>{cx('settings.torrents.portTitle')}</SectionTitle>
        <Field label={cx('settings.torrents.port')} hint={cx('settings.torrents.portHint')}>
          <NumberInput
            value={tr.port}
            min={0}
            max={65535}
            onValue={(v) => write({ port: Math.max(0, Math.min(65535, v)) })}
          />
        </Field>
        <PortMapPanel cx={cx} port={tr.port} />
      </Card>

      <Card hue={3} className="flex flex-col gap-4">
        <SectionTitle hint={cx('settings.torrents.privateNote')}>{cx('settings.torrents.networkTitle')}</SectionTitle>
        <ToggleRow
          checked={tr.dhtEnabled}
          onChange={(v) => write({ dhtEnabled: v })}
          label={cx('settings.torrents.dht')}
          hint={cx('settings.torrents.dhtHint')}
        />
        <ToggleRow
          checked={tr.pexEnabled}
          onChange={(v) => write({ pexEnabled: v })}
          label={cx('settings.torrents.pex')}
          hint={cx('settings.torrents.pexHint')}
        />
      </Card>
    </div>
  );
}

type PortMapOutcome = 'confirmed' | 'unconfirmed' | 'failed';

/** PortMapResult mirrors internal/portmap.Result and its json tags. */
interface PortMapResult {
  outcome: PortMapOutcome;
  reason: string;
  detail?: string;
  gateway?: string;
  internalPort?: number;
  externalPort?: number;
}

/**
 * PortMapPanel runs a one-off mapping attempt on click; it is neither loaded on
 * mount nor part of the draft.
 */
function PortMapPanel({ cx, port }: { cx: Cx; port: number }) {
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<PortMapResult | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  const [error, setError] = useState('');

  async function attempt() {
    setBusy(true);
    setResult(null);
    setUnavailable(false);
    setError('');
    try {
      const r = await fetch('/api/torrents/portmap', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ port }),
      });
      // A 404 means this build has no portmap route, unlike a router refusal.
      if (r.status === 404) {
        setUnavailable(true);
        return;
      }
      if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
      setResult((await r.json()) as PortMapResult);
    } catch (e) {
      setError(String(e).replace(/^(Error|TypeError):\s*/, ''));
    } finally {
      setBusy(false);
    }
  }

  const disabled = busy || port <= 0;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <Button kind="secondary" disabled={disabled} hint={cx('settings.torrents.portMapHint')} onClick={() => void attempt()}>
          {busy ? cx('settings.torrents.portMapping') : cx('settings.torrents.portMapButton')}
        </Button>
      </div>
      {port <= 0 && <p className="text-[11px] text-statusWarn">{cx('settings.torrents.portMapNeedsPort')}</p>}
      {unavailable && <p className="text-xs text-carbon-textMuted">{cx('settings.torrents.portMapUnavailable')}</p>}
      {error && <p className="text-xs text-statusFail">{cx('settings.torrents.portMapFailed', { error })}</p>}
      {result?.outcome === 'confirmed' && (
        <p className="text-xs text-statusOk">{cx('settings.torrents.portMapConfirmed', { port })}</p>
      )}
      {result?.outcome === 'unconfirmed' && (
        <p className="text-xs text-statusWarn">{cx('settings.torrents.portMapUnconfirmed')}</p>
      )}
      {result?.outcome === 'failed' && (
        <p className="text-xs text-statusFail">
          {cx('settings.torrents.portMapFailed', { error: result.detail ?? result.reason })}
        </p>
      )}
    </div>
  );
}
