import { StyleSheet, View } from 'react-native';
import type { ServerConnection } from '../api/types';
import { useT } from '../i18n/I18nContext';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { CardButton, NotchCard, UnavailableNotice } from './glim';
import { Back } from './IconBadge';
import { Text } from './Text';

/**
 * The settings' door to the subscriptions. They belong to an instance rather
 * than to the phone, so the card lists every saved instance and each opens its
 * own, the way the language button opens its page.
 */
export function SubscriptionsCard({
  hue,
  connections,
  onOpen,
}: {
  hue: number;
  connections: ServerConnection[];
  onOpen: (conn: ServerConnection) => void;
}) {
  const { t } = useT();
  const { c, corners } = useAppearance();
  return (
    <NotchCard title={t('subs.title')} hue={hue} info={t('subs.settingsHint')}>
      {connections.length === 0 ? (
        <UnavailableNotice title={t('subs.title')} reason={t('subs.settingsNone')} />
      ) : (
        <View style={styles.list}>
          {connections.map((conn) => (
            <CardButton
              key={conn.id}
              style={[styles.button, { backgroundColor: c.surface2, ...corners.control }]}
              onPress={() => onOpen(conn)}
              accessibilityLabel={t('subs.settingsOpen', { name: conn.name })}
            >
              <Text style={[styles.name, { color: c.text }]} numberOfLines={1}>
                {conn.name}
              </Text>
              {/* The back triangle turned round, as on the language button. */}
              <View style={styles.chevron}>
                <Back color={c.textMuted} size={14} />
              </View>
            </CardButton>
          ))}
        </View>
      )}
    </NotchCard>
  );
}

const styles = StyleSheet.create({
  list: { gap: 8 },
  button: { flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 48, paddingHorizontal: 14 },
  name: { flex: 1, fontSize: TYPE.body, fontWeight: '600' },
  chevron: { transform: [{ scaleX: -1 }] },
});
