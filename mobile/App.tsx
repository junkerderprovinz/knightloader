import { useEffect, useState } from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { useFonts } from 'expo-font';
import { NavigationContainer } from '@react-navigation/native';
import { createNativeStackNavigator } from '@react-navigation/native-stack';
import { loadActiveConnection, removeConnection, setActiveConnectionId } from './src/storage/connections';
import type { Instance, ServerConnection } from './src/api/types';
import ConnectionsScreen from './src/screens/ConnectionsScreen';
import RelayConnectScreen from './src/screens/RelayConnectScreen';
import DownloadsScreen from './src/screens/DownloadsScreen';
import AddDownloadScreen from './src/screens/AddDownloadScreen';
import SettingsScreen from './src/screens/SettingsScreen';
import LanguagePickerScreen from './src/screens/LanguagePickerScreen';
import { fetchAppearance, setRainbowPalette } from './src/api/client';
import { AppearanceProvider, useAppearance } from './src/theme/AppearanceContext';
import { MotionProvider } from './src/theme/MotionContext';
import { I18nProvider } from './src/i18n/I18nContext';
import { HouseFontReady } from './src/components/Text';
import { HOUSE_FONTS, familyFor } from './src/theme/font';

type RootStackParamList = {
  Connections: undefined;
  RelayConnect: undefined;
  Downloads: { peer?: Instance } | undefined;
  AddDownload: { peer?: Instance } | undefined;
  Settings: undefined;
  LanguagePicker: undefined;
};

const Stack = createNativeStackNavigator<RootStackParamList>();

// The providers sit above everything, because look is applied at the root of
// an app and never by the screen that edits it: a screen that paints itself
// leaves every other screen on the old value.
//
// Motion is its own provider rather than four more fields on the appearance
// one. Appearance is what an instance may lead on, colour, corners and the
// palette, while motion belongs to this phone and the person holding it, never
// travels over the wire, and is half an operating-system setting no server has
// business overriding. It sits outside appearance, because disco's glide is
// motion and reads the level.
export default function App() {
  return (
    <MotionProvider>
      <AppearanceProvider>
        <I18nProvider>
          <Shell />
        </I18nProvider>
      </AppearanceProvider>
    </MotionProvider>
  );
}

function Shell() {
  const { c, accent, accentInk, dark, setInstanceAppearance } = useAppearance();
  const [conn, setConn] = useState<ServerConnection | null>(null);
  const [loading, setLoading] = useState(true);
  // The first screen waits for the house font as it waits for the saved
  // connection, so no label is drawn in the system font and then redrawn.
  const [fontLoaded, fontError] = useFonts(HOUSE_FONTS);
  // The screen the navigator opens on: Downloads if a connection was left
  // active last time, Connections otherwise. Opening the connect form on an
  // empty list would put a form in front of somebody who has not seen the app
  // yet, and ConnectionsScreen's empty state offers the same action.
  const [initialRoute, setInitialRoute] = useState<keyof RootStackParamList>('Connections');

  useEffect(() => {
    (async () => {
      const active = await loadActiveConnection();
      if (active) {
        setConn(active);
        setInitialRoute('Downloads');
      }
      setLoading(false);
    })();
  }, []);

  // Adopt the look of whichever instance is active. Cleared when there is none,
  // so switching to a connection that cannot be reached falls back to the
  // family default rather than keeping the previous instance's colour.
  useEffect(() => {
    let alive = true;
    if (!conn) {
      setInstanceAppearance(undefined);
      return;
    }
    fetchAppearance(conn).then((a) => {
      if (alive) setInstanceAppearance(a);
    });
    return () => {
      alive = false;
    };
  }, [conn, setInstanceAppearance]);

  if (loading || (!fontLoaded && !fontError)) {
    return (
      <View style={[styles.loading, { backgroundColor: c.bg }]}>
        {/* accentInk rather than accent: a spinner is ink on the page's ground,
            and a bright Sunflower on the light theme's near-white is a shape
            you cannot see. */}
        <ActivityIndicator color={accentInk} size="large" />
      </View>
    );
  }

  return (
    <HouseFontReady value={fontLoaded}>
      <NavigationContainer
        theme={{
          dark,
          // Built from the resolved tokens rather than a second fixed set: the
          // navigator paints the gaps between screens, and a hard-coded dark
          // there gives a light theme black bars.
          colors: {
            primary: accent,
            background: c.bg,
            card: c.surface,
            text: c.text,
            border: c.border,
            notification: accent,
          },
          fonts: navFonts,
        }}
      >
        <StatusBar style={dark ? 'light' : 'dark'} />
        <Stack.Navigator initialRouteName={initialRoute} screenOptions={{ headerShown: false }}>
          <Stack.Screen name="Connections">
            {({ navigation }) => (
              <ConnectionsScreen
                onActivate={(c) => {
                  setConn(c);
                  navigation.navigate('Downloads', {});
                }}
                onAddPress={() => navigation.navigate('RelayConnect')}
                // The one door to Settings in the whole app. Settings are not
                // a property of one instance, and a gear inside one would
                // suggest they were.
                onOpenSettings={() => navigation.navigate('Settings')}
              />
            )}
          </Stack.Screen>

          {/* The phrase screen is the one way in, as it is in the browser
              extension. The name-and-address form it replaced also carried the
              remote-access QR, which is a bare address, and hand-typed token
              entry; a connection saved through it keeps working, but there is
              no way to create another one. */}
          <Stack.Screen name="RelayConnect" options={{ presentation: 'modal' }}>
            {({ navigation }) => (
              <RelayConnectScreen
                onConnected={(c) => {
                  setConn(c);
                  navigation.navigate('Downloads', {});
                }}
                // goBack rather than navigate('Connections'): this screen is
                // reached from the overview and from its empty state, and back
                // means whichever of those it was.
                onBack={() => navigation.goBack()}
              />
            )}
          </Stack.Screen>

          <Stack.Screen name="Downloads">
            {({ navigation, route }) =>
              conn ? (
                <DownloadsScreen
                  conn={conn}
                  peer={route.params?.peer}
                  onAddPress={() => navigation.navigate('AddDownload', { peer: route.params?.peer })}
                  onSwitchConnection={async () => {
                    await setActiveConnectionId(null);
                    navigation.navigate('Connections');
                  }}
                    onBackToOwn={route.params?.peer ? () => navigation.goBack() : undefined}
                  // Removing the connection this screen is about leaves it as
                  // well, back to the overview, where the list of what is left
                  // lives.
                  onRemoveConnection={
                    route.params?.peer
                      ? undefined
                      : async () => {
                          await removeConnection(conn.id);
                          await setActiveConnectionId(null);
                          setConn(null);
                          navigation.reset({ index: 0, routes: [{ name: 'Connections' }] });
                        }
                  }
                />
              ) : null
            }
          </Stack.Screen>

          {/* There is no Instances screen: the overview is the list of
              instances, since every member of the group is a connection there.
              DownloadsScreen and AddDownloadScreen keep their `peer` branch,
              the proxy path (/api/instances/{name}) a peer view would need,
              although nothing sets it today. */}

          <Stack.Screen name="AddDownload" options={{ presentation: 'modal' }}>
            {({ navigation, route }) =>
              conn ? (
                <AddDownloadScreen conn={conn} peer={route.params?.peer} onDone={() => navigation.goBack()} />
              ) : null
            }
          </Stack.Screen>

          <Stack.Screen name="Settings">
            {({ navigation }) => (
              <SettingsScreen
                onBack={() => navigation.goBack()}
                onOpenLanguagePicker={() => navigation.navigate('LanguagePicker')}
                onRemovedAllConnections={() => {
                  setConn(null);
                  navigation.reset({ index: 0, routes: [{ name: 'Connections' }] });
                }}
                onRefreshAppearance={() => {
                  // "Follow the instance" has just cleared the local
                  // overrides, so the instance is asked again rather than the
                  // screen falling back to the look fetched at startup.
                  if (conn) void fetchAppearance(conn).then(setInstanceAppearance);
                }}
                // The rainbow palette belongs to the instance, so editing one
                // is a write over the wire rather than a local preference (see
                // setRainbowPalette. Passed only when there is a connection,
                // which is what makes the settings screen drop the row and say
                // why instead of drawing eight swatches no press can reach.
                onSetPalette={
                  conn
                    ? async (palette) => setInstanceAppearance(await setRainbowPalette(conn, palette))
                    : undefined
                }
              />
            )}
          </Stack.Screen>

          <Stack.Screen name="LanguagePicker">
            {({ navigation }) => <LanguagePickerScreen onBack={() => navigation.goBack()} />}
          </Stack.Screen>
        </Stack.Navigator>
      </NavigationContainer>
    </HouseFontReady>
  );
}

// The navigator draws its own text, outside components/Text.tsx, so each entry
// names its Noto cut itself. The weight is left at normal because the cut
// already carries it, and on Android a bold weight on top of a runtime-loaded
// family falls back to the system font.
const navFonts = {
  regular: { fontFamily: familyFor(400), fontWeight: 'normal' as const },
  medium: { fontFamily: familyFor(500), fontWeight: 'normal' as const },
  bold: { fontFamily: familyFor(700), fontWeight: 'normal' as const },
  heavy: { fontFamily: familyFor(900), fontWeight: 'normal' as const },
};

const styles = StyleSheet.create({
  // The ground colour is applied inline from the resolved tokens rather than
  // baked in here: a stylesheet is built once and cannot follow a theme change.
  loading: { flex: 1, alignItems: 'center', justifyContent: 'center' },
});
