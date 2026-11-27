import { useEffect, useState, type ReactNode } from 'react';
import { Modal, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { errorText } from '../api/client';
import type { ServerConnection } from '../api/types';
import {
  AUDIO_FORMATS,
  DEFAULT_INTERVAL_MINUTES,
  DEFAULT_MEDIA,
  EXTRAS,
  MARK_ONLY,
  QUALITIES,
  SPONSOR_CATEGORIES,
  SubscriptionFault,
  VIDEO_FORMATS,
  clampInterval,
  healthText,
  kindOf,
  lengthText,
  mediaWhat,
  nameOf,
  removeSubscription,
  rowFaults,
  saveRefusal,
  saveSubscription,
  skipText,
  sponsorChoice,
  testSubscription,
  withExtra,
  withSponsor,
  withWhat,
  type Backfill,
  type FaultField,
  type FeedHealth,
  type FeedTest,
  type Kind,
  type MediaChoice,
  type MediathekQuery,
  type SponsorBlock,
  type SponsorChoice,
  type Subscription,
} from '../api/subscriptions';
import { fetchDownloadFolders, fetchPriorities, type PriorityChoice } from '../api/taskEdit';
import { useT, type TranslationKey } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { TYPE, type Palette } from '../theme/tokens';
import { ConfirmDialog } from './ConfirmDialog';
import { GlimButton, GlimRow, GlimToggle, NotchCard, WellSelector } from './glim';
import IconBadge, { Back, Check, Cross, Search, Trash } from './IconBadge';
import { MovingScroll, NoArrival } from './Moving';
import { EditBox, EditField, PickChips } from './TaskEdit';
import { Text } from './Text';

export interface OpenSubscription {
  /** The address the row is stored under, null for a row not saved yet. */
  was: string | null;
  row: Subscription;
}

const NO_PRIORITY = 'unset';
const EMPTY_QUERY: MediathekQuery = { channel: '', topic: '', title: '', quality: 'hd', subtitles: false };
const BACKFILL_START: Record<'count' | 'days', number> = { count: 20, days: 30 };

/** digits reads a number box, where anything but a whole number is nothing. */
const digits = (v: string) => Number.parseInt(v.replace(/\D/g, ''), 10) || 0;
const shown = (n: number | undefined) => (n ? String(n) : '');

/**
 * The editor of one subscription, the phone's side of the web's subscription
 * window. Every field of the row is here, grouped as the web groups them, and
 * the test runs on the form as it stands, saved or not.
 *
 * Nothing is written until Save, and a save writes this row alone, so an edit
 * on the phone and one in a browser never take each other's rows away.
 */
export function SubscriptionEdit({
  conn,
  open,
  health,
  onClose,
  onSaved,
}: {
  conn: ServerConnection;
  open: OpenSubscription | null;
  health?: FeedHealth;
  onClose: () => void;
  /** The list as the instance stored it, after a save or a removal. */
  onSaved: (rows: Subscription[]) => void;
}) {
  const { t, lang } = useT();
  const { c, corners } = useAppearance();
  const { motion } = useMotion();
  const insets = useSafeAreaInsets();
  const [row, setRow] = useState<Subscription | null>(open?.row ?? null);
  const [folders, setFolders] = useState<string[]>([]);
  const [priorities, setPriorities] = useState<PriorityChoice[]>([]);
  const [faults, setFaults] = useState<Partial<Record<FaultField | 'dir', string>>>({});
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [testing, setTesting] = useState(false);
  const [result, setResult] = useState<FeedTest | null>(null);
  const [testError, setTestError] = useState('');
  const [removing, setRemoving] = useState(false);

  useEffect(() => {
    setRow(open?.row ?? null);
    setFaults({});
    setError('');
    setResult(null);
    setTestError('');
    if (!open) return;
    let alive = true;
    void fetchDownloadFolders(conn).then((f) => alive && setFolders(f));
    void fetchPriorities(conn).then((p) => alive && setPriorities(p));
    return () => {
      alive = false;
    };
  }, [open, conn]);

  if (!open || !row) return null;

  const set = (patch: Partial<Subscription>) => {
    setRow((r) => (r ? { ...r, ...patch } : r));
    setFaults({});
    setError('');
  };
  const kind = kindOf(row);
  const stored = open.was !== null;
  const status = stored ? healthText(t, lang, health) : null;

  // The field a refusal names can be a long way up the form, so the reason
  // stands beside Save as well.
  const refuse = (field: FaultField | 'dir', text: string) => {
    setFaults({ [field]: text });
    setError(text);
  };

  const save = async () => {
    const found = rowFaults(row);
    if (found.length > 0) {
      setFaults(Object.fromEntries(found.map((f) => [f.field, t(f.key)])));
      setError(t(found[0].key));
      return;
    }
    setBusy(true);
    setError('');
    try {
      onSaved(await saveSubscription(conn, open.was, row));
    } catch (e) {
      setBusy(false);
      if (e instanceof SubscriptionFault) {
        refuse('url', t(e.key));
        return;
      }
      const said = saveRefusal(t, e, row);
      if (said.field === 'dir') refuse('dir', said.text);
      else setError(said.text);
      return;
    }
    setBusy(false);
    onClose();
  };

  const runTest = async () => {
    setTesting(true);
    setResult(null);
    setTestError('');
    try {
      setResult(await testSubscription(conn, row));
    } catch (e) {
      setTestError(errorText(t, e));
    }
    setTesting(false);
  };

  const remove = async () => {
    setRemoving(false);
    if (open.was === null) return;
    setBusy(true);
    try {
      onSaved(await removeSubscription(conn, open.was));
    } catch (e) {
      setBusy(false);
      setError(errorText(t, e));
      return;
    }
    setBusy(false);
    onClose();
  };

  const fault = (f: FaultField | 'dir') =>
    faults[f] ? <Text style={[styles.fault, { color: c.statusFailSolid }]}>{faults[f]}</Text> : null;

  const mediathek = row.mediathek ?? EMPTY_QUERY;
  const setQuery = (patch: Partial<MediathekQuery>) => set({ mediathek: { ...mediathek, ...patch } });

  return (
    <Modal visible animationType={motion === 'off' ? 'none' : 'slide'} onRequestClose={onClose}>
      <NoArrival>
        <View style={[styles.page, { backgroundColor: c.bg }]}>
          <View style={styles.topBar}>
            <IconBadge icon={<Back color={c.textSub} />} onPress={onClose} accessibilityLabel={t('subs.cancel')} />
            <View style={styles.titles}>
              <Text style={[styles.title, { color: c.text }]} numberOfLines={1}>
                {stored ? nameOf(row) : t('subs.new')}
              </Text>
              {status ? (
                <Text style={[styles.status, { color: status.trouble ? c.statusWarnText : c.textMuted }]} numberOfLines={2}>
                  {status.text}
                </Text>
              ) : null}
            </View>
          </View>

          {/* The window is drawn edge to edge, so the form keeps the
              navigation bar's height free at its end. */}
          <MovingScroll
            contentContainerStyle={[styles.body, { paddingBottom: 32 + insets.bottom }]}
            keyboardShouldPersistTaps="handled"
          >
            <NotchCard title={t('subs.what')} hue={0}>
              <EditField label={t('subs.name')} info={t('subs.nameHint')}>
                <EditBox value={row.title ?? ''} onChange={(title) => set({ title })} label={t('subs.name')} />
              </EditField>
              <EditField label={t('subs.kind')} info={t('subs.kindHint')}>
                <WellSelector<Kind>
                  options={[
                    { value: 'rss', label: t('subs.kind.rss') },
                    { value: 'channel', label: t('subs.kind.channel') },
                    { value: 'mediathek', label: t('subs.kind.mediathek') },
                  ]}
                  value={kind}
                  hue={0}
                  onPick={(k) => set({ kind: k, mediathek: k === 'mediathek' ? mediathek : row.mediathek })}
                />
              </EditField>
              <EditField label={t('subs.url')} info={stored ? t('subs.urlStoredHint') : t('subs.urlHint')}>
                {stored ? (
                  <Text style={[styles.address, { color: c.textSub }]} selectable>
                    {row.url}
                  </Text>
                ) : (
                  <EditBox value={row.url} onChange={(url) => set({ url })} label={t('subs.url')} placeholder="https://" keyboard="url" />
                )}
                {fault('url')}
              </EditField>
              <EditField label={t('subs.interval')} info={t('subs.intervalHint')}>
                <EditBox
                  value={shown(row.intervalMinutes)}
                  onChange={(v) => set({ intervalMinutes: clampInterval(digits(v)) })}
                  label={t('subs.interval')}
                  placeholder={String(DEFAULT_INTERVAL_MINUTES)}
                  keyboard="number-pad"
                />
              </EditField>
            </NotchCard>

            {kind === 'mediathek' ? (
              <NotchCard title={t('subs.mediathek')} hue={1} info={t('subs.mediathekHint')}>
                <EditField label={t('subs.mtChannel')} info={t('subs.mtChannelHint')}>
                  <EditBox value={mediathek.channel} onChange={(channel) => setQuery({ channel })} label={t('subs.mtChannel')} />
                </EditField>
                <EditField label={t('subs.mtTopic')}>
                  <EditBox value={mediathek.topic} onChange={(topic) => setQuery({ topic })} label={t('subs.mtTopic')} />
                </EditField>
                <EditField label={t('subs.mtTitle')}>
                  <EditBox value={mediathek.title} onChange={(title) => setQuery({ title })} label={t('subs.mtTitle')} />
                </EditField>
                {fault('mediathek')}
                <EditField label={t('subs.mtQuality')} info={t('subs.mtQualityHint')}>
                  <WellSelector<MediathekQuery['quality']>
                    options={[
                      { value: 'hd', label: t('subs.mtQuality.hd') },
                      { value: 'normal', label: t('subs.mtQuality.normal') },
                      { value: 'low', label: t('subs.mtQuality.low') },
                    ]}
                    value={mediathek.quality}
                    hue={1}
                    onPick={(quality) => setQuery({ quality })}
                  />
                </EditField>
                <GlimRow
                  label={t('subs.mtSubtitles')}
                  info={t('subs.mtSubtitlesHint')}
                  control={
                    <GlimToggle
                      label={t('subs.mtSubtitles')}
                      value={mediathek.subtitles}
                      onChange={(subtitles) => setQuery({ subtitles })}
                    />
                  }
                />
              </NotchCard>
            ) : null}

            <NotchCard title={t('subs.entries')} hue={2}>
              <EditField label={t('subs.include')} info={t('subs.includeHint')}>
                <EditBox value={row.titleFilter ?? ''} onChange={(titleFilter) => set({ titleFilter })} label={t('subs.include')} />
                {fault('titleFilter')}
              </EditField>
              <EditField label={t('subs.exclude')} info={t('subs.excludeHint')}>
                <EditBox value={row.excludeFilter ?? ''} onChange={(excludeFilter) => set({ excludeFilter })} label={t('subs.exclude')} />
                {fault('excludeFilter')}
              </EditField>
              <View style={styles.pair}>
                <View style={styles.half}>
                  <EditField label={t('subs.minMinutes')} info={t('subs.lengthHint')}>
                    <EditBox
                      value={shown(row.minMinutes)}
                      onChange={(v) => set({ minMinutes: digits(v) })}
                      label={t('subs.minMinutes')}
                      keyboard="number-pad"
                    />
                  </EditField>
                </View>
                <View style={styles.half}>
                  <EditField label={t('subs.maxMinutes')}>
                    <EditBox
                      value={shown(row.maxMinutes)}
                      onChange={(v) => set({ maxMinutes: digits(v) })}
                      label={t('subs.maxMinutes')}
                      keyboard="number-pad"
                    />
                  </EditField>
                </View>
              </View>
              {fault('length')}
              {kind === 'channel' ? backfillFields(t, row.backfill ?? null, (backfill) => set({ backfill })) : null}
              {fault('backfill')}
            </NotchCard>

            <NotchCard title={t('subs.where')} hue={3}>
              <EditField label={t('subs.folder')} info={t('subs.folderHint')}>
                <EditBox value={row.dir ?? ''} onChange={(dir) => set({ dir })} label={t('subs.folder')} />
                <PickChips values={folders} chosen={row.dir ?? ''} onPick={(dir) => set({ dir })} />
                {fault('dir')}
              </EditField>
              {priorities.length > 0 ? (
                <EditField label={t('subs.priority')} info={t('subs.priorityHint')}>
                  <WellSelector
                    options={[
                      { value: NO_PRIORITY, label: t('subs.priorityNone') },
                      ...priorities.map((p) => ({ value: String(p.value), label: t(`edit.priority.${p.id}` as TranslationKey) })),
                    ]}
                    // Matched on absence, since 0 is a priority.
                    value={row.priority === undefined || row.priority === null ? NO_PRIORITY : String(row.priority)}
                    hue={2}
                    onPick={(v) => set({ priority: v === NO_PRIORITY ? null : Number(v) })}
                  />
                </EditField>
              ) : null}
              <EditField label={t('subs.naming')} info={t('subs.namingHint')}>
                <WellSelector
                  options={[
                    { value: 'entry', label: t('subs.naming.entry') },
                    { value: 'channel', label: t('subs.naming.channel') },
                  ]}
                  value={row.naming || 'entry'}
                  hue={3}
                  onPick={(v) => set({ naming: v === 'channel' ? 'channel' : '' })}
                />
              </EditField>
              <EditField label={t('subs.library')} info={t('subs.libraryHint')}>
                <WellSelector
                  options={[
                    { value: 'default', label: t('subs.library.default') },
                    { value: 'series', label: t('subs.library.series') },
                  ]}
                  value={row.library || 'default'}
                  hue={4}
                  onPick={(v) => set({ library: v === 'series' ? 'series' : '' })}
                />
              </EditField>
              <GlimRow
                label={t('subs.autoStart')}
                info={t('subs.autoStartHint')}
                control={
                  <GlimToggle hue={0} label={t('subs.autoStart')} value={!!row.autoStart} onChange={(autoStart) => set({ autoStart })} />
                }
              />
              <GlimRow
                label={t('subs.allowDownloaded')}
                info={t('subs.allowDownloadedHint')}
                control={
                  <GlimToggle
                    hue={1}
                    label={t('subs.allowDownloaded')}
                    value={!!row.allowDownloaded}
                    onChange={(allowDownloaded) => set({ allowDownloaded })}
                  />
                }
              />
            </NotchCard>

            {/* A Mediathek search stages the files themselves, so the yt-dlp
                choices would have nothing to act on. */}
            {kind !== 'mediathek' ? (
              <>
                <NotchCard title={t('subs.video')} hue={4} info={t('subs.videoHint')}>
                  {mediaFields(t, row.media ?? null, (media) => set({ media }))}
                </NotchCard>
                <NotchCard title={t('subs.sponsorBlockCard')} hue={5} info={t('subs.sponsorBlockHint')}>
                  {sponsorFields(t, row.sponsorBlock ?? null, (sponsorBlock) => set({ sponsorBlock }))}
                </NotchCard>
              </>
            ) : null}

            <NotchCard title={t('subs.testCard')} hue={6} info={t('subs.testHint')}>
              <View style={styles.actions}>
                <GlimButton tone="quiet" label={t('subs.test')} icon={(ink) => <Search color={ink} />} busy={testing} onPress={runTest} />
              </View>
              {testError ? <Text style={[styles.fault, { color: c.statusFailSolid }]}>{testError}</Text> : null}
              {result ? (
                <View style={[styles.result, { backgroundColor: c.surface2, ...corners.control }]}>{testLines(t, c, result, row)}</View>
              ) : null}
            </NotchCard>

            {error ? <Text style={[styles.error, { color: c.statusFailSolid }]}>{error}</Text> : null}
            {stored ? (
              <View style={styles.actions}>
                <GlimButton tone="quiet" label={t('subs.remove')} icon={(ink) => <Trash color={ink} />} onPress={() => setRemoving(true)} />
              </View>
            ) : null}
            <View style={styles.actions}>
              <GlimButton tone="quiet" grow label={t('subs.cancel')} icon={(ink) => <Cross color={ink} />} onPress={onClose} />
              <GlimButton grow label={t('subs.save')} icon={(ink) => <Check color={ink} />} busy={busy} onPress={save} />
            </View>
          </MovingScroll>

          <ConfirmDialog
            visible={removing}
            title={t('subs.removeTitle')}
            message={t('subs.removeBody', { name: nameOf(row) })}
            cancelLabel={t('subs.cancel')}
            confirmLabel={t('subs.remove')}
            confirmIcon={(ink) => <Trash color={ink} />}
            onCancel={() => setRemoving(false)}
            onConfirm={remove}
          />
        </View>
      </NoArrival>
    </Modal>
  );
}

type BackfillMode = 'none' | Backfill['mode'];

type T = (key: TranslationKey, vars?: Record<string, string | number>) => string;

// The groups below are plain functions rather than components, so the form is
// one render and check-subscriptions.mjs, whose stand-in draws one component,
// reaches every box of it.

/** What a channel hands over on its first check. Without a backfill it only
 *  remembers what is there, as every feed does. */
function backfillFields(t: T, value: Backfill | null, onChange: (b: Backfill | null) => void) {
  const mode: BackfillMode = value?.mode ?? 'none';
  const pick = (m: BackfillMode) => {
    if (m === 'none') onChange(null);
    else if (m === 'all') onChange({ mode: 'all' });
    else onChange({ mode: m, n: value && value.mode === m ? value.n : BACKFILL_START[m] });
  };
  const numberKey = value?.mode === 'days' ? 'subs.backfillDaysCount' : 'subs.backfillVideos';
  return (
    <>
      <EditField label={t('subs.backfill')} info={t('subs.backfillHint')}>
        <WellSelector<BackfillMode>
          options={[
            { value: 'none', label: t('subs.backfill.none') },
            { value: 'all', label: t('subs.backfill.all') },
            { value: 'count', label: t('subs.backfill.count') },
            { value: 'days', label: t('subs.backfill.days') },
          ]}
          value={mode}
          hue={5}
          onPick={pick}
        />
      </EditField>
      {value && value.mode !== 'all' ? (
        <EditField label={t(numberKey)}>
          <EditBox
            value={shown(value.n)}
            onChange={(v) => onChange({ mode: value.mode, n: digits(v) })}
            label={t(numberKey)}
            keyboard="number-pad"
          />
        </EditField>
      ) : null}
    </>
  );
}

const formatLabel = (t: T, v: string) => (v === 'best' ? t('subs.best') : v);

/** The yt-dlp choice: the host preset, or rows and quality chosen here. */
function mediaFields(t: T, value: MediaChoice | null, onChange: (m: MediaChoice | null) => void) {
  const what = value ? mediaWhat(value) : 'video';
  return (
    <>
      <EditField label={t('subs.media')} info={t('subs.mediaHint')}>
        <WellSelector
          options={[
            { value: 'preset', label: t('subs.media.preset') },
            { value: 'own', label: t('subs.media.own') },
          ]}
          value={value ? 'own' : 'preset'}
          hue={6}
          onPick={(v) => onChange(v === 'own' ? (value ?? DEFAULT_MEDIA) : null)}
        />
      </EditField>
      {value ? (
        <>
          <EditField label={t('subs.mediaWhat')}>
            <WellSelector<'video' | 'audio'>
              options={[
                { value: 'video', label: t('subs.mediaVideo') },
                { value: 'audio', label: t('subs.mediaAudio') },
              ]}
              value={what}
              hue={7}
              onPick={(w) => onChange(withWhat(value, w))}
            />
          </EditField>
          {what === 'video' ? (
            <>
              <EditField label={t('subs.quality')} info={t('subs.qualityHint')}>
                <WellSelector
                  options={QUALITIES.map((q) => ({ value: q, label: formatLabel(t, q) }))}
                  value={value.quality}
                  hue={8}
                  onPick={(quality) => onChange({ ...value, quality })}
                />
              </EditField>
              <EditField label={t('subs.videoFormat')}>
                <WellSelector
                  options={VIDEO_FORMATS.map((f) => ({ value: f, label: formatLabel(t, f) }))}
                  value={value.videoFormat}
                  hue={9}
                  onPick={(videoFormat) => onChange({ ...value, videoFormat })}
                />
              </EditField>
            </>
          ) : (
            <EditField label={t('subs.audioFormat')}>
              <WellSelector
                options={AUDIO_FORMATS.map((f) => ({ value: f, label: formatLabel(t, f) }))}
                value={value.audioFormat}
                hue={9}
                onPick={(audioFormat) => onChange({ ...value, audioFormat })}
              />
            </EditField>
          )}
          {EXTRAS.map((extra, i) => {
            const key = EXTRA_LABELS[extra];
            return (
              <GlimRow
                key={extra}
                label={t(key)}
                control={
                  <GlimToggle
                    hue={2 + i}
                    label={t(key)}
                    value={value.variants.includes(extra)}
                    onChange={(on) => onChange(withExtra(value, extra, on))}
                  />
                }
              />
            );
          })}
        </>
      ) : null}
    </>
  );
}

const EXTRA_LABELS: Record<(typeof EXTRAS)[number], TranslationKey> = {
  subtitle: 'subs.extraSubtitles',
  thumbnail: 'subs.extraThumbnail',
  description: 'subs.extraDescription',
};

/** SponsorBlock: the instance's choice, or each category kept, marked or cut here. */
function sponsorFields(t: T, value: SponsorBlock | null, onChange: (sb: SponsorBlock | null) => void) {
  const choices = (markOnly: boolean) => [
    { value: 'off' as SponsorChoice, label: t('subs.sb.off') },
    { value: 'mark' as SponsorChoice, label: t('subs.sb.mark') },
    ...(markOnly ? [] : [{ value: 'remove' as SponsorChoice, label: t('subs.sb.remove') }]),
  ];
  return (
    <>
      <EditField label={t('subs.sponsorBlock')}>
        <WellSelector
          options={[
            { value: 'default', label: t('subs.sb.default') },
            { value: 'own', label: t('subs.sb.own') },
          ]}
          value={value ? 'own' : 'default'}
          hue={10}
          onPick={(v) => onChange(v === 'own' ? (value ?? { remove: [], mark: [] }) : null)}
        />
      </EditField>
      {value
        ? SPONSOR_CATEGORIES.map((cat) => (
            <EditField key={cat} label={t(`subs.sb.${cat}` as TranslationKey)}>
              <WellSelector<SponsorChoice>
                options={choices(MARK_ONLY.includes(cat))}
                value={sponsorChoice(value, cat)}
                onPick={(choice) => onChange(withSponsor(value, cat, choice))}
              />
            </EditField>
          ))
        : null}
    </>
  );
}

/** What the test found: the first entries, how long each is, and whether the
 *  subscription would take it. */
function testLines(t: T, c: Palette, result: FeedTest, row: Subscription): ReactNode {
  let body: ReactNode;
  if (result.error) {
    body = <Text style={[styles.line, { color: c.statusWarnText }]}>{t('subs.testFailed', { error: result.error })}</Text>;
  } else if (result.total === 0) {
    body = <Text style={[styles.line, { color: c.statusWarnText }]}>{t('subs.testEmpty')}</Text>;
  } else {
    body = (
      <>
        {result.title ? <Text style={[styles.line, { color: c.textSub }]}>{t('subs.testName', { name: result.title })}</Text> : null}
        <Text style={[styles.line, { color: c.textMuted }]}>
          {t('subs.testCount', { matched: result.matched, total: result.total })}
        </Text>
        {result.entries.map((e, i) => {
          const taken = e.matches && !e.reason;
          return (
            <View key={`${i}:${e.link}`} style={styles.entry}>
              <View style={styles.entryHead}>
                <Text style={[styles.entryTitle, { color: taken ? c.text : c.textMuted }]} numberOfLines={2}>
                  {e.title}
                </Text>
                {e.duration > 0 ? <Text style={[styles.length, { color: c.textMuted }]}>{lengthText(e.duration)}</Text> : null}
              </View>
              <Text style={[styles.verdict, { color: taken ? c.textSub : c.textMuted }]}>{skipText(t, e, row)}</Text>
            </View>
          );
        })}
        {result.total > result.entries.length ? (
          <Text style={[styles.line, { color: c.textMuted }]}>{t('subs.testShowing', { n: result.entries.length })}</Text>
        ) : null}
      </>
    );
  }
  return body;
}

// Colours and radii are applied inline from the resolved tokens: a stylesheet
// is built once and cannot follow a theme change.
const capped = { width: '100%' as const, maxWidth: 640, alignSelf: 'center' as const };

const styles = StyleSheet.create({
  page: { flex: 1 },
  topBar: { ...capped, flexDirection: 'row', alignItems: 'center', gap: 12, padding: 16, paddingTop: 56 },
  titles: { flex: 1, minWidth: 0 },
  title: { fontSize: TYPE.status, fontWeight: '600' },
  status: { fontSize: TYPE.subline, marginTop: 2 },
  body: { ...capped, paddingHorizontal: 16 },
  address: { fontSize: TYPE.body },
  pair: { flexDirection: 'row', gap: 12 },
  half: { flex: 1 },
  fault: { fontSize: TYPE.subline, marginTop: 6 },
  error: { fontSize: TYPE.subline, marginTop: 16 },
  actions: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginTop: 16 },
  result: { marginTop: 12, padding: 12, gap: 8 },
  line: { fontSize: TYPE.subline },
  entry: { gap: 2 },
  entryHead: { flexDirection: 'row', alignItems: 'baseline', gap: 8 },
  entryTitle: { flex: 1, fontSize: TYPE.body },
  length: { fontSize: TYPE.meta, fontVariant: ['tabular-nums'] },
  verdict: { fontSize: TYPE.meta },
});
