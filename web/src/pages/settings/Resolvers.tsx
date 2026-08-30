import { useEffect, useState } from 'react';
import { Card, Field, FieldGroup, InfoBubble, SectionTitle, TextInput, ToggleRow } from '../../components/ui';
import { Tabs } from '../../components/Tabs';
import { fetchOptions, type YtdlpOptions } from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft, useFeatures } from './context';

// Keyed by the id the server sends (YtdlpOptions.quality and the
// /api/options ytdlpQualities menu below), each pointing at the real
// settings.resolvers.* catalogue entry rather than embedding English text
// here - an id the list has no key for still falls back to the raw id, the
// same "never a blank tab" rule QUALITY_LABELS used before this page had
// any i18n at all. Read only on a video row - see YtdlpOptions.quality.
const QUALITY_KEYS: Record<string, TranslationKey> = {
  best: 'settings.resolvers.quality.best',
  '2160p': 'settings.resolvers.quality.2160p',
  '1440p': 'settings.resolvers.quality.1440p',
  '1080p': 'settings.resolvers.quality.1080p',
  '720p': 'settings.resolvers.quality.720p',
  '480p': 'settings.resolvers.quality.480p',
  '360p': 'settings.resolvers.quality.360p',
  custom: 'settings.resolvers.quality.custom',
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
  useEffect(() => {
    let live = true;
    void fetchOptions().then(
      (o) => {
        if (!live) return;
        setQualities(o.ytdlpQualities ?? []);
      },
      () => {
        /* the page still renders with whatever is already stored; the
           picker stays out rather than offering a guess at the menu */
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
          <Card hue={0} className="flex items-center gap-2 text-sm text-carbon-textSub">
            <SectionTitle>{t('settings.resolvers.moduleUnavailable')}</SectionTitle>
            <span>{module.reason}</span>
            <InfoBubble tip={t('settings.resolvers.moduleUnavailableHint')} />
          </Card>
      )}

      <Card hue={1} className="flex flex-col gap-5">
        <SectionTitle hint={t('settings.resolvers.intro')}>{t('settings.resolvers.quality')}</SectionTitle>

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

      {/* Whether a subtitle row exists at all, per hoster, lives on that
          hoster's own "Variante" preset now (the gear badge on a link's
          package row) - this card is only the two knobs that still apply
          instance-wide once a subtitle row is enabled: which languages and
          whether auto-generated captions count. */}
      <Card hue={3} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.resolvers.subtitles')}</SectionTitle>
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
    </div>
  );
}
