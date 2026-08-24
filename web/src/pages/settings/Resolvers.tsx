import { useEffect, useState } from 'react';
import { Card, Field, FieldGroup, InfoBubble, SectionTitle, TextInput, ToggleRow } from '../../components/ui';
import { Tabs } from '../../components/Tabs';
import { fetchOptions, type YtdlpOptions } from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft, useFeatures } from './context';

// Keyed by the id the server sends (YtdlpOptions.quality/subtitles and the
// /api/options menus below), each pointing at the real settings.resolvers.*
// catalogue entry rather than embedding English text here - an id neither
// list has a key for still falls back to the raw id, the same "never a
// blank tab" rule QUALITY_LABELS used before this page had any i18n at all.
const QUALITY_KEYS: Record<string, TranslationKey> = {
  best: 'settings.resolvers.quality.best',
  '2160p': 'settings.resolvers.quality.2160p',
  '1440p': 'settings.resolvers.quality.1440p',
  '1080p': 'settings.resolvers.quality.1080p',
  '720p': 'settings.resolvers.quality.720p',
  '480p': 'settings.resolvers.quality.480p',
  '360p': 'settings.resolvers.quality.360p',
  audioOnly: 'settings.resolvers.quality.audioOnly',
  custom: 'settings.resolvers.quality.custom',
};

const SUBTITLE_KEYS: Record<string, TranslationKey> = {
  off: 'settings.resolvers.subtitles.off',
  file: 'settings.resolvers.subtitles.file',
  embed: 'settings.resolvers.subtitles.embed',
};

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
 */
export function Resolvers() {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { features } = useFeatures();

  const [qualities, setQualities] = useState<string[]>([]);
  const [subtitleModes, setSubtitleModes] = useState<string[]>([]);
  useEffect(() => {
    let live = true;
    void fetchOptions().then(
      (o) => {
        if (!live) return;
        setQualities(o.ytdlpQualities ?? []);
        setSubtitleModes(o.ytdlpSubtitleModes ?? []);
      },
      () => {
        /* the page still renders with whatever is already stored; the
           pickers stay out rather than offering a guess at the menus */
      },
    );
    return () => {
      live = false;
    };
  }, []);

  const ytdlp = cfg.ytdlp;
  const patchYtdlp = (fields: Partial<YtdlpOptions>) => patch({ ytdlp: { ...ytdlp, ...fields } });

  // Derived from the module registry, not guessed at: whether the yt-dlp
  // binary was actually found at start-up is live state, the same "never a
  // stored flag" rule every row on the modules page follows - see
  // routes_features.go's own file comment.
  const module = features.modules.find((m) => m.id === 'ytdlp');

  return (
    <div className="flex flex-col gap-10">
      {module && !module.enabled && (
          <Card className="flex items-center gap-2 text-sm text-carbon-textSub">
            <SectionTitle hue={0}>{t('settings.resolvers.moduleUnavailable')}</SectionTitle>
            <span>{module.reason}</span>
            <InfoBubble tip={t('settings.resolvers.moduleUnavailableHint')} />
          </Card>
      )}

      <Card className="flex flex-col gap-5">
        <SectionTitle hue={1}>{t('settings.resolvers.quality')}</SectionTitle>
        <p className="text-sm text-carbon-textMuted">{t('settings.resolvers.intro')}</p>

        {qualities.length > 0 && (
          <FieldGroup label={t('settings.resolvers.quality')} hint={t('settings.resolvers.qualityHint')}>
            <Tabs
              size="sm"
              className="w-fit"
              label={t('settings.resolvers.quality')}
              active={ytdlp.quality}
              onSelect={(id) => patchYtdlp({ quality: id })}
              items={qualities.map((q) => ({ id: q, label: QUALITY_KEYS[q] ? t(QUALITY_KEYS[q]) : q }))}
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

      <Card className="flex flex-col gap-5">
        <SectionTitle hue={2}>{t('settings.resolvers.subtitles')}</SectionTitle>
        {subtitleModes.length > 0 && (
          <FieldGroup label={t('settings.resolvers.subtitles')} hint={t('settings.resolvers.subtitlesHint')}>
            <Tabs
              size="sm"
              className="w-fit"
              label={t('settings.resolvers.subtitles')}
              active={ytdlp.subtitles}
              onSelect={(id) => patchYtdlp({ subtitles: id })}
              items={subtitleModes.map((m) => ({ id: m, label: SUBTITLE_KEYS[m] ? t(SUBTITLE_KEYS[m]) : m }))}
            />
          </FieldGroup>
        )}

        {ytdlp.subtitles !== 'off' && (
          <>
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
            />
          </>
        )}
      </Card>

      <Card className="flex flex-col gap-5">
        <SectionTitle hue={3}>{t('settings.resolvers.outputTitle')}</SectionTitle>
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
    </div>
  );
}
