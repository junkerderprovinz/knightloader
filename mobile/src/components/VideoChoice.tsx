import { useEffect, useState } from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import type { ServerConnection } from '../api/types';
import { fetchLinkMenus } from '../api/videoLink';
import {
  choiceOf,
  convertedFormats,
  firstPick,
  formatLabel,
  formatsOf,
  offersChoice,
  partsOf,
  qualitiesOf,
  withFormat,
  withPart,
  type LinkChoice,
  type LinkMenus,
  type Pick,
} from '../api/videoPick';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { NotchCard, WellSelector } from './glim';
import { Text } from './Text';

// Typing a link changes it on every key, and each probe costs the instance a
// call to yt-dlp, so the probe waits until the field holds still.
const SETTLE_MS = 700;

/**
 * VideoChoice looks at the one link a field holds and offers its formats and
 * qualities once the instance has said what the video has (VideoPicker). A
 * link the instance does not give yt-dlp shows nothing, and so does a probe
 * that fails: the link is then added as it is. `onChoice` hears the choice to
 * send, null while there is none.
 */
export function VideoChoice({
  conn,
  base = '/api',
  url,
  onChoice,
}: {
  conn: ServerConnection;
  base?: string;
  /** The one link to look at, null for none. */
  url: string | null;
  onChoice: (choice: LinkChoice | null) => void;
}) {
  const { t } = useT();
  const { c } = useAppearance();
  const [probing, setProbing] = useState(false);
  const [menus, setMenus] = useState<LinkMenus | null>(null);

  useEffect(() => {
    setMenus(null);
    setProbing(false);
    onChoice(null);
    if (!url) return;
    let live = true;
    const timer = setTimeout(() => {
      setProbing(true);
      fetchLinkMenus(conn, url, base)
        .then((m) => {
          if (live && m && offersChoice(m)) setMenus(m);
        })
        .catch(() => {})
        .finally(() => live && setProbing(false));
    }, SETTLE_MS);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [conn, base, url, onChoice]);

  if (probing) {
    return (
      <View style={styles.probing}>
        <ActivityIndicator color={c.textMuted} size="small" />
        <Text style={[styles.probingText, { color: c.textMuted }]}>{t('video.probing')}</Text>
      </View>
    );
  }
  return menus ? <VideoPicker menus={menus} onChoice={onChoice} /> : null;
}

/**
 * VideoPicker is the choice the web's pickers give a yt-dlp row: video or
 * audio where the link has both, then a format, then a quality or a bitrate
 * within it. It starts on Auto, which leaves each to the instance.
 */
export function VideoPicker({ menus, onChoice }: { menus: LinkMenus; onChoice: (choice: LinkChoice) => void }) {
  const { t } = useT();
  const { c } = useAppearance();
  const [pick, setPick] = useState<Pick>(() => firstPick(menus));

  useEffect(() => {
    onChoice(choiceOf(menus, pick));
  }, [menus, pick, onChoice]);

  const parts = partsOf(menus);
  const qualities = qualitiesOf(menus, pick.part, pick.format);
  const video = pick.part === 'video';
  const converted = video ? [] : convertedFormats(menus);
  return (
    <NotchCard title={t('video.variant')} info={t('video.hint')} hue={0} style={styles.card}>
      {parts.length > 1 && (
        <WellSelector
          options={parts.map((p) => ({ value: p, label: t(p === 'video' ? 'video.video' : 'video.audio') }))}
          value={pick.part}
          onPick={(p) => setPick(withPart(p))}
          hue={0}
        />
      )}
      <Text style={[styles.label, parts.length < 2 && styles.firstLabel, { color: c.textSub }]}>
        {t(video ? 'video.videoFormat' : 'video.audioFormat')}
      </Text>
      <WellSelector
        options={formatsOf(menus, pick.part).map((f) => ({
          value: f,
          // A conversion reads as one, as the web's menu has them under "Convert to".
          label: converted.includes(f) ? `${t('video.convertTo')} ${formatLabel(f, '')}` : formatLabel(f, t('video.auto')),
        }))}
        value={pick.format}
        onPick={(f) => setPick(withFormat(menus, pick, f))}
        hue={1}
      />
      {qualities.length > 0 && (
        <>
          <Text style={[styles.label, { color: c.textSub }]}>{t(video ? 'video.quality' : 'video.audioBitrate')}</Text>
          <WellSelector
            options={qualities.map((q) => ({ value: q, label: qualityLabel(q, video, t) }))}
            value={pick.quality}
            onPick={(q) => setPick({ ...pick, quality: q })}
            hue={2}
          />
        </>
      )}
    </NotchCard>
  );
}

// As the web's quality picker reads them: Auto for best and a resolution by
// its id, and an audio bitrate in kbit/s.
function qualityLabel(q: string, video: boolean, t: ReturnType<typeof useT>['t']): string {
  if (video) return q === 'best' ? t('video.auto') : q;
  return q === '' ? t('video.best') : t('video.kbps', { kbps: Number(q) });
}

// Colours are applied inline from the resolved tokens, because a stylesheet is
// built once and cannot follow a theme change.
const styles = StyleSheet.create({
  card: { marginTop: 24 },
  probing: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 12 },
  probingText: { fontSize: TYPE.subline },
  label: { fontSize: TYPE.meta, marginTop: 12, marginBottom: 6, letterSpacing: 0.6 },
  firstLabel: { marginTop: 0 },
});
