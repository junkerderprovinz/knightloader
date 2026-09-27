import { ActivityIndicator, Modal, Pressable, StyleSheet, View, useWindowDimensions } from 'react-native';
import { WebView } from 'react-native-webview';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { GlimButton, NotchCard } from './glim';
import { Cross } from './IconBadge';
import { NoArrival } from './Moving';
import { Text } from './Text';

/** The one Buy Me a Coffee page that allows framing. */
const COFFEE_WIDGET = 'https://buymeacoffee.com/widget/page/junkerderprovinz?description=&color=%23FFDD00';

/**
 * The coffee window: the appeal and BMAC's own widget page, so a donor pays
 * without leaving the app. Nothing from BMAC loads before the window opens,
 * since the web view mounts only while it is visible.
 */
export function CoffeeDonate({ visible, onClose }: { visible: boolean; onClose: () => void }) {
  const { t } = useT();
  const { c, corners, accent } = useAppearance();
  const { motion } = useMotion();
  const { height } = useWindowDimensions();

  return (
    <Modal visible={visible} transparent animationType={motion === 'off' ? 'none' : 'fade'} onRequestClose={onClose}>
      <NoArrival>
        <Pressable style={[styles.scrim, { backgroundColor: c.scrim }]} onPress={onClose}>
          {/* Swallows the press so a tap inside the window closes nothing. */}
          <Pressable style={styles.window} onPress={() => {}} accessibilityViewIsModal>
            <NotchCard title="Buy Me a Coffee" info={t('settings.coffeeIntro')} style={styles.flush}>
              <Text style={[styles.appeal, { color: c.text }]}>{t('settings.donateAppeal')}</Text>
              {/* A fixed share of the screen, since the widget scrolls itself
                  and a scroll view around it would fight it for the gesture. */}
              <View style={[styles.widget, { height: height * 0.55, ...corners.control }]}>
                {visible && (
                  <WebView
                    source={{ uri: COFFEE_WIDGET }}
                    startInLoadingState
                    renderLoading={() => (
                      <View style={styles.loading}>
                        <ActivityIndicator color={accent} />
                      </View>
                    )}
                  />
                )}
              </View>
              <View style={styles.actions}>
                <GlimButton tone="quiet" label={t('settings.donateClose')} icon={(ink) => <Cross color={ink} />} onPress={onClose} />
              </View>
            </NotchCard>
          </Pressable>
        </Pressable>
      </NoArrival>
    </Modal>
  );
}

const styles = StyleSheet.create({
  scrim: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: 16 },
  window: { width: '100%', maxWidth: 480 },
  flush: { marginTop: 0 },
  appeal: { fontSize: TYPE.body, lineHeight: 20, marginBottom: 16 },
  // BMAC paints its page white, so the box does too while it loads.
  widget: { overflow: 'hidden', backgroundColor: '#ffffff' },
  loading: { position: 'absolute', top: 0, right: 0, bottom: 0, left: 0, alignItems: 'center', justifyContent: 'center' },
  actions: { flexDirection: 'row', justifyContent: 'flex-end', marginTop: 16 },
});
