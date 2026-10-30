import { useEffect, useRef, useState } from 'react';
import {
  Button,
  Card,
  IconBadge,
  InfoBubble,
  Modal,
  NumberInput,
  SectionTitle,
  TextInput,
  Toggle,
  ToggleRow,
} from '../../components/ui';
import { TrackMerge } from '../../components/pictures/TrackMerge';
import { Tabs } from '../../components/Tabs';
import {
  NO_PRESET_MENUS,
  VariantDropdown,
  bitrateLabel,
  formatLabel,
  hostPresetMenus,
  presetMenusOf,
  presetPickers,
  type PickerProps,
  type PresetMenus,
} from '../../components/VariantPicker';
import { CookieJarsCard } from './resolvers/CookieJars';
import { fileTracks, mergedExtension } from './resolvers/fileTracks';
import { MediaToolsCard } from './resolvers/MediaToolsCard';
import { IconAdd, IconClose, IconTrash } from '../../lib/icons';
import {
  fetchHosterFormats,
  fetchOptions,
  YTDLP_VARIANT_KINDS,
  type YtdlpEmbed,
  type YtdlpHosterPreset,
  type YtdlpHostMenus,
  type YtdlpLive,
  type YtdlpMeasure,
  type YtdlpOptions,
  type YtdlpVariantKind,
} from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft } from './context';
import { SettingRow, SubCard } from './controls';

// Named like the download list's own variant labels.
const VARIANT_KEYS: Record<YtdlpVariantKind, TranslationKey> = {
  video: 'columns.variant.video',
  audio: 'columns.variant.audio',
  thumbnail: 'columns.variant.thumbnail',
  subtitle: 'columns.variant.subtitle',
  description: 'columns.variant.description',
};

// What buildArgs in internal/resolver/ytdlp falls back to, which no route
// serves: an empty template is this one.
const DEFAULT_TEMPLATE = '%(title)s.%(ext)s';

// ytdlp.VideoFormats(), in menu order.
const VIDEO_FORMATS = ['best', 'mp4', 'mkv', 'webm'];

// The default for every video stops at 360p, as sanitizeResolvers in
// internal/settings does; a smaller picture is a pick for one download.
const TOO_SMALL = new Set(['240p', '144p']);

// ytdlp.DefaultHosterPreset(), so a new row starts as what it replaces.
const DEFAULT_PRESET: YtdlpHosterPreset = {
  variants: [...YTDLP_VARIANT_KINDS],
  videoFormat: 'best',
  quality: 'best',
  audioFormat: 'best',
  audioBitrate: '',
};

/**
 * normaliseHost reduces a typed host or pasted URL to a www-stripped host. A
 * task's host arrives www-stripped (hostOf), while sanitizeResolvers only trims
 * and lower-cases, so without this a row for "www.youtube.com" would never
 * match.
 */
function normaliseHost(raw: string): string {
  const typed = raw.trim().toLowerCase();
  if (!typed) return '';
  let host: string;
  try {
    // Parsed as a URL so scheme, path, query and port go in one step.
    host = new URL(typed.includes('://') ? typed : `https://${typed}`).hostname;
  } catch {
    // Not a URL: keep the part before any slash; the server trims the rest.
    host = typed.split('/')[0] ?? typed;
  }
  return host.replace(/^www\./, '');
}

/**
 * Resolvers configures yt-dlp, the one resolver with options of its own; the
 * routing order lives on the Accounts page. The per-row pickers in the
 * collector beat the per-host preset, which beats these defaults, and every
 * staged yt-dlp link carries its preset's format and quality, so the defaults
 * here only reach what variant expansion does not set. The hints say so.
 */
export function Resolvers() {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  const [menus, setMenus] = useState<PresetMenus>(NO_PRESET_MENUS);
  useEffect(() => {
    // Not `live`, which names the livestream options on this page.
    let alive = true;
    void fetchOptions().then(
      (o) => {
        if (alive) setMenus(presetMenusOf(o));
      },
      () => {
        /* The page renders with what is stored; the pickers stay out. */
      },
    );
    return () => {
      alive = false;
    };
  }, []);
  const { qualities, audioFormats, audioBitrates } = menus;
  const resolutions = qualities.filter((q) => !TOO_SMALL.has(q));
  // Short, since the segments share one row: the resolution alone, which the
  // presets' pickers write the same way.
  const resolutionLabel = (q: string) => {
    if (q === 'best') return t('settings.resolvers.best');
    return q === 'custom' ? t('settings.resolvers.custom') : q;
  };

  const ytdlp = cfg.ytdlp;
  const patchYtdlp = (fields: Partial<YtdlpOptions>) => patch({ ytdlp: { ...ytdlp, ...fields } });
  // embed, measure and live are nested a level deeper, so each gets its own
  // spread; the shell patches whole top-level keys.
  const embed = ytdlp.embed;
  const measure = ytdlp.measure;
  const live = ytdlp.live;
  const patchEmbed = (fields: Partial<YtdlpEmbed>) => patchYtdlp({ embed: { ...embed, ...fields } });
  const patchMeasure = (fields: Partial<YtdlpMeasure>) => patchYtdlp({ measure: { ...measure, ...fields } });
  const patchLive = (fields: Partial<YtdlpLive>) => patchYtdlp({ live: { ...live, ...fields } });

  // A Go map never written marshals as null.
  const presets = cfg.ytdlpPresets ?? {};
  const presetRows = Object.entries(presets).sort(([a], [b]) => a.localeCompare(b));
  const [newHost, setNewHost] = useState('');
  const [duplicate, setDuplicate] = useState(false);
  const [addingPreset, setAddingPreset] = useState(false);

  // Each host's own formats, asked for once per host the table shows, a row
  // added in this draft included. Until one answers, or where none can, the
  // full menus stand in.
  const [hostMenus, setHostMenus] = useState<Record<string, YtdlpHostMenus>>({});
  const askedHosts = useRef(new Set<string>());
  const hostList = presetRows.map(([host]) => host).join(' ');
  useEffect(() => {
    for (const host of hostList.split(' ')) {
      if (!host || askedHosts.current.has(host)) continue;
      askedHosts.current.add(host);
      void fetchHosterFormats(host).then(
        (m) => setHostMenus((all) => ({ ...all, [host]: m })),
        () => {
          /* The full menus stay. */
        },
      );
    }
  }, [hostList]);

  // Rebuilt from this render's draft, since the collector's gear badge writes
  // the same map through POST /api/ytdlp/preset.
  const writePreset = (host: string, fields: Partial<YtdlpHosterPreset>) => {
    const current = presets[host];
    if (!current) return;
    patch({ ytdlpPresets: { ...presets, [host]: { ...current, ...fields } } });
  };

  const toggleVariant = (host: string, kind: YtdlpVariantKind) => {
    const current = presets[host];
    if (!current) return;
    const next = current.variants.includes(kind)
      ? current.variants.filter((v) => v !== kind)
      : [...current.variants, kind];
    // Kept in staging order, so toggling back and forth stores the same list.
    writePreset(host, { variants: YTDLP_VARIANT_KINDS.filter((k) => next.includes(k)) });
  };

  // Without a row the host falls back to all five variants at best quality,
  // so removing is the reset and rows need no switch.
  const removePreset = (host: string) => {
    const next = { ...presets };
    delete next[host];
    patch({ ytdlpPresets: next });
  };

  const addPreset = () => {
    const host = normaliseHost(newHost);
    if (!host) return;
    if (presets[host]) {
      setDuplicate(true);
      return;
    }
    patch({ ytdlpPresets: { ...presets, [host]: { ...DEFAULT_PRESET, variants: [...DEFAULT_PRESET.variants] } } });
    closeAddPreset();
  };

  const closeAddPreset = () => {
    setAddingPreset(false);
    setNewHost('');
    setDuplicate(false);
  };

  return (
    <div className="flex flex-col gap-10">
      {/* First, since an outdated yt-dlp is the usual reason somebody opens
          this page. */}
      <MediaToolsCard hue={1} />

      {/* Named like the video variant, whose format and resolution it sets
          where a row has no choice of its own. */}
      <Card hue={2} data-new="video-format" className="flex flex-col gap-5">
        <SectionTitle>{t('columns.variant.video')}</SectionTitle>

        {/* The sanitizer folds anything else to "best". */}
        <SettingRow label={t('settings.resolvers.format')} hint={t('settings.resolvers.formatHint')}>
          <Tabs
            variant="well"
            size="sm"
            inline
            hug
            label={t('settings.resolvers.format')}
            active={ytdlp.videoFormat}
            onSelect={(id) => patchYtdlp({ videoFormat: id })}
            items={VIDEO_FORMATS.map((f) => ({ id: f, label: f === 'best' ? t('settings.resolvers.best') : formatLabel(f, t) }))}
          />
        </SettingRow>

        {resolutions.length > 0 && (
          <SettingRow label={t('settings.resolvers.resolution')} hint={t('settings.resolvers.resolutionHint')}>
            <Tabs
              variant="well"
              size="sm"
              inline
              hug
              label={t('settings.resolvers.resolution')}
              active={ytdlp.quality}
              onSelect={(id) => patchYtdlp({ quality: id })}
              items={resolutions.map((q) => ({ id: q, label: resolutionLabel(q) }))}
            />
          </SettingRow>
        )}

        {ytdlp.quality === 'custom' && (
          <SettingRow
            control="long"
            label={t('settings.resolvers.customFormat')}
            hint={t('settings.resolvers.customFormatHint')}
          >
            <TextInput
              dir="ltr"
              value={ytdlp.customFormat}
              placeholder="bestvideo+bestaudio/best"
              spellCheck={false}
              onChange={(e) => patchYtdlp({ customFormat: e.target.value })}
            />
          </SettingRow>
        )}
      </Card>

      <Card hue={3} data-new="audio-best" className="flex flex-col gap-5">
        <SectionTitle>{t('settings.resolvers.audioTitle')}</SectionTitle>

        {/* The menu comes from the server; the sanitizer folds anything else to "best". */}
        {audioFormats.length > 0 && (
          <SettingRow label={t('settings.resolvers.audioFormat')} hint={t('settings.resolvers.audioFormatHint')}>
            <Tabs
              variant="well"
              size="sm"
              inline
              hug
              label={t('settings.resolvers.audioFormat')}
              active={ytdlp.audioFormat}
              onSelect={(id) => patchYtdlp({ audioFormat: id })}
              items={audioFormats.map((f) => ({ id: f, label: formatLabel(f, t) }))}
            />
          </SettingRow>
        )}

        {/* "" asks for the best quality. Kept while the format is "best",
            since a row can still pick a format that is converted. */}
        {audioBitrates.length > 0 && (
          <SettingRow label={t('settings.resolvers.audioBitrate')} hint={t('settings.resolvers.audioBitrateHint')}>
            <Tabs
              variant="well"
              size="sm"
              inline
              hug
              label={t('settings.resolvers.audioBitrate')}
              active={ytdlp.audioBitrate}
              onSelect={(id) => patchYtdlp({ audioBitrate: id })}
              items={audioBitrates.map((b) => ({ id: b, label: bitrateLabel(b, t) }))}
            />
          </SettingRow>
        )}

        {/* Free text: the available languages vary per source. The server keeps
            only letters, digits and hyphens. */}
        <SettingRow
          control="long"
          label={t('settings.resolvers.audioLang')}
          hint={t('settings.resolvers.audioLangHint')}
        >
          <TextInput
            dir="ltr"
            value={ytdlp.audioLang}
            placeholder="de"
            spellCheck={false}
            onChange={(e) => patchYtdlp({ audioLang: e.target.value })}
          />
        </SettingRow>

        {/* Music mode writes metadata on the audio row whatever the embed card
            says, or the tags it fills in would never be written. */}
        <ToggleRow
          checked={ytdlp.music}
          onChange={(v) => patchYtdlp({ music: v })}
          label={t('settings.resolvers.music')}
          hint={t('settings.resolvers.musicHint')}
        />
      </Card>

      {/* Whether a subtitle row exists is set per hoster in the presets; this
          card holds the instance-wide knobs. The embed card reuses these
          languages, so muxed tracks and .srt files agree. */}
      <Card hue={4} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.resolvers.subtitlesTitle')}</SectionTitle>
        <SettingRow
          control="long"
          label={t('settings.resolvers.subtitleLangs')}
          hint={t('settings.resolvers.subtitleLangsHint')}
        >
          <TextInput
            dir="ltr"
            value={ytdlp.subtitleLangs}
            placeholder="en"
            spellCheck={false}
            onChange={(e) => patchYtdlp({ subtitleLangs: e.target.value })}
          />
        </SettingRow>
        <ToggleRow
          checked={ytdlp.subtitleAuto}
          onChange={(v) => patchYtdlp({ subtitleAuto: v })}
          label={t('settings.resolvers.subtitleAuto')}
          hue={0}
        />
        {/* yt-dlp runs without warnings, so a missing language would settle
            green over an empty folder; this turns that into a failure. Off by
            default, since it fails rows existing installs settle green. */}
        <ToggleRow
          checked={ytdlp.subtitleStrict}
          onChange={(v) => patchYtdlp({ subtitleStrict: v })}
          label={t('settings.resolvers.subtitleStrict')}
          hint={t('settings.resolvers.subtitleStrictHint')}
          hue={1}
        />
      </Card>

      <Card hue={5} className="flex flex-col gap-4">
        <SectionTitle>{t('settings.resolvers.embedTitle')}</SectionTitle>

        {/* The video row as these settings leave it, following the switches
            below it. */}
        <TrackMerge
          label={t('settings.resolvers.embedTitle')}
          tracks={fileTracks(ytdlp, t)}
          extension={mergedExtension(ytdlp)}
          filename={ytdlp.outputTemplate.trim() || DEFAULT_TEMPLATE}
          split={embed.splitChapters ? t('picture.tracks.perChapter') : undefined}
          sidecar={embed.nfo ? 'nfo' : undefined}
          apart={embed.separate ? t('picture.tracks.apart') : undefined}
        />

        <div data-new="merge-files">
          <ToggleRow
            checked={!embed.separate}
            onChange={(v) => patchEmbed({ separate: !v })}
            label={t('settings.resolvers.mergeFiles')}
            hint={t('settings.resolvers.mergeFilesHint')}
          />
        </div>

        <ToggleRow
          checked={embed.metadata}
          onChange={(v) => patchEmbed({ metadata: v })}
          label={t('settings.resolvers.embedMetadata')}
          hint={t('settings.resolvers.embedMetadataHint')}
          hue={0}
        />
        <ToggleRow
          checked={embed.thumbnail}
          onChange={(v) => patchEmbed({ thumbnail: v })}
          label={t('settings.resolvers.embedThumbnail')}
          hint={t('settings.resolvers.embedThumbnailHint')}
          hue={1}
        />
        <ToggleRow
          checked={embed.chapters}
          onChange={(v) => patchEmbed({ chapters: v })}
          label={t('settings.resolvers.embedChapters')}
          hint={t('settings.resolvers.embedChaptersHint')}
          hue={2}
        />
        {/* Uses the subtitles card's languages (empty means en) and auto-caption
            switch. */}
        <ToggleRow
          checked={embed.subs}
          onChange={(v) => patchEmbed({ subs: v })}
          label={t('settings.resolvers.embedSubs')}
          hint={t('settings.resolvers.embedSubsHint')}
          hue={3}
        />
        <ToggleRow
          checked={embed.splitChapters}
          onChange={(v) => patchEmbed({ splitChapters: v })}
          label={t('settings.resolvers.embedSplitChapters')}
          hint={t('settings.resolvers.embedSplitChaptersHint')}
          hue={4}
        />
        {/* KnightLoader writes this sidecar itself from the info json, and a
            failed write never fails the download. */}
        <ToggleRow
          checked={embed.nfo}
          onChange={(v) => patchEmbed({ nfo: v })}
          label={t('settings.resolvers.embedNfo')}
          hint={t('settings.resolvers.embedNfoHint')}
          hue={5}
        />
      </Card>


      {/* Everything about the files a link becomes other than what is in
          them: how a playlist and a file are named, whether the finished file
          is measured, and when a recording stops. */}
      <Card hue={6} data-new="files-merged" className="flex flex-col gap-4">
        <SectionTitle>{t('settings.resolvers.filesCard')}</SectionTitle>

        <SubCard label={t('settings.resolvers.filesTitle')}>
          <ToggleRow
            checked={ytdlp.playlist}
            onChange={(v) => patchYtdlp({ playlist: v })}
            label={t('settings.resolvers.playlist')}
          />

          {/* Music mode's naming, on the audio card, applies only while this
              field is empty. */}
          <SettingRow
            control="long"
            label={t('settings.resolvers.outputTitle')}
            hint={t('settings.resolvers.outputHint')}
          >
            <TextInput
              dir="ltr"
              value={ytdlp.outputTemplate}
              placeholder={DEFAULT_TEMPLATE}
              spellCheck={false}
              onChange={(e) => patchYtdlp({ outputTemplate: e.target.value })}
            />
          </SettingRow>
        </SubCard>

        <SubCard label={t('settings.resolvers.measureTitle')}>
          <ToggleRow
            checked={measure.enabled}
            onChange={(v) => patchMeasure({ enabled: v })}
            label={t('settings.resolvers.measure')}
            hint={t('settings.resolvers.measureHint')}
            hue={0}
          />

          {/* Absent while the switch is off. Sanitize folds anything outside
              1..100, 0 included, onto 90, so onValue clamps typed input too. */}
          {measure.enabled && (
            <>
              <SettingRow
                control="short"
                label={t('settings.resolvers.measureShortPercent')}
                hint={t('settings.resolvers.measureShortPercentHint')}
              >
                <NumberInput
                  value={measure.shortPercent || 90}
                  min={1}
                  max={100}
                  step={1}
                  onValue={(v) => patchMeasure({ shortPercent: Math.max(1, Math.min(100, v)) })}
                />
              </SettingRow>

              <ToggleRow
                checked={measure.failOnShort}
                onChange={(v) => patchMeasure({ failOnShort: v })}
                label={t('settings.resolvers.measureFailOnShort')}
                hint={t('settings.resolvers.measureFailOnShortHint')}
                hue={1}
              />
            </>
          )}
        </SubCard>

        <SubCard label={t('settings.resolvers.liveTitle')}>
          {/* This switch also gates the detection, so with it off progress is
              parsed as before, and the rows under it are absent. Both limits
              are floored at 0 in onValue, since a negative one would stop the
              recording at once; 0 means no limit. */}
          <ToggleRow
            checked={live.enabled}
            onChange={(v) => patchLive({ enabled: v })}
            label={t('settings.resolvers.live')}
            hint={t('settings.resolvers.liveHint')}
            hue={0}
          />
          {live.enabled && (
            <>
              <ToggleRow
                checked={live.fromStart}
                onChange={(v) => patchLive({ fromStart: v })}
                label={t('settings.resolvers.liveFromStart')}
                hint={t('settings.resolvers.liveFromStartHint')}
                hue={1}
              />
              <SettingRow
                control="short"
                label={t('settings.resolvers.liveMaxMinutes')}
                hint={t('settings.resolvers.liveMaxMinutesHint')}
              >
                <NumberInput
                  value={live.maxMinutes}
                  min={0}
                  max={10080}
                  step={1}
                  onValue={(v) => patchLive({ maxMinutes: Math.max(0, Math.min(10080, v)) })}
                />
              </SettingRow>
              <SettingRow
                control="short"
                label={t('settings.resolvers.liveMaxMB')}
                hint={t('settings.resolvers.liveMaxMBHint')}
              >
                <NumberInput
                  value={live.maxMB}
                  min={0}
                  step={1}
                  onValue={(v) => patchLive({ maxMB: Math.max(0, v) })}
                />
              </SettingRow>
            </>
          )}
        </SubCard>
      </Card>

      {/* Its own file, since the jar form is shared with a failed download. */}
      <CookieJarsCard hue={7} />

      <Card hue={8} data-new="variant-blocks" className="flex flex-col gap-5">
        <SectionTitle
          hint={t('settings.resolvers.presetsHint')}
          right={
            <Button kind="secondary" icon={<IconAdd width={16} height={16} />} onClick={() => setAddingPreset(true)}>
              {t('settings.resolvers.presetAdd')}
            </Button>
          }
        >
          {t('settings.resolvers.variantDefaults')}
        </SectionTitle>

        {/* A block per site rather than a table: five columns do not fit a
            phone, and the pickers would have to cut their labels short. */}
        <div className="glim-well p-0">
          {presetRows.length === 0 ? (
            <p className="px-4 py-3 text-sm text-carbon-textMuted">{t('settings.resolvers.presetsEmpty')}</p>
          ) : (
            <ul className="flex flex-col divide-y divide-carbon-border/40" aria-label={t('settings.resolvers.variantDefaults')}>
              {presetRows.map(([host, preset], i) => {
                const pickers = presetPickers({
                  preset,
                  menus: hostPresetMenus(menus, hostMenus[host]),
                  t,
                  onChange: (fields) => writePreset(host, fields),
                });
                const pairs: Partial<Record<YtdlpVariantKind, (PickerProps | null)[]>> = {
                  video: [pickers.video.format, pickers.video.quality],
                  audio: [pickers.audio.format, pickers.audio.bitrate],
                };
                return (
                  <li key={host}>
                    <div role="group" aria-label={host} className="flex flex-col gap-3 px-4 py-3">
                      {/* Read-only, since the host is the lookup key; remove the
                          site and add it again instead. */}
                      <div className="flex items-center gap-2">
                        <span className="flex min-w-0 flex-1 items-center gap-1 text-sm font-medium text-carbon-text">
                          <span className="min-w-0 break-words">{host}</span>
                          {hostMenus[host]?.known === false && (
                            <InfoBubble tip={t('settings.resolvers.presetFormatsUnknown', { host })} />
                          )}
                        </span>
                        <IconBadge
                          hue={i}
                          icon={<IconTrash width={16} height={16} />}
                          title={`${t('settings.resolvers.presetRemove')} · ${host}`}
                          aria-label={`${t('settings.resolvers.presetRemove')} · ${host}`}
                          onClick={() => removePreset(host)}
                        />
                      </div>
                      <ul className="flex flex-col gap-2">
                        {/* Hued by variant, since each switch is its own question. */}
                        {YTDLP_VARIANT_KINDS.map((kind, k) => (
                          <li key={kind} className="flex min-h-[var(--btn-h)] flex-wrap items-center gap-x-4 gap-y-2">
                            <Toggle
                              checked={preset.variants.includes(kind)}
                              onChange={() => toggleVariant(host, kind)}
                              label={t(VARIANT_KEYS[kind])}
                              hue={k}
                            />
                            {qualities.length > 0 && pairs[kind] && (
                              <span className="ms-auto flex flex-wrap justify-end gap-2">
                                {/* Named with the host for a screen reader, which
                                    reads a picker's name without its block. */}
                                {pairs[kind].map(
                                  (p) =>
                                    p && (
                                      <VariantDropdown
                                        key={p.label}
                                        picker={{ ...p, label: `${p.label} · ${host}` }}
                                        width="widest"
                                      />
                                    ),
                                )}
                              </span>
                            )}
                          </li>
                        ))}
                      </ul>
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        {addingPreset && (
          <Modal
            title={t('settings.resolvers.presetAdd')}
            hue={8}
            onClose={closeAddPreset}
            footer={
              <>
                <span className="flex-1" />
                <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={closeAddPreset} />
                <Button icon={<IconAdd />} disabled={!normaliseHost(newHost)} onClick={addPreset}>
                  {t('settings.resolvers.presetAdd')}
                </Button>
              </>
            }
          >
            <SettingRow control="long" label={t('settings.resolvers.presetHost')} hint={t('settings.resolvers.presetHostHint')}>
              <TextInput
                dir="ltr"
                autoFocus
                value={newHost}
                placeholder="youtube.com"
                spellCheck={false}
                onChange={(e) => {
                  setNewHost(e.target.value);
                  setDuplicate(false);
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    addPreset();
                  }
                }}
              />
            </SettingRow>
            {/* The host rules' sentence, since it is the same mistake. */}
            {duplicate && <p className="text-xs text-statusWarn">{t('settings.hostRules.duplicate')}</p>}
          </Modal>
        )}
      </Card>
    </div>
  );
}

