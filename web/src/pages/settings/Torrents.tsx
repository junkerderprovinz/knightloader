import { useCallback, useEffect, useState } from 'react';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  NumberInput,
  PageHeader,
  SectionTitle,
  TextInput,
  ToggleRow,
  UnitNumberInput,
} from '../../components/ui';
import { Tabs } from '../../components/Tabs';
import type { TorrentFileRules } from '../../lib/api';
import { happened } from '../../lib/countdown';
import { fmtDate } from '../../lib/format';
import { useT } from '../../lib/i18n';
import { useDraft } from './context';
import { ListArea, RowRefusal } from './controls';
import { ModuleToggle } from './ModuleToggle';
import { UploadLimitField } from './torrents/UploadLimit';

/**
 * Torrents sets the seed target, transfer limit, port with its UPnP mapping,
 * DHT/PEX, the file selection, the trackers, and how torrents go through a
 * debrid service and where what it fetched seeds. settings.Torrent is a flat
 * group of fields but for its qBittorrent, so the page uses the shared draft
 * like Reconnect.tsx. lib/api.ts's Settings does not name `torrent`, so
 * readTorrent casts the way readReconnect does.
 */

interface TorrentSettings {
  seedRatioTarget: number;
  seedDurationSeconds: number;
  uploadLimitKiBs: number;
  port: number;
  dhtEnabled: boolean;
  pexEnabled: boolean;
  minFileSize: number;
  // The server sends null for an empty list.
  includeFiles: string[] | null;
  excludeFiles: string[] | null;
  extraTrackers: string[] | null;
  trackerListUrl: string;
  bannedTrackers: string[] | null;
  keepOnService: boolean;
  debridCachedOnly: boolean;
  debridStallMinutes: number;
  seedAfterDebrid: boolean;
  /** Empty is the built-in client too, as settings.Torrent.SeedIn has it. */
  seedIn: 'builtin' | 'qbittorrent' | '';
  qbittorrent: QBittorrentSettings;
}

/** Mirrors settings.QBittorrent. */
interface QBittorrentSettings {
  url: string;
  username: string;
  password: string;
  category: string;
  downloadsPath: string;
}

/** The mask for a stored qBittorrent password; a protocol value shared with reconnect.RedactedPassword. */
const REDACTED = '********';

// For an older server that sends no `torrent`; mirrors settings.defaultTorrent().
const DEFAULTS: TorrentSettings = {
  seedRatioTarget: 1,
  seedDurationSeconds: 2 * 60 * 60,
  uploadLimitKiBs: 0,
  port: 0,
  dhtEnabled: true,
  pexEnabled: true,
  minFileSize: 0,
  includeFiles: [],
  excludeFiles: [],
  extraTrackers: [],
  trackerListUrl: '',
  bannedTrackers: [],
  keepOnService: false,
  debridCachedOnly: false,
  debridStallMinutes: 10,
  seedAfterDebrid: false,
  seedIn: 'builtin',
  qbittorrent: { url: '', username: '', password: '', category: '', downloadsPath: '' },
};

// A day, as settings.maxDebridStallMinutes caps it.
const DEBRID_STALL_MAX = 24 * 60;

const KIB = 1024;

// A file size is entered like a speed, without the "/s".
const SIZE_UNITS = [
  { label: 'KiB', factor: KIB, step: 256 * KIB },
  { label: 'MiB', factor: KIB ** 2, step: KIB ** 2 },
  { label: 'GiB', factor: KIB ** 3, step: KIB ** 3 },
] as const;

function readTorrent(cfg: unknown): TorrentSettings {
  const tr = (cfg as { torrent?: Partial<TorrentSettings> }).torrent ?? {};
  return { ...DEFAULTS, ...tr, qbittorrent: { ...DEFAULTS.qbittorrent, ...(tr.qbittorrent ?? {}) } };
}

export function Torrents() {
  const { t } = useT();
  const { cfg, saved, patch } = useDraft();
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
        <UploadLimitField value={tr.uploadLimitKiBs} onValue={(uploadLimitKiBs) => write({ uploadLimitKiBs })} />
      </Card>

      <Card hue={2} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.torrents.portTitle')}</SectionTitle>
        <Field
          label={t('settings.torrents.port')}
          hint={[
            t('settings.torrents.portHint'),
            t('settings.torrents.clientApplyHint'),
            t('settings.torrents.portMapNowHint'),
          ]}
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
          hint={[t('settings.torrents.dhtHint'), t('settings.torrents.clientApplyHint')]}
        />
        <ToggleRow
          checked={tr.pexEnabled}
          onChange={(v) => write({ pexEnabled: v })}
          label={t('settings.torrents.pex')}
          hint={[t('settings.torrents.pexHint'), t('settings.torrents.clientApplyHint')]}
        />
      </Card>

      <Card hue={4} className="flex flex-col gap-5">
        <SectionTitle hint={t('settings.torrents.filesHint')}>{t('settings.torrents.filesTitle')}</SectionTitle>
        <FileSelectionFields
          rules={{ minFileSize: tr.minFileSize, includeFiles: tr.includeFiles, excludeFiles: tr.excludeFiles }}
          onChange={write}
          refusedAt="torrent"
        />
      </Card>

      <Card hue={5} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.torrents.trackersTitle')}</SectionTitle>
        <LinesField
          lines={tr.extraTrackers}
          label={t('settings.torrents.extraTrackers')}
          hint={t('settings.torrents.extraTrackersHint')}
          refusal="torrent.extraTrackers"
          onLines={(extraTrackers) => write({ extraTrackers })}
        />
        <div className="flex flex-col gap-1.5">
          <Field label={t('settings.torrents.trackerList')} hint={t('settings.torrents.trackerListHint')}>
            <TextInput
              type="url"
              dir="ltr"
              spellCheck={false}
              placeholder="https://ngosang.github.io/trackerslist/trackers_best.txt"
              value={tr.trackerListUrl}
              onChange={(e) => write({ trackerListUrl: e.target.value })}
            />
          </Field>
          <RowRefusal field="torrent.trackerListUrl" className="pb-0" />
          <TrackerListStatus saved={readTorrent(saved).trackerListUrl.trim()} />
        </div>
        <LinesField
          lines={tr.bannedTrackers}
          label={t('settings.torrents.bannedTrackers')}
          hint={t('settings.torrents.bannedTrackersHint')}
          refusal="torrent.bannedTrackers"
          onLines={(bannedTrackers) => write({ bannedTrackers })}
        />
      </Card>

      <Card hue={6} className="flex flex-col gap-4">
        <SectionTitle hint={t('settings.torrents.debridHint')}>{t('settings.torrents.debridTitle')}</SectionTitle>
        <ToggleRow
          checked={tr.keepOnService}
          onChange={(v) => write({ keepOnService: v })}
          label={t('settings.torrents.keepOnService')}
          hint={t('settings.torrents.keepOnServiceHint')}
        />
        <ToggleRow
          checked={tr.debridCachedOnly}
          onChange={(v) => write({ debridCachedOnly: v })}
          label={t('settings.torrents.cachedOnly')}
          hint={t('settings.torrents.cachedOnlyHint')}
        />
        <Field label={t('settings.torrents.debridStall')} hint={t('settings.torrents.debridStallHint')}>
          <div className="flex items-center gap-2">
            <NumberInput
              value={tr.debridStallMinutes}
              min={0}
              max={DEBRID_STALL_MAX}
              onValue={(v) => write({ debridStallMinutes: Math.max(0, Math.min(DEBRID_STALL_MAX, v)) })}
            />
            <span className="glim-num shrink-0 text-xs text-carbon-textMuted">
              {t('settings.torrents.debridStallUnit')}
            </span>
          </div>
        </Field>
        <ToggleRow
          checked={tr.seedAfterDebrid}
          onChange={(v) => write({ seedAfterDebrid: v })}
          label={t('settings.torrents.seedAfterDebrid')}
          hint={t('settings.torrents.seedAfterDebridHint')}
        />
        {tr.seedAfterDebrid && (
          <FieldGroup layout="row" label={t('settings.torrents.seedIn')} hint={t('settings.torrents.seedInHint')}>
            <Tabs
              variant="well"
              label={t('settings.torrents.seedIn')}
              active={tr.seedIn === 'qbittorrent' ? 'qbittorrent' : 'builtin'}
              onSelect={(seedIn) => write({ seedIn: seedIn as TorrentSettings['seedIn'] })}
              items={[
                { id: 'builtin', label: t('settings.torrents.seedIn.builtin') },
                { id: 'qbittorrent', label: t('settings.torrents.seedIn.qbittorrent') },
              ]}
            />
          </FieldGroup>
        )}
        {tr.seedAfterDebrid && tr.seedIn === 'qbittorrent' && (
          <QBittorrentFields
            q={tr.qbittorrent}
            savedUrl={readTorrent(saved).qbittorrent.url}
            onChange={(fields) => write({ qbittorrent: { ...tr.qbittorrent, ...fields } })}
          />
        )}
      </Card>
    </div>
  );
}

/**
 * FileSelectionFields edits a torrent file selection: the minimum size and the
 * two pattern boxes. The Torrents page draws it, and so does a category with a
 * selection of its own. refusedAt is the settings path the server files a
 * refused box under; without it a refusal shows where the caller puts it, as a
 * category does on its row.
 */
export function FileSelectionFields({
  rules,
  onChange,
  refusedAt,
}: {
  rules: TorrentFileRules;
  onChange: (next: TorrentFileRules) => void;
  refusedAt?: string;
}) {
  const { t } = useT();
  return (
    <>
      {/* Stored in bytes, so a sample of 40 MiB is as easy to write as an
          .nfo of 3 KiB. */}
      <Field label={t('settings.torrents.minFileSize')} hint={t('settings.torrents.minFileSizeHint')}>
        <UnitNumberInput
          value={rules.minFileSize}
          units={SIZE_UNITS}
          snap={(bytes) => Math.round(bytes)}
          onValue={(minFileSize) => onChange({ ...rules, minFileSize })}
        />
      </Field>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <LinesField
          lines={rules.includeFiles}
          label={t('settings.torrents.includeFiles')}
          hint={t('settings.torrents.includeFilesHint')}
          refusal={refusedAt && `${refusedAt}.includeFiles`}
          onLines={(includeFiles) => onChange({ ...rules, includeFiles })}
        />
        <LinesField
          lines={rules.excludeFiles}
          label={t('settings.torrents.excludeFiles')}
          hint={t('settings.torrents.excludeFilesHint')}
          refusal={refusedAt && `${refusedAt}.excludeFiles`}
          onLines={(excludeFiles) => onChange({ ...rules, excludeFiles })}
        />
      </div>
    </>
  );
}

/**
 * QBittorrentFields hold the qBittorrent that seeds what a debrid service
 * fetched. The stored password comes back masked; the box stays empty and the
 * draft keeps the mask, which the save merges back while the address is the
 * one saved. savedUrl is that address, so an edited one asks for the password
 * again, as the save will.
 */
function QBittorrentFields({
  q,
  savedUrl,
  onChange,
}: {
  q: QBittorrentSettings;
  savedUrl: string;
  onChange: (fields: Partial<QBittorrentSettings>) => void;
}) {
  const { t } = useT();
  const stored = q.password === REDACTED && q.url.trim() === savedUrl.trim();
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <Field label={t('settings.torrents.qbitUrl')} hint={t('settings.torrents.qbitUrlHint')}>
          <TextInput
            type="url"
            dir="ltr"
            spellCheck={false}
            placeholder="http://192.168.1.10:8080"
            value={q.url}
            onChange={(e) => onChange({ url: e.target.value })}
          />
        </Field>
        <RowRefusal field="torrent.qbittorrent.url" className="pb-0" />
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Field label={t('settings.torrents.qbitUsername')} hint={t('settings.torrents.qbitUsernameHint')}>
          <TextInput
            dir="ltr"
            autoComplete="off"
            spellCheck={false}
            value={q.username}
            onChange={(e) => onChange({ username: e.target.value })}
          />
        </Field>
        <Field label={t('settings.torrents.qbitPassword')} hint={t('settings.torrents.qbitPasswordHint')}>
          <TextInput
            type="password"
            dir="ltr"
            autoComplete="new-password"
            value={q.password === REDACTED ? '' : q.password}
            placeholder={stored ? t('settings.torrents.qbitPasswordStored') : ''}
            onChange={(e) => onChange({ password: e.target.value })}
          />
        </Field>
        <Field label={t('settings.torrents.qbitCategory')} hint={t('settings.torrents.qbitCategoryHint')}>
          <TextInput
            dir="ltr"
            spellCheck={false}
            value={q.category}
            onChange={(e) => onChange({ category: e.target.value })}
          />
        </Field>
        <Field label={t('settings.torrents.qbitDownloadsPath')} hint={t('settings.torrents.qbitDownloadsPathHint')}>
          <TextInput
            dir="ltr"
            spellCheck={false}
            placeholder="/data/downloads"
            value={q.downloadsPath}
            onChange={(e) => onChange({ downloadsPath: e.target.value })}
          />
        </Field>
      </div>
      <QBittorrentTest q={q} />
    </div>
  );
}

/**
 * QBittorrentTest posts the fields exactly as edited; the server merges a
 * stored password back as a save would, so the test covers what a save would
 * write.
 */
function QBittorrentTest({ q }: { q: QBittorrentSettings }) {
  const { t } = useT();
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ version?: string; error?: string } | null>(null);

  async function run() {
    setBusy(true);
    setResult(null);
    try {
      const r = await fetch('/api/torrents/qbittorrent/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(q),
      });
      if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
      setResult((await r.json()) as { version?: string; error?: string });
    } catch (e) {
      setResult({ error: String(e).replace(/^(Error|TypeError):\s*/, '') });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <Button kind="secondary" disabled={busy} hint={t('settings.torrents.qbitTestHint')} onClick={() => void run()}>
          {busy ? t('settings.torrents.qbitTesting') : t('settings.torrents.qbitTest')}
        </Button>
      </div>
      {result?.version && (
        <p className="text-xs text-statusOk">{t('settings.torrents.qbitTestOk', { version: result.version })}</p>
      )}
      {result?.error && (
        <p className="text-xs text-statusFail">{t('settings.torrents.qbitTestFailed', { error: result.error })}</p>
      )}
    </div>
  );
}

/**
 * LinesField edits a list as a box with one entry per line, with the server's
 * refusal of the list beneath it when refusal names where that is filed.
 */
function LinesField({
  lines,
  label,
  hint,
  refusal,
  onLines,
}: {
  lines: string[] | null;
  label: string;
  hint: string;
  refusal?: string;
  onLines: (lines: string[]) => void;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Field label={label} hint={hint}>
        <ListArea rows={3} dir="ltr" lines={lines} onLines={onLines} />
      </Field>
      {refusal && <RowRefusal field={refusal} className="pb-0" />}
    </div>
  );
}

/** TrackerListState mirrors internal/trackerlist.Status and its json tags. */
interface TrackerListState {
  url: string;
  trackers: number;
  fetchedAt?: string;
  error?: string;
  triedAt?: string;
  fetching: boolean;
}

// How often, and how many times at most, the line looks again while a fetch
// is under way.
const LIST_POLL_MS = 1500;
const LIST_POLL_TRIES = 20;

/**
 * TrackerListStatus says how the saved tracker list address fared. Saving an
 * address starts a fetch on the server, so the line looks again whenever the
 * saved address changes, and keeps looking while that fetch runs.
 */
function TrackerListStatus({ saved }: { saved: string }) {
  const { t } = useT();
  const [state, setState] = useState<TrackerListState | null>(null);

  useEffect(() => {
    if (!saved) {
      setState(null);
      return;
    }
    let live = true;
    let tries = 0;
    let timer: number | undefined;
    const look = async () => {
      try {
        const r = await fetch('/api/torrents/trackers');
        if (!r.ok) return;
        const next = (await r.json()) as TrackerListState;
        if (!live) return;
        setState(next);
        // The first look can come before the save's fetch has begun.
        if ((next.fetching || (!happened(next.fetchedAt) && !next.error)) && ++tries < LIST_POLL_TRIES) {
          timer = window.setTimeout(() => void look(), LIST_POLL_MS);
        }
      } catch {
        // An older server without the route shows no line.
      }
    };
    void look();
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [saved]);

  if (!saved || !state || state.url !== saved) return null;
  return (
    <div className="flex flex-col gap-0.5 text-xs">
      {state.trackers > 0 ? (
        <p className="text-carbon-textSub">
          {t('settings.torrents.trackerListFetched', { n: state.trackers, when: fmtDate(state.fetchedAt) })}
        </p>
      ) : (
        !state.error && (
          <p className="text-carbon-textMuted">
            {state.fetching ? t('settings.torrents.trackerListFetching') : t('settings.torrents.trackerListPending')}
          </p>
        )
      )}
      {state.error && <p className="text-statusWarn">{t('settings.torrents.trackerListFailed', { error: state.error })}</p>}
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
