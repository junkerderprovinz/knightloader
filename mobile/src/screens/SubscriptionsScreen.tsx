import { useCallback, useEffect, useState } from 'react';
import { RefreshControl, StyleSheet, View } from 'react-native';
import * as Clipboard from 'expo-clipboard';
import { ApiError, errorText } from '../api/client';
import type { ServerConnection } from '../api/types';
import {
  fetchFeedHealth,
  fetchSubscriptions,
  healthText,
  kindOf,
  nameOf,
  offersOf,
  resolveSubscription,
  type FeedHealth,
  type Offer,
  type Subscription,
} from '../api/subscriptions';
import { CardButton, GlimButton, NotchCard } from '../components/glim';
import IconBadge, { Back, Paste, Plus, Search } from '../components/IconBadge';
import { MovingScroll } from '../components/Moving';
import { SubscriptionEdit, type OpenSubscription } from '../components/SubscriptionEdit';
import { EditBox, EditField } from '../components/TaskEdit';
import { Text } from '../components/Text';
import { useT, type TranslationKey } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';

/**
 * The subscriptions of one instance: the feeds, channels and Mediathek
 * searches it checks on its own and hands to its collector. A pasted link is
 * looked up first, so the person picks from what the instance found rather
 * than typing a feed address, and every row opens the editor.
 */
export default function SubscriptionsScreen({ conn, onBack }: { conn: ServerConnection; onBack: () => void }) {
  const { t, lang } = useT();
  const { c, accentInk, corners } = useAppearance();
  const [rows, setRows] = useState<Subscription[] | null>(null);
  const [health, setHealth] = useState<Record<string, FeedHealth>>({});
  const [loadError, setLoadError] = useState('');
  const [pulling, setPulling] = useState(false);
  const [link, setLink] = useState('');
  const [looking, setLooking] = useState(false);
  const [found, setFound] = useState<{ name: string; offers: Offer[] } | null>(null);
  const [lookError, setLookError] = useState<{ text: string; reason: string } | null>(null);
  const [open, setOpen] = useState<OpenSubscription | null>(null);

  const load = useCallback(async () => {
    try {
      const [list, known] = await Promise.all([
        fetchSubscriptions(conn),
        // No health is drawn when nothing answered, and the rows still are.
        fetchFeedHealth(conn).catch(() => ({})),
      ]);
      setRows(list);
      setHealth(known);
      setLoadError('');
    } catch (e) {
      // An instance from before the member routes answers that it has none.
      setLoadError(e instanceof ApiError && e.status === 404 ? t('subs.tooOld') : errorText(t, e));
    }
  }, [conn, t]);

  useEffect(() => {
    void load();
  }, [load]);

  const pull = async () => {
    setPulling(true);
    await load();
    setPulling(false);
  };

  const paste = async () => {
    const text = await Clipboard.getStringAsync().catch(() => '');
    if (text) {
      setLink(text.trim());
      setFound(null);
      setLookError(null);
    }
  };

  const lookUp = async () => {
    setLooking(true);
    setFound(null);
    setLookError(null);
    try {
      const r = await resolveSubscription(conn, link);
      setFound({ name: r.title?.trim() || r.channel?.title?.trim() || '', offers: offersOf(r, link) });
    } catch (e) {
      // The instance's reason is a technical one, so the app's own sentence
      // leads and the reason follows quietly beneath it.
      setLookError(
        e instanceof ApiError && e.code === 'feedNotFound'
          ? { text: t('subs.notFound'), reason: e.message }
          : { text: errorText(t, e), reason: '' },
      );
    }
    setLooking(false);
  };

  const saved = (list: Subscription[]) => {
    setRows(list);
    if (open?.was === null) {
      setLink('');
      setFound(null);
    }
    void fetchFeedHealth(conn).then(setHealth, () => undefined);
  };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <View style={styles.topBar}>
        <IconBadge icon={<Back color={c.textSub} />} onPress={onBack} accessibilityLabel={t('settings.back')} />
        <View style={styles.titles}>
          <Text style={[styles.title, { color: c.text }]}>{t('subs.title')}</Text>
          <Text style={[styles.instance, { color: c.textMuted }]} numberOfLines={1}>
            {conn.name}
          </Text>
        </View>
      </View>

      <MovingScroll
        contentContainerStyle={styles.list}
        keyboardShouldPersistTaps="handled"
        refreshControl={
          <RefreshControl refreshing={pulling} onRefresh={pull} colors={[accentInk]} progressBackgroundColor={c.surface} />
        }
      >
        {loadError ? (
          <Text style={[styles.error, { color: c.statusFailText }]}>{loadError}</Text>
        ) : (
          <>
            <NotchCard title={t('subs.add')} hue={0} info={t('subs.addHint')}>
              <EditField label={t('subs.link')}>
                <EditBox
                  value={link}
                  onChange={(v) => {
                    setLink(v);
                    setFound(null);
                    setLookError(null);
                  }}
                  label={t('subs.link')}
                  placeholder="https://"
                  keyboard="url"
                />
              </EditField>
              <View style={styles.actions}>
                <GlimButton tone="quiet" label={t('subs.paste')} icon={(ink) => <Paste color={ink} />} onPress={paste} />
                <GlimButton
                  grow
                  label={t('subs.lookUp')}
                  icon={(ink) => <Search color={ink} />}
                  busy={looking}
                  disabled={link.trim() === ''}
                  onPress={lookUp}
                />
              </View>
              {found ? (
                <View style={styles.found}>
                  {found.name ? <Text style={[styles.foundName, { color: c.text }]}>{t('subs.found', { name: found.name })}</Text> : null}
                  {found.offers.map((o, i) => (
                    <GlimButton
                      key={o.key}
                      hue={i}
                      tone={i === 0 ? 'solid' : 'quiet'}
                      label={t(o.key)}
                      icon={(ink) => <Plus color={ink} />}
                      onPress={() => setOpen({ was: null, row: o.row })}
                    />
                  ))}
                </View>
              ) : null}
              {lookError ? (
                <View style={styles.found}>
                  <View>
                    <Text style={[styles.lookError, { color: c.statusWarnText }]}>{lookError.text}</Text>
                    {lookError.reason ? (
                      <Text style={[styles.lookReason, { color: c.textMuted }]}>{lookError.reason}</Text>
                    ) : null}
                  </View>
                  <GlimButton
                    tone="quiet"
                    label={t('subs.byHand')}
                    icon={(ink) => <Plus color={ink} />}
                    onPress={() => setOpen({ was: null, row: { url: link.trim(), intervalMinutes: 0 } })}
                  />
                </View>
              ) : null}
            </NotchCard>

            {rows ? (
              <NotchCard title={t('subs.list')} hue={1}>
                {rows.length === 0 ? (
                  <View style={styles.empty}>
                    <Text style={[styles.emptyText, { color: c.textSub }]}>{t('subs.empty')}</Text>
                    <Text style={[styles.emptyHint, { color: c.textMuted }]}>{t('subs.emptyHint')}</Text>
                  </View>
                ) : (
                  <View style={styles.rows}>
                    {rows.map((r) => {
                      const status = healthText(t, lang, health[r.url]);
                      const kind = t(`subs.kind.${kindOf(r)}` as TranslationKey);
                      return (
                        <CardButton
                          key={r.url}
                          style={[styles.row, { backgroundColor: c.surface2, ...corners.control }]}
                          onPress={() => setOpen({ was: r.url, row: r })}
                          accessibilityLabel={`${nameOf(r)}, ${kind}`}
                        >
                          <View style={styles.rowHead}>
                            <Text style={[styles.rowName, { color: c.text }]} numberOfLines={1}>
                              {nameOf(r)}
                            </Text>
                            <Text style={[styles.rowKind, { color: c.textMuted }]}>{kind}</Text>
                          </View>
                          <Text
                            style={[styles.rowStatus, { color: status.trouble ? c.statusWarnText : c.textMuted }]}
                            numberOfLines={2}
                          >
                            {status.text}
                          </Text>
                        </CardButton>
                      );
                    })}
                  </View>
                )}
              </NotchCard>
            ) : null}
          </>
        )}
      </MovingScroll>

      <SubscriptionEdit
        conn={conn}
        open={open}
        health={open ? health[open.row.url] : undefined}
        onClose={() => setOpen(null)}
        onSaved={saved}
      />
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens: a stylesheet
// is built once and cannot follow a theme change.
const capped = { width: '100%' as const, maxWidth: 640, alignSelf: 'center' as const };

const styles = StyleSheet.create({
  container: { flex: 1 },
  topBar: { ...capped, flexDirection: 'row', alignItems: 'center', gap: 12, padding: 16, paddingTop: 56 },
  titles: { flex: 1, minWidth: 0 },
  title: { fontSize: TYPE.status, fontWeight: '600' },
  instance: { fontSize: TYPE.subline, marginTop: 2 },
  list: { ...capped, paddingHorizontal: 16, paddingBottom: 32 },
  error: { fontSize: TYPE.subline, marginTop: 8 },
  actions: { flexDirection: 'row', gap: 8, marginTop: 12 },
  found: { marginTop: 14, gap: 8 },
  foundName: { fontSize: TYPE.body, fontWeight: '600' },
  lookError: { fontSize: TYPE.subline },
  lookReason: { fontSize: TYPE.meta, marginTop: 4 },
  empty: { alignItems: 'center', paddingVertical: 18, gap: 4 },
  emptyText: { fontSize: TYPE.body, textAlign: 'center' },
  emptyHint: { fontSize: TYPE.subline, textAlign: 'center' },
  rows: { gap: 8 },
  row: { paddingHorizontal: 14, paddingVertical: 10, gap: 2 },
  rowHead: { flexDirection: 'row', alignItems: 'baseline', gap: 8 },
  rowName: { flex: 1, fontSize: TYPE.body, fontWeight: '600' },
  rowKind: { fontSize: TYPE.meta },
  rowStatus: { fontSize: TYPE.subline },
});
