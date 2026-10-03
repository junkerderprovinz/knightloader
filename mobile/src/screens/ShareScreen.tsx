import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, Image, ScrollView, StyleSheet, View } from 'react-native';
import { addSharedText, errorText } from '../api/client';
import type { ServerConnection } from '../api/types';
import { listConnections } from '../storage/connections';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { CardButton, DefaultBadge, GlimButton } from '../components/glim';
import { Connect, Cross } from '../components/IconBadge';
import { InfoTip } from '../components/InfoTip';
import { Arrive } from '../components/Moving';
import { Text } from '../components/Text';

const MARK = require('../../assets/android-icon-foreground.png');

type Phase =
  | { kind: 'loading' }
  | { kind: 'unpaired' }
  | { kind: 'pick'; list: ServerConnection[] }
  | { kind: 'busy'; conn: ServerConnection }
  | { kind: 'done'; conn: ServerConnection; count: number }
  | { kind: 'error'; conn: ServerConnection; message: string };

/**
 * Where text shared from another app lands. Nothing is sent until an instance
 * is tapped, also with only one paired: any app can open this screen with an
 * explicit intent, so the share sheet may never have been shown.
 */
export default function ShareScreen({
  text,
  title,
  defaultId,
  onOpen,
  onConnect,
  onClose,
}: {
  text: string;
  title?: string;
  defaultId?: string;
  onOpen: (conn: ServerConnection) => void;
  onConnect: () => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const { c, corners } = useAppearance();
  const [phase, setPhase] = useState<Phase>({ kind: 'loading' });

  const send = useCallback(
    async (conn: ServerConnection) => {
      setPhase({ kind: 'busy', conn });
      try {
        const created = await addSharedText(conn, text, title);
        setPhase({ kind: 'done', conn, count: created.length });
      } catch (err) {
        setPhase({ kind: 'error', conn, message: errorText(t, err) });
      }
    },
    [text, title, t],
  );

  useEffect(() => {
    listConnections().then((list) => setPhase(list.length === 0 ? { kind: 'unpaired' } : { kind: 'pick', list }));
  }, []);

  const result = (p: Extract<Phase, { kind: 'done' }>) => {
    if (p.count === 0) return t('share.none');
    if (p.count === 1) return t('share.added', { name: p.conn.name });
    return t('share.addedCount', { n: p.count, name: p.conn.name });
  };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <View style={styles.titleRow}>
        <Text style={[styles.title, { color: c.text }]}>{t('share.title')}</Text>
        <InfoTip text={t('share.hint')} />
      </View>

      <Text style={[styles.shared, { color: c.textMuted }]} numberOfLines={3}>
        {text}
      </Text>

      {phase.kind === 'loading' && <ActivityIndicator color={c.textMuted} />}

      {phase.kind === 'unpaired' && (
        <Arrive style={[styles.card, { backgroundColor: c.surface, ...corners.card }]}>
          <Connect color={c.textMuted} />
          <Text style={[styles.message, { color: c.textMuted }]}>{t('share.noConnection')}</Text>
          <GlimButton hue={0} label={t('connections.emptyButton')} onPress={onConnect} />
        </Arrive>
      )}

      {phase.kind === 'pick' && (
        <>
          <Text style={[styles.pick, { color: c.text }]}>{t(phase.list.length === 1 ? 'share.pickOne' : 'share.pick')}</Text>
          <ScrollView contentContainerStyle={styles.list}>
            {phase.list.map((conn) => (
              <Arrive key={conn.id}>
                <CardButton
                  style={[styles.row, { backgroundColor: c.surface, ...corners.card }]}
                  onPress={() => void send(conn)}
                >
                  <Image source={MARK} style={styles.rowMark} resizeMode="contain" />
                  <Text style={[styles.rowName, { color: c.text }]} numberOfLines={1}>
                    {conn.name}
                  </Text>
                  {conn.id === defaultId && <DefaultBadge />}
                </CardButton>
              </Arrive>
            ))}
          </ScrollView>
          <View style={styles.actions}>
            <GlimButton tone="quiet" grow label={t('share.cancel')} icon={(ink) => <Cross color={ink} />} onPress={onClose} />
          </View>
        </>
      )}

      {phase.kind === 'busy' && (
        <Arrive style={[styles.card, { backgroundColor: c.surface, ...corners.card }]}>
          <ActivityIndicator color={c.textMuted} />
          <Text style={[styles.message, { color: c.textMuted }]}>{t('share.adding')}</Text>
        </Arrive>
      )}

      {phase.kind === 'done' && (
        <>
          <Arrive style={[styles.card, { backgroundColor: c.surface, ...corners.card }]}>
            <Text style={[styles.message, { color: phase.count > 0 ? c.statusOkText : c.textMuted }]}>{result(phase)}</Text>
          </Arrive>
          <View style={styles.actions}>
            <GlimButton tone="quiet" grow label={t('share.close')} icon={(ink) => <Cross color={ink} />} onPress={onClose} />
            <GlimButton hue={1} grow label={t('share.openCollector')} onPress={() => onOpen(phase.conn)} />
          </View>
        </>
      )}

      {phase.kind === 'error' && (
        <>
          <Arrive style={[styles.card, { backgroundColor: c.surface, ...corners.card }]}>
            <Text style={[styles.message, { color: c.statusFailSolid }]}>
              {t('share.failed', { error: phase.message })}
            </Text>
          </Arrive>
          <View style={styles.actions}>
            <GlimButton tone="quiet" grow label={t('share.close')} icon={(ink) => <Cross color={ink} />} onPress={onClose} />
            <GlimButton hue={1} grow label={t('share.retry')} onPress={() => void send(phase.conn)} />
          </View>
        </>
      )}
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens, because a
// stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  container: { flex: 1, padding: 24, paddingTop: 56, gap: 16 },
  titleRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  title: { fontSize: TYPE.heading, fontWeight: '600', flexShrink: 1 },
  shared: { fontSize: TYPE.dense },
  pick: { fontSize: TYPE.body, fontWeight: '600' },
  list: { gap: 8 },
  card: { alignItems: 'center', gap: 14, paddingVertical: 24, paddingHorizontal: 20 },
  message: { fontSize: TYPE.body, textAlign: 'center' },
  row: { flexDirection: 'row', alignItems: 'center', padding: 14, gap: 12 },
  rowMark: { width: 32, height: 32 },
  rowName: { flex: 1, fontSize: TYPE.body, fontWeight: '600' },
  actions: { flexDirection: 'row', gap: 12, marginTop: 'auto' },
});
