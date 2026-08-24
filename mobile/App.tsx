import { useEffect, useState } from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { NavigationContainer } from '@react-navigation/native';
import { createNativeStackNavigator } from '@react-navigation/native-stack';
import { loadConnection } from './src/storage/connection';
import type { ServerConnection } from './src/api/types';
import ConnectScreen from './src/screens/ConnectScreen';
import DownloadsScreen from './src/screens/DownloadsScreen';
import AddDownloadScreen from './src/screens/AddDownloadScreen';
import { colors } from './src/theme';

type RootStackParamList = {
  Connect: undefined;
  Downloads: undefined;
  AddDownload: undefined;
};

const Stack = createNativeStackNavigator<RootStackParamList>();

export default function App() {
  const [conn, setConn] = useState<ServerConnection | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    loadConnection()
      .then(setConn)
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <View style={styles.loading}>
        <ActivityIndicator color={colors.accent} size="large" />
      </View>
    );
  }

  return (
    <NavigationContainer theme={{ dark: true, colors: navColors, fonts: navFonts }}>
      <StatusBar style="light" />
      <Stack.Navigator screenOptions={{ headerShown: false }}>
        {!conn ? (
          <Stack.Screen name="Connect">{() => <ConnectScreen onConnected={setConn} />}</Stack.Screen>
        ) : (
          <>
            <Stack.Screen name="Downloads">
              {({ navigation }) => (
                <DownloadsScreen
                  conn={conn}
                  onAddPress={() => navigation.navigate('AddDownload')}
                  onDisconnect={() => setConn(null)}
                />
              )}
            </Stack.Screen>
            <Stack.Screen name="AddDownload" options={{ presentation: 'modal' }}>
              {({ navigation }) => <AddDownloadScreen conn={conn} onDone={() => navigation.goBack()} />}
            </Stack.Screen>
          </>
        )}
      </Stack.Navigator>
    </NavigationContainer>
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
