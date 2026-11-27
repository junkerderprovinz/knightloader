import { Fragment, useEffect, useState } from 'react';
import { Toggle } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { VARIANT_KIND_LABEL_KEY } from '../../../components/columns';
import {
  NO_PRESET_MENUS,
  VariantDropdown,
  presetMenusOf,
  presetPickers,
  type PickerProps,
  type PresetMenus,
} from '../../../components/VariantPicker';
import {
  fetchOptions,
  YTDLP_VARIANT_KINDS,
  type FeedSubscription,
  type YtdlpHosterPreset,
  type YtdlpVariantKind,
} from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { SettingRow } from '../controls';
import { NO_SPONSORBLOCK, SponsorBlockRows } from '../resolvers/SponsorBlock';

// What a subscription's yt-dlp links become, in place of the host preset and
// the instance's SponsorBlock default. An absent field leaves both in charge.

type MediaMode = 'preset' | 'own' | 'audio';

// ytdlp.DefaultHosterPreset(), so "Own choice" starts as what it replaces.
const DEFAULT_MEDIA: YtdlpHosterPreset = {
  variants: [...YTDLP_VARIANT_KINDS],
  videoFormat: 'best',
  quality: 'best',
  audioFormat: 'best',
  audioBitrate: '',
};

function modeOf(media: YtdlpHosterPreset | null | undefined): MediaMode {
  if (!media) return 'preset';
  return media.variants.length === 1 && media.variants[0] === 'audio' ? 'audio' : 'own';
}

/** The menus /api/options offers, empty until it answers, which keeps the pickers out. */
function usePresetMenus(): PresetMenus {
  const [menus, setMenus] = useState<PresetMenus>(NO_PRESET_MENUS);
  useEffect(() => {
    let alive = true;
    void fetchOptions().then(
      (o) => {
        if (alive) setMenus(presetMenusOf(o));
      },
      () => {
        /* The switches stand without their pickers. */
      },
    );
    return () => {
      alive = false;
    };
  }, []);
  return menus;
}

/**
 * MediaRows chooses which rows a staged yt-dlp link gets and their quality.
 * "Own choice" lays the rows out like the host preset window in the
 * collector, so the two read as one control.
 */
export function MediaRows({ row, onChange }: { row: FeedSubscription; onChange: (next: FeedSubscription) => void }) {
  const { t } = useT();
  const menus = usePresetMenus();
  const media = row.media ?? null;
  const mode = modeOf(media);

  const write = (next: YtdlpHosterPreset | null) => {
    const out = { ...row };
    delete out.media;
    if (next) out.media = next;
    onChange(out);
  };

  const choose = (next: MediaMode) => {
    if (next === 'preset') write(null);
    else if (next === 'audio') write({ ...(media ?? DEFAULT_MEDIA), variants: ['audio'] });
    else write({ ...(media ?? DEFAULT_MEDIA), variants: [...YTDLP_VARIANT_KINDS] });
  };

  const toggle = (kind: YtdlpVariantKind) => {
    if (!media) return;
    const on = media.variants.includes(kind);
    // The last row stays, since a link with no row would stage nothing.
    if (on && media.variants.length === 1) return;
    const next = on ? media.variants.filter((v) => v !== kind) : [...media.variants, kind];
    write({ ...media, variants: YTDLP_VARIANT_KINDS.filter((k) => next.includes(k)) });
  };

  const pickers = media && presetPickers({ preset: media, menus, t, onChange: (fields) => write({ ...media, ...fields }) });
  const pairs: Partial<Record<YtdlpVariantKind, (PickerProps | null)[]>> = pickers
    ? { video: [pickers.video.format, pickers.video.quality], audio: [pickers.audio.format, pickers.audio.bitrate] }
    : {};
  const shown: readonly YtdlpVariantKind[] = mode === 'audio' ? ['audio'] : YTDLP_VARIANT_KINDS;

  return (
    <>
      <SettingRow label={t('settings.feeds.media')} hint={t('settings.feeds.mediaHint')}>
        <Tabs
          variant="well"
          size="sm"
          inline
          hug
          label={t('settings.feeds.media')}
          active={mode}
          onSelect={(id) => choose(id as MediaMode)}
          items={[
            { id: 'preset', label: t('settings.feeds.mediaPreset') },
            { id: 'own', label: t('settings.feeds.ownChoice') },
            { id: 'audio', label: t('settings.feeds.mediaAudio') },
          ]}
        />
      </SettingRow>
      {media && (
        // One grid, so the pickers of the video and audio lines stand in columns.
        <div className="grid auto-rows-[minmax(var(--btn-h),auto)] grid-cols-[auto_minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-2">
          {shown.map((kind, k) => {
            const kindLabel = t(VARIANT_KIND_LABEL_KEY[kind]);
            const pair = menus.qualities.length > 0 ? pairs[kind] : undefined;
            return (
              <Fragment key={kind}>
                <span className="text-sm text-carbon-text">{kindLabel}</span>
                {[0, 1].map((i) => {
                  const p = pair?.[i];
                  return p ? <VariantDropdown key={i} picker={p} width="fill" /> : <span key={i} />;
                })}
                {mode === 'audio' ? (
                  <span />
                ) : (
                  <Toggle
                    hideLabel
                    label={kindLabel}
                    checked={media.variants.includes(kind)}
                    onChange={() => toggle(kind)}
                    hue={k}
                  />
                )}
              </Fragment>
            );
          })}
        </div>
      )}
    </>
  );
}

/** SponsorBlockOverride keeps the instance's SponsorBlock default or sets this subscription's own. */
export function SponsorBlockOverride({
  row,
  onChange,
}: {
  row: FeedSubscription;
  onChange: (next: FeedSubscription) => void;
}) {
  const { t } = useT();
  const own = row.sponsorBlock ?? null;

  const write = (next: FeedSubscription['sponsorBlock']) => {
    const out = { ...row };
    delete out.sponsorBlock;
    if (next) out.sponsorBlock = next;
    onChange(out);
  };

  return (
    <>
      <SettingRow label={t('settings.resolvers.sponsorBlock')} hint={t('settings.feeds.sponsorBlockHint')}>
        <Tabs
          variant="well"
          size="sm"
          inline
          hug
          label={t('settings.resolvers.sponsorBlock')}
          active={own ? 'own' : 'default'}
          onSelect={(id) => write(id === 'own' ? { ...NO_SPONSORBLOCK } : null)}
          items={[
            { id: 'default', label: t('settings.feeds.sponsorBlockDefault') },
            { id: 'own', label: t('settings.feeds.ownChoice') },
          ]}
        />
      </SettingRow>
      {own && <SponsorBlockRows value={own} onChange={write} />}
    </>
  );
}
