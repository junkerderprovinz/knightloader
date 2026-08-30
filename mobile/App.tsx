import { useEffect, useState } from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import { StatusBar } from 'expo-status-bar';
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
import { fetchAppearance } from './src/api/client';
import { AppearanceProvider, useAppearance } from './src/theme/AppearanceContext';
import { I18nProvider } from './src/i18n/I18nContext';

type RootStackParamList = {
  Connections: undefined;
  RelayConnect: undefined;
  Downloads: { peer?: Instance } | undefined;
  AddDownload: { peer?: Instance } | undefined;
  Settings: undefined;
  LanguagePicker: undefined;
};

const Stack = createNativeStackNavigator<RootStackParamList>();

// The provider sits ABOVE everything, because look is applied at the root of
// an app and never by the screen that edits it: a screen that paints itself
// leaves every other screen behind on the old value, and the settings page is
// the last place to notice.
export default function App() {
  return (
    <AppearanceProvider>
      <I18nProvider>
        <Shell />
      </I18nProvider>
    </AppearanceProvider>
  );
}

function Shell() {
  const { c, accent, accentInk, dark, setInstanceAppearance } = useAppearance();
  const [conn, setConn] = useState<ServerConnection | null>(null);
  const [loading, setLoading] = useState(true);
  // The screen the navigator opens on: Downloads if a connection was left
  // active last time, Connections otherwise - EVERY other case, saved list
  // or none at all, lands in the app itself first. It used to jump straight
  // to the connect form on a bare-empty list, which is exactly the "thrown
  // into a form before I've even seen the app" jdp asked to remove
  // (2026-08-24: "es soll nicht sofort gleich der Verbindungsbildschirm
  // kommen") - ConnectionsScreen's own empty state already offers the same
  // "add a connection" action, just as something you land ON rather than
  // something forced in front of you.
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

  // Adopt the look of whichever instance is active. Cleared when there is
  // none, so switching to a connection that cannot be reached falls back to
  // the family default rather than keeping the previous instance's colour and
  // quietly claiming to be it.
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

  if (loading) {
    return (
      <View style={[styles.loading, { backgroundColor: c.bg }]}>
        {/* accentInk, not accent: a spinner is ink on the page's own ground,
            and a bright Sunflower on the light theme's near-white is a shape
            you cannot see. */}
        <ActivityIndicator color={accentInk} size="large" />
      </View>
    );
  }

  return (
      <NavigationContainer
        theme={{
          dark,
          // Built from the resolved tokens rather than a second, fixed set:
          // the navigator paints the gaps between screens, and a hard-coded
          // dark there is exactly how a light theme ends up with black bars.
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
                onOpenSettings={() => navigation.navigate('Settings')}
              />
            )}
          </Stack.Screen>

          {/* The name-and-address form is gone (jdp, 2026-08-29: "In der App
              kann man sich immer noch mit Name+URL verbinden. das soll raus.
              nur noch per Phrase!") - the phrase screen is the one way in,
              exactly as it is in the browser extension. What the old form
              uniquely carried and what dies with it, named rather than lost
              silently: the remote-access QR (a bare address) and hand-typed
              token entry. A connection saved back when that path existed
              keeps working; there is just no way to create another one. */}
          <Stack.Screen name="RelayConnect" options={{ presentation: 'modal' }}>
            {({ navigation }) => (
              <RelayConnectScreen
                onConnected={(c) => {
                  setConn(c);
                  navigation.navigate('Downloads', {});
                }}
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
                  onOpenSettings={() => navigation.navigate('Settings')}
                  onBackToOwn={route.params?.peer ? () => navigation.goBack() : undefined}
                  // Removing the connection you are standing in has to leave
                  // it as well - the screen's whole subject just stopped
                  // existing. Back to the overview, which is where the list of
                  // what is left lives.
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

          {/* There was an Instances screen here, listing the federation peers
              of whichever instance was connected, reached from a link in the
              Downloads top bar. jdp took that link out (2026-08-30: "Wenn man
              in einer instanz ist soll oben der button 'Instanzen' weg") and
              renamed the one beside it to Übersicht - which IS the list of
              instances, since every member of the group is a connection
              there. The screen went with the link rather than staying behind
              as an address nothing leads to; it was also the last place in
              the app that still took a name and an address by hand, which the
              phrase replaced everywhere else.

              What is still here: DownloadsScreen and AddDownloadScreen keep
              their `peer` branch. Nothing sets it today - it is the proxy
              path (/api/instances/{name}) a peer view would need, and it is a
              few lines rather than a feature, so it waits rather than being
              rebuilt from scratch if that view comes back. */}

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
                  // "Follow the instance" just cleared the local overrides;
                  // what it must NOT show is the look fetched at startup (jdp:
                  // "Einstellungen übernehmen funktionieren nicht") - so ask
                  // the instance again, now.
                  if (conn) void fetchAppearance(conn).then(setInstanceAppearance);
                }}
              />
            )}
          </Stack.Screen>

          <Stack.Screen name="LanguagePicker">
            {({ navigation }) => <LanguagePickerScreen onBack={() => navigation.goBack()} />}
          </Stack.Screen>
        </Stack.Navigator>
      </NavigationContainer>
  );
}

const navFonts = {
  regular: { fontFamily: 'System', fontWeight: '400' as const },
  medium: { fontFamily: 'System', fontWeight: '500' as const },
  bold: { fontFamily: 'System', fontWeight: '700' as const },
  heavy: { fontFamily: 'System', fontWeight: '900' as const },
};

const styles = StyleSheet.create({
  // The ground colour is applied inline from the resolved tokens, not baked in
  // here: a stylesheet is built once and cannot follow a theme change.
  loading: { flex: 1, alignItems: 'center', justifyContent: 'center' },
});
