import { Linking, Modal, Pressable, StyleSheet, View, useWindowDimensions } from 'react-native';
import { WebView } from 'react-native-webview';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { COFFEE_WIDGET } from '../donate';
import { GlimButton, NotchCard } from './glim';
import { Cross } from './IconBadge';
import { NoArrival } from './Moving';
import { Text } from './Text';

/**
 * The coffee window: BMAC's own widget in a web view, so a donor pays without
 * leaving the app, card and wallet payments included. It is the web's
 * CoffeeDialog rebuilt in this toolkit from the same strings, and the web view
 * mounts only while the window is open, so nothing from BMAC loads before
 * somebody asks for it.
 */
export function CoffeeDonate({ visible, onClose }: { visible: boolean; onClose: () => void }) {
  const { t } = useT();
  const { c, corners } = useAppearance();
  const { motion } = useMotion();
  const { height } = useWindowDimensions();

  return (
    <Modal visible={visible} transparent animationType={motion === 'off' ? 'none' : 'fade'} onRequestClose={onClose}>
      <NoArrival>
        <Pressable style={[styles.scrim, { backgroundColor: c.scrim }]} onPress={onClose}>
          <Pressable style={styles.window} onPress={() => {}} accessibilityViewIsModal>
            <NotchCard title="Buy Me a Coffee" style={styles.flush}>
              <Text style={[styles.appeal, { color: c.text }]}>{t('settings.donateAppeal')}</Text>
              <Text style={[styles.intro, { color: c.textSub }]}>{t('settings.coffeeIntro')}</Text>
              {/* As tall as the screen allows less the window's own rows: BMAC's
                  payment step runs long, and every point here is scrolling a
                  donor is spared. White behind the page, so its first paint is
                  not a dark hole on the dark theme; BMAC's page is light either
                  way. */}
              <View style={[styles.frame, { height: height * 0.6, backgroundColor: c.surface2, ...corners.card }]}>
                {visible && (
                  <WebView
                    source={{ uri: COFFEE_WIDGET }}
                    domStorageEnabled
                    // A window BMAC opens, such as its terms or a bank's check,
                    // belongs in the phone's browser rather than over the widget.
                    onOpenWindow={(e) => {
                      const url = e.nativeEvent.targetUrl;
                      if (/^https?:\/\//.test(url)) void Linking.openURL(url);
                    }}
                    style={[styles.page, corners.control]}
                    containerStyle={corners.control}
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
  window: { width: '100%', maxWidth: 512 },
  flush: { marginTop: 0 },
  appeal: { fontSize: TYPE.body, lineHeight: 20, marginTop: 4 },
  intro: { fontSize: TYPE.body, lineHeight: 20, marginTop: 8, marginBottom: 12 },
  frame: { padding: 8 },
  page: { flex: 1, backgroundColor: '#ffffff' },
  actions: { flexDirection: 'row', justifyContent: 'flex-end', marginTop: 16 },
});
