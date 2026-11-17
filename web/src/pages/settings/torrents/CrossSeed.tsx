import { useCallback, useEffect, useState } from 'react';
import {
  Button,
  Card,
  IconBadge,
  LabelBadge,
  SectionTitle,
  TextInput,
  ToggleRow,
  type ButtonVerdict,
} from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { TestButton } from '../../../components/TestButton';
import { fmtDate } from '../../../lib/format';
import { useT } from '../../../lib/i18n';
import type { TranslationKey } from '../../../lib/locales/en';
import { IconAdd, IconCheck, IconRetry, IconTrash } from '../../../lib/icons';
import { RowRefusal, SettingPathInput, SettingRow, SubCard, SubHead } from '../controls';

/** Mirrors settings.GazelleSite. */
interface GazelleSite {
  apiKey: string;
  announce: string;
}

/** Mirrors settings.TorznabIndexer. */
interface TorznabIndexer {
  name: string;
  url: string;
  apiKey: string;
}

export type LinkMode = 'none' | 'hardlink' | 'symlink' | 'reflink';

/** Mirrors settings.CrossSeed. */
export interface CrossSeedSettings {
  enabled: boolean;
  red: GazelleSite;
  ops: GazelleSite;
  // The server sends null for an empty list.
  indexers: TorznabIndexer[] | null;
  linkMode: LinkMode | '';
  linkDir: string;
  category: string;
  daemon: { url: string; apiKey: string; folder: string };
}

export const CROSS_SEED_DEFAULTS: CrossSeedSettings = {
  enabled: false,
  red: { apiKey: '', announce: '' },
  ops: { apiKey: '', announce: '' },
  indexers: [],
  linkMode: 'none',
  linkDir: '',
  category: '',
  daemon: { url: '', apiKey: '', folder: '' },
};

/** The mask for a stored secret, as reconnect.RedactedPassword has it. */
const REDACTED = '********';

const LINK_MODES: LinkMode[] = ['none', 'hardlink', 'symlink', 'reflink'];

/** What /api/torrents/crossseed/test answers. */
interface TestAnswer {
  ok?: boolean;
  account?: string;
  error?: string;
}

/** Mirrors crossseed.Entry. */
interface LogEntry {
  time: string;
  tracker: string;
  name: string;
  infoHash: string;
  result: 'injected' | 'refused' | 'error' | 'handed';
  reason?: string;
}

const RESULT_TONE: Record<LogEntry['result'], 'ok' | 'warn' | 'fail' | undefined> = {
  injected: 'ok',
  handed: 'ok',
  refused: 'warn',
  error: 'fail',
};

const LOG_LINES = 20;

/**
 * CrossSeedCard sets up cross-seeding: the two Gazelle sites, the Torznab
 * indexers, how a twin reaches the files and the user's own daemon, with the
 * log of what came of it. Every secret comes back masked: its box stays
 * empty and the draft keeps the mask, which the save merges back.
 */
export function CrossSeedCard({
  hue,
  c,
  onChange,
}: {
  hue: number;
  c: CrossSeedSettings;
  onChange: (fields: Partial<CrossSeedSettings>) => void;
}) {
  const { t } = useT();
  const indexers = c.indexers ?? [];
  const linked = c.linkMode !== '' && c.linkMode !== 'none';
  return (
    <Card hue={hue} data-new="cross-seed" className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.torrents.crossSeedHint')}>{t('settings.torrents.crossSeedTitle')}</SectionTitle>
      <ToggleRow
        checked={c.enabled}
        onChange={(enabled) => onChange({ enabled })}
        label={t('settings.torrents.crossSeedOn')}
        hint={t('settings.torrents.crossSeedOnHint')}
      />
      {c.enabled && (
        <>
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <SiteFields site="red" other="OPS" c={c} onChange={onChange} />
            <SiteFields site="ops" other="RED" c={c} onChange={onChange} />
          </div>

          <div className="flex flex-col gap-4">
            <SubHead
              label={t('settings.torrents.crossSeedIndexers')}
              hint={t('settings.torrents.crossSeedIndexersHint')}
              right={
                <Button
                  kind="secondary"
                  icon={<IconAdd width={16} height={16} />}
                  onClick={() => onChange({ indexers: [...indexers, { name: '', url: '', apiKey: '' }] })}
                >
                  {t('settings.torrents.crossSeedIndexerAdd')}
                </Button>
              }
            />
            {indexers.length > 0 && (
              <ul className="flex flex-col gap-4">
                {indexers.map((x, i) => (
                  <IndexerRow
                    key={i}
                    index={i}
                    row={x}
                    c={c}
                    onChange={(fields) =>
                      onChange({ indexers: indexers.map((y, j) => (j === i ? { ...y, ...fields } : y)) })
                    }
                    onRemove={() => onChange({ indexers: indexers.filter((_, j) => j !== i) })}
                  />
                ))}
              </ul>
            )}
          </div>

          <div className="flex flex-col gap-4">
            <SettingRow
              label={t('settings.torrents.crossSeedLinkMode')}
              hint={t('settings.torrents.crossSeedLinkModeHint')}
            >
              <Tabs
                variant="well"
                size="sm"
                inline
                hug
                label={t('settings.torrents.crossSeedLinkMode')}
                active={c.linkMode || 'none'}
                onSelect={(linkMode) => onChange({ linkMode: linkMode as LinkMode })}
                items={LINK_MODES.map((m) => ({
                  id: m,
                  label: t(`settings.torrents.crossSeedLink.${m}` as TranslationKey),
                }))}
              />
            </SettingRow>
            {linked && (
              <SettingRow
                control="long"
                label={t('settings.torrents.crossSeedLinkDir')}
                hint={t('settings.torrents.crossSeedLinkDirHint')}
              >
                <SettingPathInput
                  field="torrent.crossSeed.linkDir"
                  value={c.linkDir}
                  title={t('settings.torrents.crossSeedLinkDir')}
                  onValue={(linkDir) => onChange({ linkDir })}
                />
              </SettingRow>
            )}
            <SettingRow
              control="long"
              label={t('settings.torrents.crossSeedCategory')}
              hint={t('settings.torrents.crossSeedCategoryHint')}
            >
              <TextInput
                dir="ltr"
                spellCheck={false}
                value={c.category}
                onChange={(e) => onChange({ category: e.target.value })}
              />
            </SettingRow>
          </div>

          <DaemonFields c={c} onChange={onChange} />
          <CrossSeedLog />
        </>
      )}
    </Card>
  );
}

/** SecretInput is a masked secret's box: empty while it holds the stored value. */
function SecretInput({
  value,
  onValue,
  placeholder,
}: {
  value: string;
  onValue: (v: string) => void;
  placeholder?: string;
}) {
  const { t } = useT();
  return (
    <TextInput
      type="password"
      dir="ltr"
      autoComplete="off"
      spellCheck={false}
      value={value === REDACTED ? '' : value}
      placeholder={value === REDACTED ? t('settings.torrents.crossSeedStored') : placeholder}
      onChange={(e) => onValue(e.target.value)}
    />
  );
}

function SiteFields({
  site,
  other,
  c,
  onChange,
}: {
  site: 'red' | 'ops';
  other: string;
  c: CrossSeedSettings;
  onChange: (fields: Partial<CrossSeedSettings>) => void;
}) {
  const { t } = useT();
  const acc = c[site];
  const name = site.toUpperCase();
  const set = (fields: Partial<GazelleSite>) => onChange({ [site]: { ...acc, ...fields } });
  return (
    <SubCard label={name} hint={t('settings.torrents.crossSeedGazelleHint', { site: name, other })}>
      <div className="flex flex-col gap-4">
        <SettingRow
          control="long"
          label={t('settings.torrents.crossSeedApiKey')}
          hint={t('settings.torrents.crossSeedApiKeyHint')}
        >
          <SecretInput value={acc.apiKey} onValue={(apiKey) => set({ apiKey })} />
        </SettingRow>
        <div className="flex flex-col gap-1.5">
          <SettingRow
            control="long"
            label={t('settings.torrents.crossSeedAnnounce')}
            hint={t('settings.torrents.crossSeedAnnounceHint')}
          >
            <SecretInput value={acc.announce} onValue={(announce) => set({ announce })} />
          </SettingRow>
          <RowRefusal field={`torrent.crossSeed.${site}.announce`} className="pb-0" />
        </div>
      </div>
      <CrossSeedTest
        what={site}
        c={c}
        label={t('settings.torrents.crossSeedTestKey')}
        hint={t('settings.torrents.crossSeedTestKeyHint', { site: name })}
        disabled={acc.apiKey === ''}
      />
    </SubCard>
  );
}

function IndexerRow({
  index,
  row,
  c,
  onChange,
  onRemove,
}: {
  index: number;
  row: TorznabIndexer;
  c: CrossSeedSettings;
  onChange: (fields: Partial<TorznabIndexer>) => void;
  onRemove: () => void;
}) {
  const { t } = useT();
  return (
    <li>
      <SubCard
        label={row.name.trim() || t('settings.torrents.crossSeedIndexer', { n: index + 1 })}
        right={
          <IconBadge
            labelled
            icon={<IconTrash width={16} height={16} />}
            hue={index}
            title={t('settings.torrents.crossSeedIndexerRemove')}
            aria-label={t('settings.torrents.crossSeedIndexerRemove')}
            onClick={onRemove}
          />
        }
      >
        <div className="flex flex-col gap-4">
          <SettingRow
            control="long"
            label={t('settings.torrents.crossSeedIndexerName')}
            hint={t('settings.torrents.crossSeedIndexerNameHint')}
          >
            <TextInput spellCheck={false} value={row.name} onChange={(e) => onChange({ name: e.target.value })} />
          </SettingRow>
          <div className="flex flex-col gap-1.5">
            <SettingRow
              control="long"
              label={t('settings.torrents.crossSeedIndexerUrl')}
              hint={t('settings.torrents.crossSeedIndexerUrlHint')}
            >
              <TextInput
                type="url"
                dir="ltr"
                spellCheck={false}
                placeholder="http://prowlarr:9696/1/api"
                value={row.url}
                onChange={(e) => onChange({ url: e.target.value })}
              />
            </SettingRow>
            <RowRefusal field={`torrent.crossSeed.indexers.${index}`} className="pb-0" />
          </div>
          <SettingRow
            control="long"
            label={t('settings.torrents.crossSeedApiKey')}
            hint={t('settings.torrents.crossSeedIndexerKeyHint')}
          >
            <SecretInput value={row.apiKey} onValue={(apiKey) => onChange({ apiKey })} />
          </SettingRow>
        </div>
        <CrossSeedTest
          what="indexer"
          index={index}
          c={c}
          label={t('settings.torrents.crossSeedTestIndexer')}
          hint={t('settings.torrents.crossSeedTestIndexerHint')}
          disabled={row.url.trim() === ''}
        />
      </SubCard>
    </li>
  );
}

function DaemonFields({
  c,
  onChange,
}: {
  c: CrossSeedSettings;
  onChange: (fields: Partial<CrossSeedSettings>) => void;
}) {
  const { t } = useT();
  const set = (fields: Partial<CrossSeedSettings['daemon']>) => onChange({ daemon: { ...c.daemon, ...fields } });
  return (
    <SubCard label={t('settings.torrents.crossSeedDaemon')} hint={t('settings.torrents.crossSeedDaemonHint')}>
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <SettingRow
            control="long"
            label={t('settings.torrents.crossSeedDaemonUrl')}
            hint={t('settings.torrents.crossSeedDaemonUrlHint')}
          >
            <TextInput
              type="url"
              dir="ltr"
              spellCheck={false}
              placeholder="http://cross-seed:2468"
              value={c.daemon.url}
              onChange={(e) => set({ url: e.target.value })}
            />
          </SettingRow>
          <RowRefusal field="torrent.crossSeed.daemon.url" className="pb-0" />
        </div>
        <SettingRow
          control="long"
          label={t('settings.torrents.crossSeedApiKey')}
          hint={t('settings.torrents.crossSeedDaemonKeyHint')}
        >
          <SecretInput value={c.daemon.apiKey} onValue={(apiKey) => set({ apiKey })} />
        </SettingRow>
        <SettingRow
          control="long"
          label={t('settings.torrents.crossSeedDaemonFolder')}
          hint={t('settings.torrents.crossSeedDaemonFolderHint')}
        >
          <SettingPathInput
            field="torrent.crossSeed.daemon.folder"
            value={c.daemon.folder}
            title={t('settings.torrents.crossSeedDaemonFolder')}
            onValue={(folder) => set({ folder })}
          />
        </SettingRow>
      </div>
      <CrossSeedTest
        what="daemon"
        c={c}
        label={t('settings.torrents.crossSeedTestDaemon')}
        hint={t('settings.torrents.crossSeedTestDaemonHint')}
        disabled={c.daemon.url.trim() === ''}
      />
    </SubCard>
  );
}

/**
 * CrossSeedTest posts the settings exactly as edited; the server merges the
 * stored keys back as a save would, so the test covers what a save would
 * write.
 */
function CrossSeedTest({
  what,
  index = 0,
  c,
  label,
  hint,
  disabled,
}: {
  what: 'red' | 'ops' | 'indexer' | 'daemon';
  index?: number;
  c: CrossSeedSettings;
  label: string;
  hint: string;
  disabled: boolean;
}) {
  const { t } = useT();
  const [answer, setAnswer] = useState<TestAnswer | null>(null);
  const part = what === 'indexer' ? (c.indexers ?? [])[index] : what === 'daemon' ? c.daemon : c[what];

  async function run(): Promise<ButtonVerdict> {
    setAnswer(null);
    let got: TestAnswer;
    try {
      const r = await fetch('/api/torrents/crossseed/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ what, index, crossSeed: c }),
      });
      if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
      got = (await r.json()) as TestAnswer;
    } catch (e) {
      got = { error: String(e).replace(/^(Error|TypeError):\s*/, '') };
    }
    setAnswer(got);
    return got.ok ? 'ok' : 'fail';
  }

  return (
    <div className="flex flex-col gap-3">
      {answer?.ok && (
        <p className="text-xs text-carbon-textSub">
          {answer.account
            ? t('settings.torrents.crossSeedTestOkAccount', { account: answer.account })
            : t('settings.torrents.crossSeedTestOk')}
        </p>
      )}
      {answer?.error && (
        <p className="text-xs text-carbon-textSub">
          {t('settings.torrents.crossSeedTestFailed', { error: answer.error })}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <TestButton
          icon={<IconCheck />}
          label={label}
          busyLabel={t('settings.torrents.qbitTesting')}
          hint={hint}
          disabled={disabled}
          words={{ ok: t('test.passed'), fail: t('test.failed') }}
          run={run}
          resetKey={JSON.stringify(part)}
        />
      </div>
    </div>
  );
}

/** CrossSeedLog is the newest lines of cross-seed.jsonl, newest first. */
function CrossSeedLog() {
  const { t } = useT();
  const [entries, setEntries] = useState<LogEntry[] | null>(null);

  const load = useCallback(async () => {
    try {
      const r = await fetch(`/api/torrents/crossseed/log?limit=${LOG_LINES}`);
      if (!r.ok) return;
      setEntries(((await r.json()) as { entries: LogEntry[] }).entries);
    } catch {
      // An older server without the route shows no log.
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  if (entries === null) return null;
  return (
    <SubCard
      label={t('settings.torrents.crossSeedLog')}
      hint={t('settings.torrents.crossSeedLogHint')}
      right={
        <IconBadge
          labelled
          icon={<IconRetry width={16} height={16} />}
          title={t('settings.torrents.crossSeedLogRefresh')}
          aria-label={t('settings.torrents.crossSeedLogRefresh')}
          onClick={() => void load()}
        />
      }
    >
      {entries.length === 0 ? (
        <p className="text-sm text-carbon-textMuted">{t('settings.torrents.crossSeedLogEmpty')}</p>
      ) : (
        <ul className="flex flex-col divide-y divide-carbon-border">
          {entries.map((e, i) => (
            <li
              key={`${e.time}-${e.infoHash}-${i}`}
              className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2.5 first:pt-0 last:pb-0"
            >
              <LabelBadge
                label={t(`settings.torrents.crossSeedResult.${e.result}` as TranslationKey)}
                tone={RESULT_TONE[e.result]}
              />
              <span className="min-w-0 flex-1 truncate text-sm text-carbon-text" title={e.name}>
                {e.name}
              </span>
              <span className="glim-num w-full text-xs text-carbon-textMuted sm:w-auto sm:shrink-0">
                {e.tracker === 'daemon' ? t('settings.torrents.crossSeedDaemon') : e.tracker} · {fmtDate(e.time)}
              </span>
              {e.reason && <p className="w-full text-xs text-carbon-textSub first-letter:uppercase">{e.reason}</p>}
            </li>
          ))}
        </ul>
      )}
    </SubCard>
  );
}
