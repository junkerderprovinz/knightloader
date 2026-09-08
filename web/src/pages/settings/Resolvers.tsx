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

// Keyed by the id the server sends (YtdlpOptions.quality and the
// /api/options ytdlpQualities menu below), each pointing at the real
// settings.resolvers.* catalogue entry rather than embedding English text
// here - an id the list has no key for still falls back to the raw id, the
// same "never a blank tab" rule QUALITY_LABELS used before this page had
// any i18n at all. Read only on a video row - see YtdlpOptions.quality.
//
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

// Every other entry in ytdlp.AudioFormats() (aac, alac, flac, m4a, mp3,
// opus, vorbis, wav) is a codec name, which is the same word in every
// locale and is therefore rendered as the raw id - only "best" is a word
// somebody reads, and it means exactly what the quality strip's own "best"
// means, so it borrows that entry instead of a second string saying the
// same thing in a slightly different way.
const AUDIO_FORMAT_KEYS: Record<string, TranslationKey> = {
  best: 'settings.resolvers.quality.best',
};

// The five "Variante" rows every yt-dlp link is staged as, named with the
// list's own column labels so the preset table and the download list call
// the same row the same thing.
const VARIANT_KEYS: Record<YtdlpVariantKind, TranslationKey> = {
  video: 'columns.variant.video',
  audio: 'columns.variant.audio',
  thumbnail: 'columns.variant.thumbnail',
  subtitle: 'columns.variant.subtitle',
  description: 'columns.variant.description',
};

// What ytdlp.DefaultHosterPreset() hands out for a host with no row: all
// five variants staged and enabled, best quality, best audio. Written here
// as well so a freshly added row starts as the thing it is replacing,
// rather than as an empty preset that would silently stage nothing.
const DEFAULT_PRESET: YtdlpHosterPreset = {
  variants: [...YTDLP_VARIANT_KINDS],
  quality: 'best',
  audioFormat: 'best',
};

/**
 * The one key normalisation on this page, and it is load-bearing.
 *
 * A preset is looked up with the host a task carries, and that host has
 * already been www-stripped by hostOf (internal/app/app.go) before
 * HosterPresetFor ever sees it. The settings sanitizer, on the other hand,
 * only trims and lower-cases what this page sends (sanitizeResolvers,
 * internal/settings/settings_resolvers.go). So a row typed as
 * "www.youtube.com" - or pasted as a whole watch URL, which is what anybody
 * with the link in their clipboard will do - is stored happily and then
 * never matches a single download. Normalising here is what closes that
 * gap from the only side this page controls.
 */
function normaliseHost(raw: string): string {
  const typed = raw.trim().toLowerCase();
  if (!typed) return '';
  let host: string;
  try {
    // Parsed rather than string-chopped so a pasted address loses its
    // scheme, path, query and port in one step; a bare host is given a
    // scheme first, because URL refuses to parse one without.
    host = new URL(typed.includes('://') ? typed : `https://${typed}`).hostname;
  } catch {
    // Not a URL at all (a stray space, a half-typed host). Keep what was
    // typed minus anything path-shaped rather than throwing the entry away:
    // the server still trims and lower-cases whatever arrives.
    host = typed.split('/')[0] ?? typed;
  }
  return host.replace(/^www\./, '');
}

/**
 * The one control the design language has no primitive for, the same
 * treatment Connections.tsx gives its own (styled to match TextInput, so a
 * row does not read as two different systems).
 *
 * A tab strip is what this page uses for the same two menus at the top -
 * but nine qualities plus nine audio formats on every table ROW would be
 * wider than the table they sit in, and a preset table is a grid of small
 * decisions, not nine strips.
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
  // A stored value the menu does not carry (an older build's id, or the
  // options fetch having failed entirely) is prepended rather than dropped:
  // a select that cannot show its own value would report the first entry as
  // chosen and overwrite the real one on the next edit.
  const items = options.includes(value) ? options : [value, ...options];
  return (
    <select
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
 * Resolvers is the settings page for internal/resolver/*'s own configurable
 * knobs. Today that is yt-dlp alone: Direct/HTTPFallback take no options,
 * the debrid and TorBox backends are pure credential clients configured on
 * the Accounts page, and the headless-JD backend delegates to JD's own
 * settings - see settings_resolvers.go's own doc comment for the full
 * reasoning, and docs/jd-feature-census.md's "(per-plugin option list)" row
 * for why yt-dlp is the one place this was ever missing.
 *
 * WHICH service handles a link at all - the routing order, the JD sidecar's
 * reachability - is not repeated here: it already has a live section on the
 * Accounts page (RoutingSection, fetchResolverPriority/fetchJDStatus). This
 * page is the other half, what yt-dlp specifically does once a link has
 * already been routed to it.
 *
 * THREE LAYERS DECIDE WHAT A LINK ACTUALLY DOWNLOADS, and this page is the
 * bottom one: the per-row picker in the download list (core.Task.Variant /
 * Task.AudioBitrate) beats the per-host preset, which beats these
 * instance-wide defaults. And the middle layer always answers today -
 * expandYtdlpVariants bakes the preset's quality into the video row's
 * variant string and its audioFormat into the audio row's, and
 * HosterPreset.Sanitize guarantees both are non-empty, while
 * ytdlpOptionsForTask only falls back to a settings value when the variant
 * string carries no sub of its own. So quality and audioFormat here reach
 * nothing that was staged through variant expansion, which is every yt-dlp
 * link. That is not hidden: both hints say so, and the per-host card at the
 * foot of the page is where the answer that does reach a new link lives.
 */
export function Resolvers() {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { features } = useFeatures();

  const [qualities, setQualities] = useState<string[]>([]);
  const [audioFormats, setAudioFormats] = useState<string[]>([]);
  const [audioBitrates, setAudioBitrates] = useState<string[]>([]);
  useEffect(() => {
    // `alive`, not the `live` this guard is called everywhere else in the
    // app: `live` is the livestream option block on this page now, and a
    // mount guard shadowing it inside one callback is the kind of thing
    // that reads correctly and means something else.
    let alive = true;
    void fetchOptions().then(
      (o) => {
        if (!alive) return;
        setQualities(o.ytdlpQualities ?? []);
        setAudioFormats(o.ytdlpAudioFormats ?? []);
        setAudioBitrates(o.ytdlpAudioBitrates ?? []);
      },
      () => {
        /* the page still renders with whatever is already stored; the
           picker stays out rather than offering a guess at the menu */
      },
    );
    return () => {
      alive = false;
    };
  }, []);

  const ytdlp = cfg.ytdlp;
  const patchYtdlp = (fields: Partial<YtdlpOptions>) => patch({ ytdlp: { ...ytdlp, ...fields } });
  // embed/measure/live are nested one level deeper than everything else on
  // this page, so each gets its own spread. Never rebuild ytdlp from
  // scratch: the settings document carries more than this TS type names,
  // and the shell PATCHes whole top-level keys.
  const embed = ytdlp.embed;
  const measure = ytdlp.measure;
  const live = ytdlp.live;
  const patchEmbed = (fields: Partial<YtdlpEmbed>) => patchYtdlp({ embed: { ...embed, ...fields } });
  const patchMeasure = (fields: Partial<YtdlpMeasure>) => patchYtdlp({ measure: { ...measure, ...fields } });
  const patchLive = (fields: Partial<YtdlpLive>) => patchYtdlp({ live: { ...live, ...fields } });

  // A Go map that was never written marshals as null, not as {}, so this
  // field can arrive null however non-optional the TS type is.
  const presets = cfg.ytdlpPresets ?? {};
  const presetRows = Object.entries(presets).sort(([a], [b]) => a.localeCompare(b));
  const [newHost, setNewHost] = useState('');
  const [duplicate, setDuplicate] = useState(false);

  // Every preset write rebuilds the map from `presets` above - which is the
  // draft as it stands this render, not a value captured earlier. The gear
  // badge on a collector package writes the SAME map through POST
  // /api/ytdlp/preset with a read-modify-write of its own, so a map built
  // from a stale copy here would quietly drop whatever that badge saved.
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
    // Rebuilt in the fixed staging order rather than in click order: the
    // server keeps the order it is given, and a row whose switches read
    // video/audio before a click and audio/video after it looks like it
    // changed something it did not.
    writePreset(host, { variants: YTDLP_VARIANT_KINDS.filter((k) => next.includes(k)) });
  };

  // A real reset, not a disable: with no row of its own the host falls back
  // to all five variants on at best quality, which is why there is no
  // "enabled" switch on a row here.
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

  // Derived from the module registry, not guessed at: whether the yt-dlp
  // binary was actually found at start-up is live state, the same "never a
  // stored flag" rule every row on the modules page follows - see
  // routes_features.go's own file comment.
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

      {/* First, and above the options, because somebody who lands on this page
          is usually here because a media link that worked last month stopped
          working - and an out of date yt-dlp is by a wide margin the most
          common reason for that. The quality strip below is a preference; this
          is the fact. Its own file for the same reason CookieJars has one: the
          card carries a fetch, a verification and a swap, and none of that
          belongs in the middle of a page of option strips. hue 1 pushed the
          quality card to 2, which fills the gap the palette sequence already
          had rather than shifting eight cards. */}
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

      {/* Whether a subtitle row exists at all, per hoster, lives on that
          hoster's own "Variante" preset now (the gear badge on a link's
          package row, and the per-host table at the foot of this page) -
          this card is only the knobs that still apply instance-wide once a
          subtitle row is enabled: which languages, whether auto-generated
          captions count, and whether a row that wrote nothing is allowed to
          settle green. The embed card further down has no language field of
          its own either: its "mux subtitles" switch reads the two above,
          so the muxed tracks and the .srt files cannot disagree. */}
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
        {/* The backend runs yt-dlp with warnings switched off, so "that
            language was never on offer" arrives as nothing at all and the
            row settles green over an empty folder. This is the only switch
            that turns that silence into a failure, and it stays off by
            default because turning it on makes rows fail that an existing
            install has been settling green for months. */}
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

      {/* Below the output filename card on purpose: music mode's own naming
          scheme only applies while that field is empty, and its hint says
          so about the field directly above it. */}
      <Card hue={5} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.resolvers.audioTitle')}</SectionTitle>

        {/* Menu from the server, never a guessed one - the sanitizer folds
            anything not on this list, empty included, to "best", so a tab
            this build invented would silently become something else on
            save. */}
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

        {/* The empty id is a real entry, not a gap: "" is what the audio row
            sends when it passes no --audio-quality at all, and the list's
            own "Auto" label is the one already used by the per-row bitrate
            picker. Not disabled while audioFormat is "best", even though it
            does nothing there: the format can also be chosen per row, so a
            greyed-out field here would be greying out a value that is still
            about to be used. The hint carries that instead. */}
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

        {/* Free text, and it stays free text: ytdlp.AvailableAudioLangs is
            computed per source and never lands on a task or on
            /api/options, so any menu here would be this build guessing at
            what a given video carries. The server keeps only letters,
            digits and hyphens and silently drops the rest rather than
            refusing the save - a stray bracket or comma would change the
            shape of the format selector, not just the language. */}
        <Field label={t('settings.resolvers.audioLang')} hint={t('settings.resolvers.audioLangHint')}>
          <TextInput
            dir="ltr"
            value={ytdlp.audioLang}
            placeholder="de"
            spellCheck={false}
            onChange={(e) => patchYtdlp({ audioLang: e.target.value })}
          />
        </Field>

        {/* Music mode switches embedded metadata on for the audio row by
            itself, whatever the embed card below says: filling tags in and
            then never writing them would rename the files and tag nothing. */}
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
        {/* Reads the subtitles card above for its languages (empty meaning
            en) and for the auto-caption switch, so the muxed tracks and the
            .srt files can never disagree about what was asked for. */}
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
        {/* The one row here that is not a yt-dlp flag: KnightLoader writes
            the sidecar itself, from the info json it asks for and deletes
            again, and a failed write never fails the download. */}
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

        {/* ZERO IS NOT ZERO HERE. Sanitize folds anything outside 1..100,
            0 included, onto the built-in 90 instead of clamping it to the
            nearest end - so a stored 0 behaves as 90 and is shown as 90,
            and the field never sends a 0 back. NumberInput clamps its
            stepper only; typed input goes through Number() untouched,
            which is why the clamp is repeated in onValue. */}
        <div className={measure.enabled ? '' : 'pointer-events-none opacity-40'}>
          <Field
            label={t('settings.resolvers.measureShortPercent')}
            hint={t('settings.resolvers.measureShortPercentHint')}
          >
            <NumberInput
              value={measure.shortPercent || 90}
              min={1}
              max={100}
              step={1}
              disabled={!measure.enabled}
              onValue={(v) => patchMeasure({ shortPercent: Math.max(1, Math.min(100, v)) })}
            />
          </Field>
        </div>

        <ToggleRow
          checked={measure.failOnShort}
          onChange={(v) => patchMeasure({ failOnShort: v })}
          label={t('settings.resolvers.measureFailOnShort')}
          hint={t('settings.resolvers.measureFailOnShortHint')}
          disabled={!measure.enabled}
          hue={1}
        />
      </Card>

      <Card hue={8} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.resolvers.liveTitle')}</SectionTitle>
        {/* This switch also gates the detection itself: left off, every
            download keeps exactly the progress format and the exact
            parsing it always had, which is why the three rows under it are
            dimmed rather than hidden - they say what the mode can do. */}
        <ToggleRow
          checked={live.enabled}
          onChange={(v) => patchLive({ enabled: v })}
          label={t('settings.resolvers.live')}
          hint={t('settings.resolvers.liveHint')}
          hue={0}
        />
        <ToggleRow
          checked={live.fromStart}
          onChange={(v) => patchLive({ fromStart: v })}
          label={t('settings.resolvers.liveFromStart')}
          hint={t('settings.resolvers.liveFromStartHint')}
          disabled={!live.enabled}
          hue={1}
        />

        {/* Both limits are floored at 0 in onValue rather than left to the
            server: a negative one is stored as 0 there, but on the way it
            would be a limit that stops the recording on its very first
            progress line. 0 is "no limit" for both, and whichever is
            reached first stops the recording gently. */}
        <div className={`flex flex-col gap-5 ${live.enabled ? '' : 'pointer-events-none opacity-40'}`}>
          <Field label={t('settings.resolvers.liveMaxMinutes')} hint={t('settings.resolvers.liveMaxMinutesHint')}>
            <NumberInput
              value={live.maxMinutes}
              min={0}
              max={10080}
              step={1}
              disabled={!live.enabled}
              onValue={(v) => patchLive({ maxMinutes: Math.max(0, Math.min(10080, v)) })}
            />
          </Field>
          <Field label={t('settings.resolvers.liveMaxMB')} hint={t('settings.resolvers.liveMaxMBHint')}>
            <NumberInput
              value={live.maxMB}
              min={0}
              step={1}
              disabled={!live.enabled}
              onValue={(v) => patchLive({ maxMB: Math.max(0, v) })}
            />
          </Field>
        </div>
      </Card>

      {/* Its own file, and not because this one is long: the window that pastes
          a jar is also opened from a failed download, and a card that kept its
          own copy of that form would be the copy that stops matching the
          dialog. What lives there is the whole feature - the switch, the list
          of sites with a jar, and the way in. */}
      <CookieJarsCard hue={9} />

      <Card hue={10} className="flex flex-col gap-5">
        <SectionTitle hint={t('settings.resolvers.presetsHint')}>
          {t('settings.resolvers.presetsTitle')}
        </SectionTitle>

        {/* The glim-well wrapper with a plain table inside, as the accounts
            table does it - never a nested Card, and never a Card per row. */}
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
                    {/* Read-only in the row: the key is what a lookup
                        matches on, so renaming it in place would move
                        every setting on the row to a different site
                        without saying so. Remove it and add the other. */}
                    <td className="px-4 py-3 font-medium text-carbon-text">{host}</td>
                    {/* Hued by COLUMN, not by row: the five switches are
                        five different things, and every row's answer to
                        "video?" should read as the same question. */}
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
                    {/* The one row action, styled the way the host-rules table
                        styles the same deletion of the same kind of per-host
                        row: `danger`, and revealed on hover or on keyboard
                        focus so a long table reads as content rather than as
                        a column of red buttons. */}
                    <td className="px-2 py-3 text-end">
                      <IconBadge
                        kind="danger"
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

        {/* FieldGroup, not Field: this caption sits over an input AND a
            button, and a <label> wrapping a button forwards its own clicks
            to the input. */}
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
        {/* Borrowed from the host-rules table rather than given a second
            string of its own: it is the same sentence about the same
            mistake on the same kind of key. */}
        {duplicate && <p className="text-xs text-statusWarn">{t('settings.hostRules.duplicate')}</p>}
      </Card>
    </div>
  );
}

