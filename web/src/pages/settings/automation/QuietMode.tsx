import { useCallback, useEffect, useState } from 'react';
import {
  Button,
  Card,
  IconBadge,
  Modal,
  NumberInput,
  SectionTitle,
  TextInput,
  ToggleRow,
  UnitNumberInput,
  type ButtonVerdict,
} from '../../../components/ui';
import { BrandPicker, type Brand } from '../../../components/BrandPicker';
import { Logo, Monogram } from '../../../components/HosterIcon';
import { TestButton } from '../../../components/TestButton';
import { IconAdd, IconBack, IconClose, IconTrash } from '../../../lib/icons';
import { withBase } from '../../../lib/basePath';
import { MEDIA_SERVERS } from '../../../lib/mediaServers';
import { RATE_UNITS } from '../../../lib/format';
import { useT, type TranslationKey } from '../../../lib/i18n';
import {
  connectWS,
  deletePlaybackServer,
  fetchPlayback,
  fetchQueue,
  REDACTED_HEADER,
  savePlaybackServer,
  setQueue,
  testPlaybackServer,
  type PlaybackFailure,
  type PlaybackKind,
  type PlaybackServer,
  type PlaybackSettings,
  type PlaybackStream,
  type PlaybackView,
  type QuietLimits,
  type QueueState,
} from '../../../lib/api';
import { useDraft } from '../context';
import { ModuleToggle } from '../ModuleToggle';
import { SettingRow } from '../controls';

// Quiet mode's second set of limits, and the media servers whose streams put
// it in force. The limits and the playback switches are ordinary settings in
// the draft; a server saves through its own route with its sealed token, as a
// media hook does, so the draft never carries the list.

const QUIET_DEFAULTS: QuietLimits = { speedLimit: 0, maxConcurrent: 0, uploadLimit: 0 };
const PLAYBACK_DEFAULTS: PlaybackSettings = {
  enabled: false,
  ignoreLocal: false,
  pausedMinutes: 30,
  holdExtraction: true,
};

const KIND_NAMES = Object.fromEntries(MEDIA_SERVERS.map((m) => [m.id, m.name])) as Record<PlaybackKind, string>;

const URL_PLACEHOLDERS: Record<PlaybackKind, string> = {
  plex: 'http://192.168.1.10:32400',
  jellyfin: 'http://192.168.1.10:8096',
  emby: 'http://192.168.1.10:8096',
};

const TOKEN_HINTS: Record<PlaybackKind, TranslationKey> = {
  plex: 'settings.playback.tokenHint.plex',
  jellyfin: 'settings.playback.tokenHint.jellyfin',
  emby: 'settings.playback.tokenHint.emby',
};

/** The failure codes; the ones the media hooks word alike borrow their sentence. */
const PROBLEM_KEYS: Record<string, TranslationKey> = {
  dns: 'settings.mediahook.problem.dns',
  refused: 'settings.mediahook.problem.refused',
  timeout: 'settings.mediahook.problem.timeout',
  tls: 'settings.mediahook.problem.tls',
  auth: 'settings.playback.problem.auth',
  notFound: 'settings.playback.problem.notFound',
  format: 'settings.playback.problem.format',
  redirect: 'settings.mediahook.problem.redirect',
  server: 'settings.mediahook.problem.server',
  proxy: 'settings.mediahook.problem.proxy',
  unknown: 'settings.mediahook.problem.unknown',
};

interface Draft {
  /** Empty for a new server. */
  id: string;
  name: string;
  /** Empty while the tiles are shown. */
  kind: PlaybackKind | '';
  url: string;
  token: string;
  /** True while the token box still holds the placeholder. */
  stored: boolean;
}

const draftFor = (s: PlaybackServer): Draft => ({
  id: s.id,
  name: s.name ?? '',
  kind: s.kind,
  url: s.url,
  // Never '' for a stored token, which would clear it.
  token: s.hasToken ? REDACTED_HEADER : '',
  stored: s.hasToken,
});

export function QuietModeCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const quiet = { ...QUIET_DEFAULTS, ...cfg.quiet };
  const playback = { ...PLAYBACK_DEFAULTS, ...cfg.playback };
  const setQuietLimits = (fields: Partial<QuietLimits>) => patch({ quiet: { ...quiet, ...fields } });
  const setPlayback = (fields: Partial<PlaybackSettings>) => patch({ playback: { ...playback, ...fields } });

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.quietmode.hint')}>{t('settings.quietmode.title')}</SectionTitle>
      <div className="flex flex-col gap-4">
        <SettingRow control="short" label={t('settings.quietmode.speedLimit')} hint={t('settings.quietmode.speedLimitHint')}>
          <UnitNumberInput
            value={quiet.speedLimit}
            units={RATE_UNITS}
            onValue={(speedLimit) => setQuietLimits({ speedLimit })}
          />
        </SettingRow>
        <SettingRow
          control="short"
          label={t('settings.quietmode.maxConcurrent')}
          hint={t('settings.quietmode.maxConcurrentHint')}
        >
          <NumberInput
            value={quiet.maxConcurrent}
            min={0}
            max={64}
            onValue={(maxConcurrent) => setQuietLimits({ maxConcurrent: Math.max(0, maxConcurrent) })}
          />
        </SettingRow>
        <SettingRow
          control="short"
          label={t('settings.quietmode.uploadLimit')}
          hint={t('settings.quietmode.uploadLimitHint')}
        >
          <UnitNumberInput
            value={quiet.uploadLimit}
            units={RATE_UNITS}
            onValue={(uploadLimit) => setQuietLimits({ uploadLimit })}
          />
        </SettingRow>
      </div>

      <ModuleToggle id="playback" hint={t('settings.modules.about.playback')}>
        <PlaybackPanel playback={playback} onPlayback={setPlayback} />
      </ModuleToggle>
    </Card>
  );
}

function PlaybackPanel({
  playback,
  onPlayback,
}: {
  playback: PlaybackSettings;
  onPlayback: (fields: Partial<PlaybackSettings>) => void;
}) {
  const { t } = useT();
  const [view, setView] = useState<PlaybackView | null>(null);
  const [queue, setQueueState] = useState<QueueState | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [confirming, setConfirming] = useState<PlaybackServer | null>(null);

  const reload = useCallback(() => {
    void fetchPlayback().then(setView, () => {
      /* the card keeps what it showed; the next tick tries again */
    });
  }, []);

  // The watcher asks every ten seconds, so the card does too. A queue event
  // means quiet mode moved, which a stream starting or ending is one cause of.
  useEffect(() => {
    reload();
    // Read once at the start, so a stream waived before the card opened does
    // not offer to be waived again until the first event arrives.
    void fetchQueue().then(setQueueState, () => undefined);
    const timer = window.setInterval(reload, 10_000);
    const stop = connectWS(
      (type, data) => {
        if (type !== 'queue') return;
        setQueueState(data as QueueState);
        reload();
      },
      ['queue'],
    );
    return () => {
      window.clearInterval(timer);
      stop();
    };
  }, [reload]);

  const run = async (work: () => Promise<PlaybackView | void>) => {
    setBusy(true);
    setError('');
    try {
      const next = await work();
      setView(next ?? (await fetchPlayback()));
      setDraft(null);
    } catch (e) {
      setError(String(e).replace(/^(Error|ApiError):\s*/, ''));
    } finally {
      setBusy(false);
    }
  };

  const save = () => {
    const kind = draft?.kind;
    if (!draft || !kind) return;
    void run(() =>
      savePlaybackServer({
        id: draft.id,
        name: draft.name.trim(),
        kind,
        url: draft.url.trim(),
        token: draft.token,
      }),
    );
  };

  const test = async (s: PlaybackServer): Promise<ButtonVerdict> => {
    setError('');
    try {
      const out = await testPlaybackServer(s.id);
      if (!out.ok && out.failure) setError(`${s.name || KIND_NAMES[s.kind]}: ${problem(out.failure)}`);
      return out.ok ? 'ok' : 'fail';
    } catch (e: unknown) {
      setError(String(e).replace(/^(Error|ApiError):\s*/, ''));
      return 'fail';
    }
  };

  const problem = (f: PlaybackFailure): string => {
    const key = PROBLEM_KEYS[f.code] ?? PROBLEM_KEYS.unknown;
    const kind = typeof f.params?.kind === 'string' ? KIND_NAMES[f.params.kind as PlaybackKind] : '';
    return t(key, { ...(f.params ?? {}), kind: kind ?? '', error: f.error ?? '' });
  };

  const stateLine = (s: PlaybackServer): string => {
    const row = view?.status.servers.find((x) => x.id === s.id);
    if (!row) return t('settings.playback.notAsked');
    if (row.ok) return t('settings.playback.answers');
    return row.failure ? problem(row.failure) : t('settings.playback.notAsked');
  };

  const kinds = view?.kinds ?? (['plex', 'jellyfin', 'emby'] as PlaybackKind[]);
  const servers = view?.servers ?? [];
  const streams = view?.status.streams ?? [];
  const active = view?.status.active === true;
  // Playing while quiet mode is off can only be a stream somebody waived.
  const waived = active && queue !== null && queue.quiet === false;

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-3">
          <p dir="auto" className="min-w-0 flex-1 text-sm text-carbon-textSub">
            {!active
              ? t('settings.playback.statusIdle')
              : waived
                ? t('settings.playback.statusWaived')
                : t('settings.playback.statusActive')}
          </p>
          {active && !waived && (
            <Button
              kind="secondary"
              title={t('settings.playback.waiveHint')}
              disabled={busy}
              onClick={() => void setQueue({ quiet: false }).then(setQueueState, () => undefined)}
            >
              {t('settings.playback.waive')}
            </Button>
          )}
        </div>
        {streams.length > 0 && (
          <ul className="flex flex-col">
            {streams.map((s, i) => (
              <StreamRow key={`${s.server}-${i}`} stream={s} last={i === streams.length - 1} />
            ))}
          </ul>
        )}
      </div>

      <div className="flex flex-col gap-3">
        {/* A row and not a SectionTitle, which would take the card's own label. */}
        <SettingRow label={t('settings.playback.serversTitle')} hint={t('settings.playback.serversHint')}>
          <Button
            kind="secondary"
            icon={<IconAdd width={16} height={16} />}
            disabled={busy}
            onClick={() => {
              setError('');
              setDraft({ id: '', name: '', kind: '', url: '', token: '', stored: false });
            }}
          >
            {t('settings.playback.add')}
          </Button>
        </SettingRow>
        {servers.length === 0 ? (
          <p className="py-4 text-center text-sm text-carbon-textSub">
            {t('settings.playback.empty')}
            <span className="mt-1 block text-meta text-carbon-textMuted">{t('settings.playback.emptyHint')}</span>
          </p>
        ) : (
          <ul className="flex flex-col">
            {servers.map((s, i) => (
              <li
                key={s.id}
                className={`flex flex-col gap-1 py-2.5 ${i === servers.length - 1 ? '' : 'border-b border-carbon-border/60'}`}
              >
                <div className="flex items-center gap-3">
                  <button
                    type="button"
                    aria-haspopup="dialog"
                    className="flex min-w-0 flex-1 items-center gap-3 text-start"
                    onClick={() => {
                      setError('');
                      setDraft(draftFor(s));
                    }}
                  >
                    <MediaMark kind={s.kind} size={20} />
                    <span className="shrink-0 text-sm text-carbon-text">{s.name || KIND_NAMES[s.kind]}</span>
                    <span dir="ltr" className="min-w-0 flex-1 truncate text-xs text-carbon-textSub">
                      {s.host}
                    </span>
                    <span className="shrink-0 text-meta text-carbon-textMuted">{KIND_NAMES[s.kind]}</span>
                  </button>
                  <TestButton
                    label={t('settings.playback.test')}
                    busyLabel={t('settings.playback.testRunning')}
                    words={{ ok: t('test.connected'), fail: t('test.failed') }}
                    disabled={busy}
                    run={() => test(s)}
                    resetKey={s.url}
                  />
                  <IconBadge
                    icon={<IconTrash width={16} height={16} />}
                    hue={i}
                    title={t('settings.playback.delete')}
                    aria-label={`${t('settings.playback.delete')} · ${s.name || KIND_NAMES[s.kind]}`}
                    disabled={busy}
                    onClick={() => setConfirming(s)}
                  />
                </div>
                <p dir="auto" className="text-meta text-carbon-textMuted">
                  {stateLine(s)}
                </p>
              </li>
            ))}
          </ul>
        )}
        {!draft && error && (
          <p dir="auto" className="text-xs text-statusWarn">
            {error}
          </p>
        )}
      </div>

      <div className="flex flex-col gap-4">
        <ToggleRow
          checked={playback.ignoreLocal}
          onChange={(ignoreLocal) => onPlayback({ ignoreLocal })}
          label={t('settings.playback.ignoreLocal')}
          hint={t('settings.playback.ignoreLocalHint')}
        />
        <SettingRow
          control="short"
          label={t('settings.playback.pausedMinutes')}
          hint={t('settings.playback.pausedMinutesHint')}
        >
          <NumberInput
            value={playback.pausedMinutes}
            min={0}
            max={1440}
            step={5}
            onValue={(pausedMinutes) => onPlayback({ pausedMinutes: Math.max(0, pausedMinutes) })}
          />
        </SettingRow>
        <ToggleRow
          checked={playback.holdExtraction}
          onChange={(holdExtraction) => onPlayback({ holdExtraction })}
          label={t('settings.playback.holdExtraction')}
          hint={t('settings.playback.holdExtractionHint')}
        />
      </div>

      {draft && (
        <ServerWindow
          draft={draft}
          kinds={kinds}
          busy={busy}
          error={error}
          onDraft={setDraft}
          onSave={save}
          onClose={() => (busy ? undefined : setDraft(null))}
        />
      )}

      {confirming && (
        <Modal
          title={t('settings.playback.delete')}
          onClose={() => setConfirming(null)}
          footer={
            <>
              <span className="flex-1" />
              <Button
                kind="ghost"
                labelled
                icon={<IconClose />}
                title={t('common.cancel')}
                disabled={busy}
                onClick={() => setConfirming(null)}
              />
              <Button
                kind="secondary"
                disabled={busy}
                onClick={() => {
                  const id = confirming.id;
                  setConfirming(null);
                  void run(async () => {
                    await deletePlaybackServer(id);
                  });
                }}
              >
                {t('settings.playback.delete')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-textSub">
            {t('settings.playback.deleteConfirm', { name: confirming.name || KIND_NAMES[confirming.kind] })}
          </p>
        </Modal>
      )}
    </div>
  );
}

function StreamRow({ stream, last }: { stream: PlaybackStream; last: boolean }) {
  const { t } = useT();
  const who = [stream.user, stream.player].filter(Boolean).join(' · ');
  const note = !stream.counts
    ? t(stream.reason === 'local' ? 'settings.playback.reason.local' : 'settings.playback.reason.paused')
    : stream.paused
      ? t('settings.playback.paused')
      : '';
  return (
    <li className={`flex items-center gap-3 py-2 ${last ? '' : 'border-b border-carbon-border/60'}`}>
      <span dir="auto" className="min-w-0 flex-1 truncate text-sm text-carbon-text">
        {stream.series ? `${stream.series}: ${stream.title}` : stream.title}
      </span>
      {who !== '' && (
        <span dir="auto" className="min-w-0 truncate text-xs text-carbon-textSub">
          {who}
        </span>
      )}
      {note !== '' && <span className="shrink-0 text-meta text-carbon-textMuted">{note}</span>}
    </li>
  );
}

const LOGOS = new Map(MEDIA_SERVERS.map((m) => [m.id, m.logo]));

/** MediaMark is a media server's logo from public/media; `lit` adds the mask its tile shows under the pointer. */
function MediaMark({ kind, size, lit }: { kind: PlaybackKind; size: number; lit?: boolean }) {
  const logo = LOGOS.get(kind);
  const box = { width: size, height: size };
  if (!logo) return <Monogram name={KIND_NAMES[kind] ?? kind} box={box} />;
  const src = withBase(`/media/${kind}.png`);
  const mask = logo.lit ? withBase(`/media/${kind}.lit.png`) : src;
  return <Logo src={src} mask={mask} rest={logo.rest} box={box} lit={lit} />;
}

/**
 * ServerWindow is where a media server is added, the way a VPN is: every kind
 * a tile, the form of the one picked in the same window. Opened on a stored
 * server, it is that server's form alone.
 */
function ServerWindow({
  draft,
  kinds,
  busy,
  error,
  onDraft,
  onSave,
  onClose,
}: {
  draft: Draft;
  kinds: PlaybackKind[];
  busy: boolean;
  error: string;
  onDraft: (next: Draft) => void;
  onSave: () => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const kind = draft.kind;
  if (kind === '') {
    const brands: Brand[] = kinds.map((k) => ({
      id: k,
      name: KIND_NAMES[k],
      mark: <MediaMark kind={k} size={25} lit />,
      paint: LOGOS.get(k),
    }));
    return (
      <Modal
        title={t('settings.playback.add')}
        hint={t('settings.playback.kindHint')}
        wide
        onClose={onClose}
        footer={
          <>
            <span className="flex-1" />
            <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          </>
        }
      >
        <BrandPicker brands={brands} none="" onPick={(id) => onDraft({ ...draft, kind: id as PlaybackKind })} />
      </Modal>
    );
  }
  return (
    <Modal
      title={draft.id ? draft.name || KIND_NAMES[kind] : KIND_NAMES[kind]}
      wide
      onClose={onClose}
      footer={
        <>
          {/* A new server's form leads back to the tiles; a stored one has none behind it. */}
          {draft.id ? (
            <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} disabled={busy} onClick={onClose} />
          ) : (
            <Button
              kind="secondary"
              labelled
              icon={<IconBack className="rtl:-scale-x-100" />}
              title={t('onboarding.back')}
              disabled={busy}
              onClick={() => onDraft({ ...draft, kind: '' })}
            />
          )}
          <span className="flex-1" />
          <Button disabled={busy || draft.url.trim() === ''} onClick={onSave}>
            {t('settings.playback.save')}
          </Button>
        </>
      }
    >
      <div data-new="picker-form" className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <MediaMark kind={kind} size={36} />
          <p className="text-sm text-carbon-textSub">{t(TOKEN_HINTS[kind])}</p>
        </div>
        <SettingRow control="long" label={t('settings.playback.name')} hint={t('settings.playback.nameHint')}>
          <TextInput
            dir="auto"
            value={draft.name}
            placeholder={KIND_NAMES[kind]}
            onChange={(e) => onDraft({ ...draft, name: e.target.value })}
          />
        </SettingRow>
        <SettingRow control="long" label={t('settings.playback.url')} hint={t('settings.playback.urlHint')}>
          <TextInput
            dir="ltr"
            spellCheck={false}
            value={draft.url}
            placeholder={URL_PLACEHOLDERS[kind]}
            onChange={(e) => onDraft({ ...draft, url: e.target.value })}
          />
        </SettingRow>
        <SettingRow
          control="long"
          label={draft.stored ? t('settings.mediahook.valueStored') : t('settings.playback.token')}
          hint={draft.stored ? t('settings.mediahook.valueStoredHint') : t(TOKEN_HINTS[kind])}
        >
          <TextInput
            dir="ltr"
            spellCheck={false}
            value={draft.token}
            onChange={(e) => onDraft({ ...draft, token: e.target.value, stored: false })}
          />
        </SettingRow>
        {error && (
          <p dir="auto" className="text-xs text-statusWarn">
            {error}
          </p>
        )}
      </div>
    </Modal>
  );
}
