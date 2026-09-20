import { useEffect, useState } from 'react';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  IconBadge,
  InfoBubble,
  NumberInput,
  SectionTitle,
  TextInput,
  Toggle,
  ToggleRow,
} from '../../components/ui';
import { Tabs } from '../../components/Tabs';
import { CookieJarsCard } from './resolvers/CookieJars';
import { MediaToolsCard } from './resolvers/MediaToolsCard';
import { IconTrash } from '../../lib/icons';
import {
  fetchOptions,
  YTDLP_VARIANT_KINDS,
  type YtdlpEmbed,
  type YtdlpHosterPreset,
  type YtdlpLive,
  type YtdlpMeasure,
  type YtdlpOptions,
  type YtdlpVariantKind,
} from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft, useFeatures } from './context';

// Labels by the quality id the server sends; an id without one shows raw.
// Read only on a video row.
const QUALITY_KEYS: Record<string, TranslationKey> = {
  best: 'settings.resolvers.quality.best',
  '4320p': 'settings.resolvers.quality.4320p',
  '2160p': 'settings.resolvers.quality.2160p',
  '1440p': 'settings.resolvers.quality.1440p',
  '1080p': 'settings.resolvers.quality.1080p',
  '720p': 'settings.resolvers.quality.720p',
  '480p': 'settings.resolvers.quality.480p',
  '360p': 'settings.resolvers.quality.360p',
  '240p': 'settings.resolvers.quality.240p',
  '144p': 'settings.resolvers.quality.144p',
  custom: 'settings.resolvers.quality.custom',
};

// The other formats are codec names, shown raw; "best" borrows the quality
// strip's label.
const AUDIO_FORMAT_KEYS: Record<string, TranslationKey> = {
  best: 'settings.resolvers.quality.best',
};

// Named like the download list's own variant labels.
const VARIANT_KEYS: Record<YtdlpVariantKind, TranslationKey> = {
  video: 'columns.variant.video',
  audio: 'columns.variant.audio',
  thumbnail: 'columns.variant.thumbnail',
  subtitle: 'columns.variant.subtitle',
  description: 'columns.variant.description',
};

// ytdlp.DefaultHosterPreset(), so a new row starts as what it replaces.
const DEFAULT_PRESET: YtdlpHosterPreset = {
  variants: [...YTDLP_VARIANT_KINDS],
  quality: 'best',
  audioFormat: 'best',
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
 * enableSelectWheel lets a closed <select> step one option per wheel notch,
 * clamped at both ends, and fires a bubbling `change` for its onChange. It uses
 * a native non-passive listener because React's onWheel is passive and cannot
 * prevent the page scroll. LogViewerCard.tsx has a copy.
 */
function enableSelectWheel(select: HTMLSelectElement | null): () => void {
  if (!select) return () => {};
  const onWheel = (event: WheelEvent) => {
    if (select.disabled || select.options.length < 2 || event.deltaY === 0) return;
    event.preventDefault();
    const delta = event.deltaY > 0 ? 1 : -1;
    const next = Math.min(select.options.length - 1, Math.max(0, select.selectedIndex + delta));
    if (next === select.selectedIndex) return;
    select.selectedIndex = next;
    select.dispatchEvent(new Event('change', { bubbles: true }));
  };
  select.addEventListener('wheel', onWheel, { passive: false });
  return () => select.removeEventListener('wheel', onWheel);
}

/**
 * Select is styled to match TextInput, as in Connections.tsx. Rows use it
 * rather than tab strips, which would not fit a table row.
 */
function Select({
  value,
  onChange,
  label,
  options,
  labelOf,
}: {
  value: string;
  onChange: (v: string) => void;
  label: string;
  options: string[];
  labelOf: (id: string) => string;
}) {
  // A stored value missing from the menu is kept as an option, or the select
  // would show the first entry and overwrite the real value on the next edit.
  const items = options.includes(value) ? options : [value, ...options];
  // A callback ref, so the wheel listener follows rows that mount and move.
  const [el, setEl] = useState<HTMLSelectElement | null>(null);
  useEffect(() => enableSelectWheel(el), [el]);
  return (
    <select
      ref={setEl}
      aria-label={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="glim-select appearance-none pe-6 w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text
        outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
    >
      {items.map((id) => (
        <option key={id} value={id}>
          {labelOf(id)}
        </option>
      ))}
    </select>
  );
}

/**
 * Resolvers configures yt-dlp, the one resolver with options of its own; the
 * routing order lives on the Accounts page. The per-row picker in the download
 * list beats the per-host preset, which beats these defaults, and every staged
 * yt-dlp link carries its preset's quality and audio format, so the defaults
 * here only reach what variant expansion does not set. The hints say so.
 */
export function Resolvers() {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { features } = useFeatures();

  const [qualities, setQualities] = useState<string[]>([]);
  const [audioFormats, setAudioFormats] = useState<string[]>([]);
  const [audioBitrates, setAudioBitrates] = useState<string[]>([]);
  useEffect(() => {
    // Not `live`, which names the livestream options on this page.
    let alive = true;
    void fetchOptions().then(
      (o) => {
        if (!alive) return;
        setQualities(o.ytdlpQualities ?? []);
        setAudioFormats(o.ytdlpAudioFormats ?? []);
        setAudioBitrates(o.ytdlpAudioBitrates ?? []);
      },
      () => {
        /* The page renders with what is stored; the picker stays out. */
      },
    );
    return () => {
      alive = false;
    };
  }, []);

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
    setNewHost('');
    setDuplicate(false);
  };

  const qualityLabel = (q: string) => (QUALITY_KEYS[q] ? t(QUALITY_KEYS[q]) : q);
  const audioFormatLabel = (f: string) => (AUDIO_FORMAT_KEYS[f] ? t(AUDIO_FORMAT_KEYS[f]) : f);

  // Whether the binary was found is live state from the module registry.
  const module = features.modules.find((m) => m.id === 'ytdlp');

  return (
    <div className="flex flex-col gap-10">
      {module && !module.enabled && (
          <Card hue={0} className="flex items-center gap-2 text-sm text-carbon-textSub">
            <SectionTitle>{t('settings.resolvers.moduleUnavailable')}</SectionTitle>
            <span>{module.reason}</span>
            <InfoBubble tip={t('settings.resolvers.moduleUnavailableHint')} />
          </Card>
      )}

      {/* First, since an outdated yt-dlp is the usual reason somebody opens
          this page. */}
      <MediaToolsCard hue={1} />

      <Card hue={2} className="flex flex-col gap-5">
        <SectionTitle hint={t('settings.resolvers.intro')}>{t('settings.resolvers.quality')}</SectionTitle>

        {qualities.length > 0 && (
          <FieldGroup label={t('settings.resolvers.quality')} hint={t('settings.resolvers.qualityHint')}>
            <Tabs
              size="sm"
              className="w-fit"
              label={t('settings.resolvers.quality')}
              active={ytdlp.quality}
              onSelect={(id) => patchYtdlp({ quality: id })}
              items={qualities.map((q) => ({ id: q, label: qualityLabel(q) }))}
            />
          </FieldGroup>
        )}

        {ytdlp.quality === 'custom' && (
          <Field label={t('settings.resolvers.customFormat')} hint={t('settings.resolvers.customFormatHint')}>
            <TextInput
              dir="ltr"
              value={ytdlp.customFormat}
              placeholder="bestvideo+bestaudio/best"
              spellCheck={false}
              onChange={(e) => patchYtdlp({ customFormat: e.target.value })}
            />
          </Field>
        )}

        <ToggleRow
          checked={ytdlp.playlist}
          onChange={(v) => patchYtdlp({ playlist: v })}
          label={t('settings.resolvers.playlist')}
        />
      </Card>

      {/* Whether a subtitle row exists is set per hoster in the presets; this
          card holds the instance-wide knobs. The embed card reuses these
          languages, so muxed tracks and .srt files agree. */}
      <Card hue={3} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.resolvers.subtitlesTitle')}</SectionTitle>
        <Field label={t('settings.resolvers.subtitleLangs')} hint={t('settings.resolvers.subtitleLangsHint')}>
          <TextInput
            dir="ltr"
            value={ytdlp.subtitleLangs}
            placeholder="en"
            spellCheck={false}
            onChange={(e) => patchYtdlp({ subtitleLangs: e.target.value })}
          />
        </Field>
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

      <Card hue={4} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.resolvers.outputTitle')}</SectionTitle>
        <Field label={t('settings.resolvers.outputTitle')} hint={t('settings.resolvers.outputHint')}>
          <TextInput
            dir="ltr"
            value={ytdlp.outputTemplate}
            placeholder="%(title)s.%(ext)s"
            spellCheck={false}
            onChange={(e) => patchYtdlp({ outputTemplate: e.target.value })}
          />
        </Field>
      </Card>

      {/* Below the output filename card, since music mode's naming applies only
          while that field is empty. */}
      <Card hue={5} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.resolvers.audioTitle')}</SectionTitle>

        {/* The menu comes from the server; the sanitizer folds anything else to "best". */}
        {audioFormats.length > 0 && (
          <FieldGroup label={t('settings.resolvers.audioFormat')} hint={t('settings.resolvers.audioFormatHint')}>
            <Tabs
              size="sm"
              className="w-fit"
              label={t('settings.resolvers.audioFormat')}
              active={ytdlp.audioFormat}
              onSelect={(id) => patchYtdlp({ audioFormat: id })}
              items={audioFormats.map((f) => ({ id: f, label: audioFormatLabel(f) }))}
            />
          </FieldGroup>
        )}

        {/* "" means no --audio-quality at all. Not disabled while the format is
            "best", since a row can still pick a format that uses it. */}
        {audioBitrates.length > 0 && (
          <FieldGroup label={t('settings.resolvers.audioBitrate')} hint={t('settings.resolvers.audioBitrateHint')}>
            <Tabs
              size="sm"
              className="w-fit"
              label={t('settings.resolvers.audioBitrate')}
              active={ytdlp.audioBitrate}
              onSelect={(id) => patchYtdlp({ audioBitrate: id })}
              items={audioBitrates.map((b) => ({ id: b, label: b === '' ? t('columns.variant.bitrateAuto') : b }))}
            />
          </FieldGroup>
        )}

        {/* Free text: the available languages vary per source. The server keeps
            only letters, digits and hyphens. */}
        <Field label={t('settings.resolvers.audioLang')} hint={t('settings.resolvers.audioLangHint')}>
          <TextInput
            dir="ltr"
            value={ytdlp.audioLang}
            placeholder="de"
            spellCheck={false}
            onChange={(e) => patchYtdlp({ audioLang: e.target.value })}
          />
        </Field>

        {/* Music mode writes metadata on the audio row whatever the embed card
            says, or the tags it fills in would never be written. */}
        <ToggleRow
          checked={ytdlp.music}
          onChange={(v) => patchYtdlp({ music: v })}
          label={t('settings.resolvers.music')}
          hint={t('settings.resolvers.musicHint')}
        />
      </Card>

      <Card hue={6} className="flex flex-col gap-4">
        <SectionTitle>{t('settings.resolvers.embedTitle')}</SectionTitle>
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

      <Card hue={7} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.resolvers.measureTitle')}</SectionTitle>
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
            <Field
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
            </Field>

            <ToggleRow
              checked={measure.failOnShort}
              onChange={(v) => patchMeasure({ failOnShort: v })}
              label={t('settings.resolvers.measureFailOnShort')}
              hint={t('settings.resolvers.measureFailOnShortHint')}
              hue={1}
            />
          </>
        )}
      </Card>

      <Card hue={8} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.resolvers.liveTitle')}</SectionTitle>
        {/* This switch also gates the detection, so with it off progress is
            parsed as before, and the rows under it are absent. Both limits are
            floored at 0 in onValue, since a negative one would stop the
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
            <Field label={t('settings.resolvers.liveMaxMinutes')} hint={t('settings.resolvers.liveMaxMinutesHint')}>
              <NumberInput
                value={live.maxMinutes}
                min={0}
                max={10080}
                step={1}
                onValue={(v) => patchLive({ maxMinutes: Math.max(0, Math.min(10080, v)) })}
              />
            </Field>
            <Field label={t('settings.resolvers.liveMaxMB')} hint={t('settings.resolvers.liveMaxMBHint')}>
              <NumberInput
                value={live.maxMB}
                min={0}
                step={1}
                onValue={(v) => patchLive({ maxMB: Math.max(0, v) })}
              />
            </Field>
          </>
        )}
      </Card>

      {/* Its own file, since the jar form is shared with a failed download. */}
      <CookieJarsCard hue={9} />

      <Card hue={10} className="flex flex-col gap-5">
        <SectionTitle hint={t('settings.resolvers.presetsHint')}>
          {t('settings.resolvers.presetsTitle')}
        </SectionTitle>

        <div className="glim-well overflow-x-auto p-0">
          {presetRows.length === 0 ? (
            <p className="px-4 py-3 text-sm text-carbon-textMuted">{t('settings.resolvers.presetsEmpty')}</p>
          ) : (
            <table className="w-full min-w-[54rem] border-collapse text-sm" aria-label={t('settings.resolvers.presetsTitle')}>
              <thead>
                <tr className="text-start text-xs text-carbon-textMuted">
                  <th className="px-4 py-3 text-start font-medium">{t('settings.resolvers.presetHost')}</th>
                  {YTDLP_VARIANT_KINDS.map((kind) => (
                    <th key={kind} className="w-16 px-2 py-3 text-start font-medium">
                      {t(VARIANT_KEYS[kind])}
                    </th>
                  ))}
                  <th className="px-2 py-3 text-start font-medium">{t('settings.resolvers.quality')}</th>
                  <th className="px-2 py-3 text-start font-medium">{t('settings.resolvers.audioFormat')}</th>
                  <th className="w-10 px-2 py-3">
                    <span className="sr-only">{t('settings.resolvers.presetRemove')}</span>
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-carbon-border/40">
                {presetRows.map(([host, preset], i) => (
                  <tr key={host} className="group transition-colors hover:bg-carbon-hover">
                    {/* Read-only, since the host is the lookup key; remove the
                        row and add another instead. */}
                    <td className="px-4 py-3 font-medium text-carbon-text">{host}</td>
                    {/* Hued by column, since each switch is its own question. */}
                    {YTDLP_VARIANT_KINDS.map((kind, k) => (
                      <td key={kind} className="px-2 py-3">
                        <Toggle
                          checked={preset.variants.includes(kind)}
                          onChange={() => toggleVariant(host, kind)}
                          label={`${t(VARIANT_KEYS[kind])} · ${host}`}
                          hideLabel
                          hue={k}
                        />
                      </td>
                    ))}
                    <td className="px-2 py-3">
                      <Select
                        value={preset.quality}
                        onChange={(v) => writePreset(host, { quality: v })}
                        label={`${t('settings.resolvers.quality')} · ${host}`}
                        options={qualities}
                        labelOf={qualityLabel}
                      />
                    </td>
                    <td className="px-2 py-3">
                      <Select
                        value={preset.audioFormat}
                        onChange={(v) => writePreset(host, { audioFormat: v })}
                        label={`${t('settings.resolvers.audioFormat')} · ${host}`}
                        options={audioFormats}
                        labelOf={audioFormatLabel}
                      />
                    </td>
                    {/* The row's action, shown on hover and focus. */}
                    <td className="px-2 py-3 text-end">
                      <IconBadge
                        hue={i}
                        className="opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100"
                        icon={<IconTrash width={16} height={16} />}
                        title={`${t('settings.resolvers.presetRemove')} · ${host}`}
                        aria-label={`${t('settings.resolvers.presetRemove')} · ${host}`}
                        onClick={() => removePreset(host)}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {/* FieldGroup, because a label around a button passes clicks to the input. */}
        <FieldGroup label={t('settings.resolvers.presetHost')} hint={t('settings.resolvers.presetHostHint')}>
          <div className="flex items-center gap-2">
            <TextInput
              dir="ltr"
              value={newHost}
              placeholder="youtube.com"
              spellCheck={false}
              aria-label={t('settings.resolvers.presetHost')}
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
            <Button kind="secondary" className="shrink-0" disabled={!normaliseHost(newHost)} onClick={addPreset}>
              {t('settings.resolvers.presetAdd')}
            </Button>
          </div>
        </FieldGroup>
        {/* The host rules' sentence, since it is the same mistake. */}
        {duplicate && <p className="text-xs text-statusWarn">{t('settings.hostRules.duplicate')}</p>}
      </Card>
    </div>
  );
}

