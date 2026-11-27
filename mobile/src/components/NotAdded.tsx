import { useState, type ReactNode } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { heldReason, originWord, restoreWord, skippedReason, when, type Held, type SkippedLink } from '../api/notAdded';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { NUM, TYPE } from '../theme/tokens';
import { GlimButton, NotchCard } from './glim';
import IconBadge, { Cross, Empty, Restore } from './IconBadge';
import { Text } from './Text';

/**
 * HeldLinks is the web's holding area for links a filter rule, a banned
 * tracker or the download history rejected, kept apart from the collector so
 * a working filter does not look like junk. Restore puts a link back past
 * what held it, Clear deletes it. Not optimistic: the rows go once the next
 * read of the list says so.
 */
export function HeldLinks({
  held,
  onRestore,
  onClear,
}: {
  held: Held[];
  /** Undefined where the instance cannot be asked, a peer's list. */
  onRestore?: (ids: string[]) => Promise<void>;
  onClear?: (ids: string[]) => Promise<void>;
}) {
  const { t } = useT();
  const { c } = useAppearance();
  const [busy, setBusy] = useState(false);
  if (held.length === 0) return null;

  const act = (run?: (ids: string[]) => Promise<void>) =>
    run &&
    (async (ids: string[]) => {
      setBusy(true);
      try {
        await run(ids);
      } finally {
        setBusy(false);
      }
    });
  const restore = act(onRestore);
  const clear = act(onClear);
  const all = held.map((h) => h.id);

  return (
    <NotchCard title={`${t('notice.filteredName')} (${held.length})`} info={t('collector.filtered.heldInfo')} style={styles.card}>
      <Rows>
        {[...held].reverse().map((h) => (
          <Row
            key={h.id}
            reason={heldReason(t, h)}
            name={h.name && h.name !== h.url ? h.name : h.url}
            source={[originWord(t, h.origin), when(h.createdAt)].filter(Boolean).join(' · ')}
            // Worded, since getting a file a second time is a choice the row
            // should spell out, and under the reason, which for a repeat is
            // a long sentence that needs the card's width.
            below={
              restore && h.skipCode === 'downloaded' ? (
                <GlimButton
                  tone="quiet"
                  label={t(restoreWord(h))}
                  icon={(ink) => <Restore color={ink} />}
                  disabled={busy}
                  onPress={() => void restore([h.id])}
                />
              ) : undefined
            }
            action={
              restore && h.skipCode !== 'downloaded' ? (
                <IconBadge
                  icon={<Restore color={c.textSub} />}
                  onPress={() => !busy && void restore([h.id])}
                  accessibilityLabel={t(restoreWord(h))}
                />
              ) : undefined
            }
          />
        ))}
      </Rows>
      {restore && clear && (
        <View style={styles.actions}>
          <GlimButton
            tone="quiet"
            grow
            label={t('collector.filtered.clear')}
            icon={(ink) => <Empty color={ink} />}
            disabled={busy}
            onPress={() => void clear(all)}
          />
          <GlimButton
            tone="quiet"
            grow
            label={t('collector.filtered.restoreAll')}
            icon={(ink) => <Restore color={ink} />}
            disabled={busy}
            onPress={() => void restore(all)}
          />
        </View>
      )}
    </NotchCard>
  );
}

/**
 * SkippedLinks lists links that did not become downloads, each with its
 * reason, so a link that vanished on paste has an explanation. Dismissing
 * means read: the card is back as soon as another one arrives. Clear forgets
 * them on the instance.
 */
export function SkippedLinks({
  skipped,
  onClear,
}: {
  skipped: SkippedLink[];
  onClear: () => Promise<void>;
}) {
  const { t } = useT();
  const [busy, setBusy] = useState(false);
  // How many were there when it was put away, so a new one brings it back.
  const [dismissedAt, setDismissedAt] = useState(-1);
  if (skipped.length === 0 || skipped.length === dismissedAt) return null;

  const clear = async () => {
    setBusy(true);
    try {
      await onClear();
    } finally {
      setBusy(false);
    }
  };

  return (
    <NotchCard title={`${t('notice.skippedName')} (${skipped.length})`} info={t('skipped.info')} style={styles.card}>
      <Rows>
        {[...skipped].reverse().map((s, i) => (
          <Row key={`${s.at}|${s.url}|${i}`} reason={skippedReason(t, s)} name={s.url} source={when(s.at)} />
        ))}
      </Rows>
      <View style={styles.actions}>
        <GlimButton
          tone="quiet"
          grow
          label={t('common.dismiss')}
          icon={(ink) => <Cross color={ink} />}
          onPress={() => setDismissedAt(skipped.length)}
        />
        <GlimButton
          tone="quiet"
          grow
          label={t('skipped.clear')}
          icon={(ink) => <Empty color={ink} />}
          busy={busy}
          onPress={() => void clear()}
        />
      </View>
    </NotchCard>
  );
}

/** Every row is listed, the box scrolling on its own once there are many, as on the web. */
function Rows({ children }: { children: ReactNode }) {
  return (
    <ScrollView style={styles.rows} nestedScrollEnabled>
      {children}
    </ScrollView>
  );
}

function Row({
  reason,
  name,
  source,
  action,
  below,
}: {
  reason: string;
  name: string;
  source: string;
  action?: ReactNode;
  below?: ReactNode;
}) {
  const { c } = useAppearance();
  return (
    <View style={styles.row}>
      <View style={styles.rowText}>
        <Text style={[styles.name, { color: c.text }]} numberOfLines={1} ellipsizeMode="middle">
          {name}
        </Text>
        <Text style={[styles.reason, { color: c.textSub }]}>{reason}</Text>
        {source !== '' && <Text style={[styles.source, NUM, { color: c.textMuted }]}>{source}</Text>}
        {below ? <View style={styles.below}>{below}</View> : null}
      </View>
      {action}
    </View>
  );
}

const styles = StyleSheet.create({
  // Close under the tab strip, with room for the notch that hangs over the top edge.
  card: { marginTop: 20, marginBottom: 4 },
  rows: { maxHeight: 224, marginTop: 4 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 6 },
  rowText: { flex: 1, minWidth: 0, gap: 2 },
  name: { fontSize: TYPE.body },
  reason: { fontSize: TYPE.subline },
  source: { fontSize: TYPE.meta },
  below: { flexDirection: 'row', marginTop: 6 },
  actions: { flexDirection: 'row', gap: 8, marginTop: 12 },
});
