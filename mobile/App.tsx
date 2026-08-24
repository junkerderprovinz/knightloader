import { useEffect, useState } from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { NavigationContainer } from '@react-navigation/native';
import { createNativeStackNavigator } from '@react-navigation/native-stack';
import { listConnections, loadActiveConnection, setActiveConnectionId } from './src/storage/connections';
import type { Instance, ServerConnection } from './src/api/types';
import ConnectionsScreen from './src/screens/ConnectionsScreen';
import ConnectScreen from './src/screens/ConnectScreen';
import DownloadsScreen from './src/screens/DownloadsScreen';
import InstancesScreen from './src/screens/InstancesScreen';
import AddDownloadScreen from './src/screens/AddDownloadScreen';
import { colors } from './src/theme';
import { I18nProvider } from './src/i18n/I18nContext';

type RootStackParamList = {
  Connections: undefined;
  AddConnection: undefined;
  Downloads: { peer?: Instance } | undefined;
  Instances: undefined;
  AddDownload: { peer?: Instance } | undefined;
};

const Stack = createNativeStackNavigator<RootStackParamList>();

export default function App() {
  const [conn, setConn] = useState<ServerConnection | null>(null);
  const [loading, setLoading] = useState(true);
  // The screen the navigator opens on: Downloads if a connection was left
  // active last time, Connections if there's a saved list to pick from,
  // AddConnection only on a genuine first run with nothing saved yet -
  // mirrors the single-connection app's old "straight to Connect" behavior
  // for that one case, instead of showing an empty list first.
  const [initialRoute, setInitialRoute] = useState<keyof RootStackParamList>('AddConnection');

  useEffect(() => {
    (async () => {
      const active = await loadActiveConnection();
      if (active) {
        setConn(active);
        setInitialRoute('Downloads');
      } else {
        const saved = await listConnections();
        setInitialRoute(saved.length > 0 ? 'Connections' : 'AddConnection');
      }
      setLoading(false);
    })();
  }, []);

  if (loading) {
    return (
      <View style={styles.loading}>
        <ActivityIndicator color={colors.accent} size="large" />
      </View>
    );
  }

  return (
    <I18nProvider>
      <NavigationContainer theme={{ dark: true, colors: navColors, fonts: navFonts }}>
        <StatusBar style="light" />
        <Stack.Navigator initialRouteName={initialRoute} screenOptions={{ headerShown: false }}>
          <Stack.Screen name="Connections">
            {({ navigation }) => (
              <ConnectionsScreen
                onActivate={(c) => {
                  setConn(c);
                  navigation.navigate('Downloads', {});
                }}
                onAddPress={() => navigation.navigate('AddConnection')}
              />
            )}
          </Stack.Screen>

          <Stack.Screen name="AddConnection" options={{ presentation: 'modal' }}>
            {({ navigation }) => (
              <ConnectScreen
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
                  onOpenInstances={() => navigation.navigate('Instances')}
                  onBackToOwn={route.params?.peer ? () => navigation.goBack() : undefined}
                />
              ) : null
            }
          </Stack.Screen>

          <Stack.Screen name="Instances">
            {({ navigation }) =>
              conn ? (
                <InstancesScreen conn={conn} onOpenInstance={(peer) => navigation.push('Downloads', { peer })} />
              ) : null
            }
          </Stack.Screen>

          <Stack.Screen name="AddDownload" options={{ presentation: 'modal' }}>
            {({ navigation, route }) =>
              conn ? (
                <AddDownloadScreen conn={conn} peer={route.params?.peer} onDone={() => navigation.goBack()} />
              ) : null
            }
          </Stack.Screen>
        </Stack.Navigator>
      </NavigationContainer>
    </I18nProvider>
  );
}

const navColors = {
  primary: colors.accent,
  background: colors.background,
  card: colors.surface,
  text: colors.text,
  border: colors.border,
  notification: colors.danger,
};

const navFonts = {
  regular: { fontFamily: 'System', fontWeight: '400' as const },
  medium: { fontFamily: 'System', fontWeight: '500' as const },
  bold: { fontFamily: 'System', fontWeight: '700' as const },
  heavy: { fontFamily: 'System', fontWeight: '900' as const },
};

const styles = StyleSheet.create({
  loading: { flex: 1, backgroundColor: colors.background, alignItems: 'center', justifyContent: 'center' },
});
