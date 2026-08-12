import { useCallback, useState } from 'react';
import { Button, Card, Field, FieldGroup, NumberInput, PageHeader, SectionTitle, Toggle } from '../../components/ui';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft } from './context';

/**
 * The Torrents page: seed target, transfer limit, port and its UPnP mapping,
 * DHT/PEX - build-plan.md's 11.5E, backed by 11.5B's settings.Torrent and
 * 11.5C's port mapping route. Both landed mid-build, after this page was
 * already written against their documented/verified shape rather than their
 * actual code - see the readTorrent gap below and PortMapPanel's own doc
 * comment for the two places that dependency shows, and this wave's report
 * for how closely each guess matched what actually shipped.
 *
 * ON THE SHARED DRAFT, NOT A DEDICATED ROUTE. Schedule.tsx is the one settings
 * sub-page that reads and writes its own route instead of joining
 * context.tsx's shared draft, and its own doc comment gives the reason: a
 * timetable is a growable list, and PUT /api/schedule exists specifically so
 * two tabs editing it cannot silently clobber each other. Nothing here has
 * that shape - settings.Torrent is a flat handful of fields, embedded in
 * Settings exactly like Reconnect and Ytdlp already are (internal/settings/
 * settings.go) - so this page follows Reconnect.tsx/Archives.tsx instead:
 * useDraft(), patch() on the shared document, saved by the shell's one bar for
 * every sub-page at once.
 *
 * THE SETTINGS TYPE IN lib/api.ts DOES NOT NAME `torrent` YET. Same gap
 * Reconnect.tsx already has for `reconnect`, and the same fix: SettingsDraft's
 * own doc comment (context.tsx) says the type is a documented SUBSET of what
 * the server actually sends, so every edit spreads the object rather than
 * rebuilding it. readTorrent below is readReconnect's own two-cast shape,
 * copied rather than reinvented. This is not a gap this page can close from
 * here - widening lib/api.ts's Settings is nobody's named lane this wave
 * (11.5A-F's file list does not mention it), and Reconnect.tsx already
 * establishes that a settings page does not need to.
 *
 * THE UPnP BUTTON'S ROUTE STARTED AS AN ASSUMED CONTRACT AND IS NOW A
 * VERIFIED ONE. When this page was first built, 11.5C's own package,
 * internal/portmap, was real, complete and tested (portmap.Attempt,
 * soap_test.go) but had no HTTP route registered anywhere in internal/api -
 * the same "fully-built, fully-tested piece of code with zero callers" shape
 * this session keeps finding elsewhere. internal/api/routes_portmap.go has
 * since landed, live-tested against this page (see this wave's report): a
 * real POST /api/torrents/portmap, taking {port} and answering
 * portmap.Result's own JSON shape verbatim (Outcome/Reason/Detail/... - see
 * internal/portmap/portmap.go's json tags) - exactly the contract this page
 * was already built against, unchanged.
 *
 * The 404 branch below is kept anyway rather than deleted now that the route
 * is confirmed live: a build that ships without internal/portmap compiled in
 * (or a future refactor that moves the route) should still tell the truth
 * instead of rendering the generic failure path with a confusing "not
 * found" sentence. The honest three-way result (confirmed / unconfirmed /
 * failed) is portmap.Outcome's own vocabulary, not invented for this page.
 *
 * Registered in settings/registry.tsx (PAGES + ICONS) and in
 * internal/api/routes_features.go's featurePages()/featureList() - both
 * halves, per that file's own doc comment on why a page on only one side is
 * exactly the gap Wave 11's review already had to fix once for Scripts.
 */

interface TorrentSettings {
  seedRatioTarget: number;
  seedDurationSeconds: number;
  uploadLimitKiBs: number;
  port: number;
  dhtEnabled: boolean;
  pexEnabled: boolean;
}

// Frontend-only fallback for the rare case an older server has not sent
// `torrent` at all (mirrors Reconnect.tsx's DEFAULTS) - every real response
// carries these fields, so this is normally never seen. Mirrors
// settings.defaultTorrent() (internal/settings/settings_torrent.go)
// verbatim, including its own reasoning for each number: 1.0 and 7200s (two
// hours) are gopeed's own built-in defaults, not a KnightLoader opinion, and
// zero elsewhere means unlimited/unset, the same convention every other
// zero in this settings document already uses.
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
 * Strings this page needs, not yet in en.ts - locale files are 11.5F's lane,
 * landing after 11.5B-E (this page included). Same arrangement as every
 * other settings page's own PENDING table (System.tsx, Scripts.tsx,
 * Schedule.tsx, Connections.tsx): cx() asks the real catalogue first, so the
 * day these keys land in en.ts this table stops being consulted.
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
  'settings.torrents.engineNote':
    'Seed ratio, seed duration and port now reach every torrent this engine starts - port only for the very first one since this instance’s last restart, because the engine’s own torrent client is built once and never rebuilt afterwards; a later save is still stored correctly and takes effect from the next restart on. Upload limit is still saved and validated only, with nowhere yet for the engine to carry it into a running download. DHT and PEX below are the same story for an ORDINARY torrent: this instance’s own default does not yet reach a running download either, so a torrent still seeds with both on regardless of what is set here - a PRIVATE torrent is a different case entirely, see the note further down. The mapping button further down still does a real thing: it asks the router to forward the port number typed above, honestly, whether or not a torrent is actually listening on it yet.',
} as const;

type PendingKey = keyof typeof PENDING;
type Cx = (key: PendingKey, vars?: Record<string, string | number>) => string;

function useCx(): Cx {
  const { t } = useT();
  return useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      // The cast is the whole point: these keys are not in the union yet. It is
      // narrow - only keys in PENDING can be passed - and it goes with the table.
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
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
      // Same shape as Reconnect.tsx's own write(): patch's real signature is
      // Partial<Settings>, which does not name `torrent` either - see the
      // file doc comment above for why that is Reconnect.tsx's gap too, not
      // a new one.
      patch({ torrent: next } as unknown as Parameters<typeof patch>[0]);
    },
    [cfg, patch],
  );

  // Stored and sent as seconds (settings.Torrent.SeedDurationSeconds), shown
  // in hours - the same "store the wire unit, show the readable one" split
  // DownloadsSettings.tsx already does for speedLimit (bytes/s stored, KB/s
  // shown). A seeding target measured in seconds is not a number anyone
  // types on purpose.
  const seedHours = Math.round(tr.seedDurationSeconds / 3600);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={cx('settings.torrents.title')} subtitle={cx('settings.torrents.subtitle')} />

      {/* Verified while building this page, not a hedge: settings_torrent.go's
          own doc comments confirm SeedRatioTarget/SeedDurationSeconds/Port now
          reach internal/engine (Engine.SetTorrentConfig, called from
          internal/app's afterSettingsChange on every save and once at boot),
          while UploadLimitKiBs and the DHTEnabled/PEXEnabled ORDINARY-torrent
          default still do not - gopeed's own public surface has nowhere for
          the first, and nowhere for the second's non-private case either (see
          settings_torrent.go's DHTEnabled doc comment for exactly why that is
          a different, still-open gap from the private-torrent case
          privateNote below describes). A page that let the fields here look
          uniformly wired, or uniformly not, would be the exact "looked
          correct on paper" shape this codebase's own history names as the
          mistake to avoid - so it says which is which, once, here, rather
          than nowhere. */}
      <div className="glim-well px-3 py-2.5 text-[11px] text-statusWarn">
        {cx('settings.torrents.engineNote')}
      </div>

      <SectionTitle>{cx('settings.torrents.seedingTitle')}</SectionTitle>
      <Card className="flex flex-col gap-5">
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

      <SectionTitle>{cx('settings.torrents.transferTitle')}</SectionTitle>
      <Card className="flex flex-col gap-5">
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

      <SectionTitle>{cx('settings.torrents.portTitle')}</SectionTitle>
      <Card className="flex flex-col gap-5">
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

      <SectionTitle>{cx('settings.torrents.networkTitle')}</SectionTitle>
      <Card className="flex flex-col gap-4">
        <FieldGroup label={cx('settings.torrents.dht')} hint={cx('settings.torrents.dhtHint')}>
          <Toggle
            checked={tr.dhtEnabled}
            onChange={(v) => write({ dhtEnabled: v })}
            label={cx('settings.torrents.dht')}
            hideLabel
          />
        </FieldGroup>
        <FieldGroup label={cx('settings.torrents.pex')} hint={cx('settings.torrents.pexHint')}>
          <Toggle
            checked={tr.pexEnabled}
            onChange={(v) => write({ pexEnabled: v })}
            label={cx('settings.torrents.pex')}
            hideLabel
          />
        </FieldGroup>
        {/* A VISIBLE note, deliberately not behind an (i): this is the one
            fact on the page that overrides what the two switches above say,
            and a caveat that changes the switches' own meaning does not
            belong one hover away from them. */}
        <p className="text-[11px] text-carbon-textMuted">{cx('settings.torrents.privateNote')}</p>
      </Card>
    </div>
  );
}

type PortMapOutcome = 'confirmed' | 'unconfirmed' | 'failed';

/**
 * Mirrors internal/portmap.Result (internal/portmap/portmap.go) field for
 * field, including its own json tags - confirmed against the real, running
 * POST /api/torrents/portmap response, not read off the struct alone. See
 * the file doc comment above.
 */
interface PortMapResult {
  outcome: PortMapOutcome;
  reason: string;
  detail?: string;
  gateway?: string;
  internalPort?: number;
  externalPort?: number;
}

/**
 * The one-off action button, shaped after Reconnect.tsx's own "Run it now" -
 * a plain async click handler with its own busy/result state, not
 * useResource (which loads on mount; nothing here should run before the
 * button is pressed) and not part of the draft (a mapping attempt is not a
 * setting to save, it is a thing that just happened).
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
      // Its own named state, not the generic catch below: a 404 here means
      // "11.5C has not landed in this build (or landed at a different
      // route)", which is a true, useful thing to say and not the same
      // sentence as "the router refused the mapping".
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
        <Button kind="secondary" disabled={disabled} onClick={() => void attempt()}>
          {busy ? cx('settings.torrents.portMapping') : cx('settings.torrents.portMapButton')}
        </Button>
        <span className="text-[11px] text-carbon-textMuted">{cx('settings.torrents.portMapHint')}</span>
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
