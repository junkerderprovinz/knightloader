import { useCallback, useState } from 'react';
import { Button, Card, Field, NumberInput, PageHeader, SectionTitle, ToggleRow } from '../../components/ui';
import { useT } from '../../lib/i18n';
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

export function Torrents() {
  const { t } = useT();
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
      <PageHeader title={t('settings.torrents.title')} />

      {/* The upload limit and the DHT/PEX default for ordinary torrents have no
          gopeed setting to reach. Saying that this build does not apply them
          is the one note on the page that is not behind an (i). */}
      <div className="glim-well px-3 py-2.5 text-[11px] text-statusWarn">
        {t('settings.torrents.notApplied')}
      </div>

      <Card hue={0} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.torrents.seedingTitle')}</SectionTitle>
        <ModuleToggle id="torrents" />
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label={t('settings.torrents.seedRatio')} hint={t('settings.torrents.seedRatioHint')}>
            <NumberInput
              value={tr.seedRatioTarget}
              min={0}
              max={50}
              step={0.1}
              onValue={(v) => write({ seedRatioTarget: Math.max(0, v) })}
            />
          </Field>
          <Field label={t('settings.torrents.seedDuration')} hint={t('settings.torrents.seedDurationHint')}>
            <div className="flex items-center gap-2">
              <NumberInput
                value={seedHours}
                min={0}
                max={8760}
                onValue={(v) => write({ seedDurationSeconds: Math.max(0, v) * 3600 })}
              />
              <span className="glim-num shrink-0 text-xs text-carbon-textMuted">
                {t('settings.torrents.seedDurationUnit')}
              </span>
            </div>
          </Field>
        </div>
      </Card>

      <Card hue={1} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.torrents.transferTitle')}</SectionTitle>
        <Field label={t('settings.torrents.uploadLimit')} hint={t('settings.torrents.uploadLimitHint')}>
          <div className="flex items-center gap-2">
            <NumberInput
              value={tr.uploadLimitKiBs}
              min={0}
              onValue={(v) => write({ uploadLimitKiBs: Math.max(0, v) })}
            />
            <span className="glim-num shrink-0 text-xs text-carbon-textMuted">
              {t('settings.torrents.uploadLimitUnit')}
            </span>
          </div>
        </Field>
      </Card>

      <Card hue={2} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.torrents.portTitle')}</SectionTitle>
        <Field
          label={t('settings.torrents.port')}
          hint={[t('settings.torrents.portHint'), t('settings.torrents.portRestartHint')]}
        >
          <NumberInput
            value={tr.port}
            min={0}
            max={65535}
            onValue={(v) => write({ port: Math.max(0, Math.min(65535, v)) })}
          />
        </Field>
        <PortMapPanel port={tr.port} />
      </Card>

      <Card hue={3} className="flex flex-col gap-4">
        <SectionTitle hint={t('settings.torrents.privateNote')}>{t('settings.torrents.networkTitle')}</SectionTitle>
        <ToggleRow
          checked={tr.dhtEnabled}
          onChange={(v) => write({ dhtEnabled: v })}
          label={t('settings.torrents.dht')}
          hint={t('settings.torrents.dhtHint')}
        />
        <ToggleRow
          checked={tr.pexEnabled}
          onChange={(v) => write({ pexEnabled: v })}
          label={t('settings.torrents.pex')}
          hint={t('settings.torrents.pexHint')}
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
function PortMapPanel({ port }: { port: number }) {
  const { t } = useT();
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

  const noPort = port <= 0;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        {/* The (i) inside the button stays readable while it is disabled, so the
            reason goes there rather than under it. */}
        <Button
          kind="secondary"
          disabled={busy || noPort}
          hint={
            noPort
              ? `${t('settings.torrents.portMapNeedsPort')} ${t('settings.torrents.portMapHint')}`
              : t('settings.torrents.portMapHint')
          }
          onClick={() => void attempt()}
        >
          {busy ? t('settings.torrents.portMapping') : t('settings.torrents.portMapButton')}
        </Button>
      </div>
      {unavailable && <p className="text-xs text-carbon-textMuted">{t('settings.torrents.portMapUnavailable')}</p>}
      {error && <p className="text-xs text-statusFail">{t('settings.torrents.portMapFailed', { error })}</p>}
      {result?.outcome === 'confirmed' && (
        <p className="text-xs text-statusOk">{t('settings.torrents.portMapConfirmed', { port })}</p>
      )}
      {result?.outcome === 'unconfirmed' && (
        <p className="text-xs text-statusWarn">{t('settings.torrents.portMapUnconfirmed')}</p>
      )}
      {result?.outcome === 'failed' && (
        <p className="text-xs text-statusFail">
          {t('settings.torrents.portMapFailed', { error: result.detail ?? result.reason })}
        </p>
      )}
    </div>
  );
}
