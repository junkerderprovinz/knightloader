import { useEffect, useState } from 'react';
import { Card, Field, FieldGroup, InfoBubble, TextInput, Toggle } from '../../components/ui';
import { Tabs } from '../../components/Tabs';
import { fetchOptions, type YtdlpOptions } from '../../lib/api';
import { useDraft, useFeatures } from './context';

// Hardcoded English rather than useT(): the keys this page needs are not in
// any locale file yet, including en.ts, which is the compile-time source of
// truth every real translation key is checked against - see
// DownloadsSettings.tsx's own doc comment for the full reasoning and why
// this is the same trade Wave 9's StatusStrip and this page's own sibling
// both made first. i18n for this page lands with the wave's own locale
// pass, not here.
const QUALITY_LABELS: Record<string, string> = {
  best: 'Best available',
  '2160p': 'Up to 2160p (4K)',
  '1440p': 'Up to 1440p',
  '1080p': 'Up to 1080p',
  '720p': 'Up to 720p',
  '480p': 'Up to 480p',
  '360p': 'Up to 360p',
  audioOnly: 'Audio only',
  custom: 'Custom format string',
};

const SUBTITLE_LABELS: Record<string, string> = {
  off: 'Off',
  file: 'Save alongside the video',
  embed: 'Embed into the video',
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
    <div className="flex flex-col gap-6">
      {module && !module.enabled && (
        <Card className="flex items-center gap-2 text-sm text-carbon-textSub">
          <span>{module.reason}</span>
          <InfoBubble tip="Everything below is still saved and takes effect the moment yt-dlp becomes available - none of it is lost by editing it now." />
        </Card>
      )}

      <Card className="flex flex-col gap-5">
        <p className="text-sm text-carbon-textMuted">
          Configuration for the yt-dlp backend, which fetches the media and streaming sites
          yt-dlp itself supports. Which service handles a given link at all - yt-dlp, a debrid
          account, the headless JD sidecar - is decided on the Accounts page's routing order;
          this is what yt-dlp does once a link has already been routed to it.
        </p>

        {qualities.length > 0 && (
          <FieldGroup
            label="Quality"
            hint="Which -f selector yt-dlp is spawned with. Best available is yt-dlp's own default, and what every download used before this setting existed."
          >
            <Tabs
              size="sm"
              className="w-fit"
              label="Quality"
              active={ytdlp.quality}
              onSelect={(id) => patchYtdlp({ quality: id })}
              items={qualities.map((q) => ({ id: q, label: QUALITY_LABELS[q] ?? q }))}
            />
          </FieldGroup>
        )}

        {ytdlp.quality === 'custom' && (
          <Field
            label="Custom format"
            hint="yt-dlp's own -f value, e.g. bestvideo[height<=720]+bestaudio/best. Passed through unexamined; a value yt-dlp rejects fails with its own error on the task."
          >
            <TextInput
              dir="ltr"
              value={ytdlp.customFormat}
              placeholder="bestvideo+bestaudio/best"
              spellCheck={false}
              onChange={(e) => patchYtdlp({ customFormat: e.target.value })}
            />
          </Field>
        )}

        <Toggle
          checked={ytdlp.playlist}
          onChange={(v) => patchYtdlp({ playlist: v })}
          label="Download the whole playlist when a link points into one"
        />
      </Card>

      <Card className="flex flex-col gap-5">
        {subtitleModes.length > 0 && (
          <FieldGroup label="Subtitles" hint="Off is what every download did before this setting existed.">
            <Tabs
              size="sm"
              className="w-fit"
              label="Subtitles"
              active={ytdlp.subtitles}
              onSelect={(id) => patchYtdlp({ subtitles: id })}
              items={subtitleModes.map((m) => ({ id: m, label: SUBTITLE_LABELS[m] ?? m }))}
            />
          </FieldGroup>
        )}

        {ytdlp.subtitles !== 'off' && (
          <>
            <Field label="Languages" hint="yt-dlp's own --sub-langs value, e.g. en,de. Empty defaults to en.">
              <TextInput
                dir="ltr"
                value={ytdlp.subtitleLangs}
                placeholder="en"
                spellCheck={false}
                onChange={(e) => patchYtdlp({ subtitleLangs: e.target.value })}
              />
            </Field>
            <Toggle
              checked={ytdlp.subtitleAuto}
              onChange={(v) => patchYtdlp({ subtitleAuto: v })}
              label="Also fetch auto-generated captions when no manual track exists"
            />
          </>
        )}
      </Card>

      <Card className="flex flex-col gap-5">
        <Field
          label="Output filename"
          hint="yt-dlp's own -o template. Empty uses the built-in %(title)s.%(ext)s. May include subfolders, e.g. %(uploader)s/%(title)s.%(ext)s."
        >
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
